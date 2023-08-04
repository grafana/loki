package cache

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"

	"golang.org/x/net/http2"

	"github.com/grafana/groupcache_exporter"
	"github.com/mailgun/groupcache/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/server"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"

	lokiutil "github.com/grafana/loki/pkg/util"
)

type GroupCache struct {
	peerRing             ring.ReadRing
	cache                *groupcache.Group
	pool                 *groupcache.HTTPPool
	stopChan             chan struct{}
	updateInterval       time.Duration
	logger               log.Logger
	wg                   sync.WaitGroup
	reg                  prometheus.Registerer
	startWaitingForClose context.CancelFunc
}

// RingCfg is a wrapper for the Groupcache ring configuration plus the replication factor.
type RingCfg struct {
	lokiutil.RingConfig `yaml:",inline"`
}

type GroupCacheConfig struct {
	Enabled bool    `yaml:"enabled,omitempty"`
	Ring    RingCfg `yaml:"ring,omitempty"`

	Cache Cache `yaml:"-"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *GroupCacheConfig) RegisterFlagsWithPrefix(prefix, _ string, f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix(prefix, "", f)

	f.BoolVar(&cfg.Enabled, prefix+".enabled", false, "Whether or not groupcache is enabled")
}

type ringManager interface {
	Addr() string
	Ring() ring.ReadRing
}

func NewGroupCache(rm ringManager, server *server.Server, logger log.Logger, reg prometheus.Registerer) (*GroupCache, error) {
	addr := fmt.Sprintf("http://%s", rm.Addr())
	level.Info(logger).Log("msg", "groupcache local address set to", "addr", addr)

	pool := groupcache.NewHTTPPoolOpts(addr, &groupcache.HTTPPoolOptions{Transport: http2Transport})
	server.HTTP.PathPrefix("/_groupcache/").Handler(pool)

	startCtx, cancel := context.WithCancel(context.Background())
	cache := &GroupCache{
		peerRing:             rm.Ring(),
		pool:                 pool,
		logger:               logger,
		stopChan:             make(chan struct{}),
		updateInterval:       1 * time.Minute,
		wg:                   sync.WaitGroup{},
		startWaitingForClose: cancel,
		reg:                  reg,
	}

	go func() {
		// Avoid starting the cache and peer discovery until
		// a cache is being used
		<-startCtx.Done()
		go cache.updatePeers()

		cache.wg.Wait()
		close(cache.stopChan)
	}()

	return cache, nil
}

func (c *GroupCache) updatePeers() {
	c.update()

	t := time.NewTicker(c.updateInterval)
	for {
		select {
		case <-t.C:
			c.update()
		case <-c.stopChan:
			return
		}
	}
}

func (c *GroupCache) update() {
	urls, err := c.peerUrls()
	if err != nil {
		level.Warn(c.logger).Log("msg", "unable to get groupcache peer urls", "err", err)
		return
	}

	level.Info(c.logger).Log("msg", "got groupcache peers", "peers", strings.Join(urls, ","))
	c.pool.Set(urls...)
}

func (c *GroupCache) peerUrls() ([]string, error) {
	replicationSet, err := c.peerRing.GetAllHealthy(ring.WriteNoExtend)
	if err != nil {
		return nil, err
	}

	var addrs []string
	for _, i := range replicationSet.Instances {
		addrs = append(addrs, fmt.Sprintf("http://%s", i.Addr))
	}
	return addrs, nil
}

func (c *GroupCache) Stats() *groupcache.Stats {
	if c.cache == nil {
		return nil
	}

	return &c.cache.Stats
}

type group struct {
	cache         *groupcache.Group
	logger        log.Logger
	wg            *sync.WaitGroup
	fetchDuration prometheus.Observer
	storeDuration prometheus.Observer
}

func (c *GroupCache) NewGroup(name string, getter groupcache.GetterFunc) SingleFlightCache {
	c.wg.Add(1)
	c.startWaitingForClose()

	requestDuration := promauto.With(c.reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "loki",
		Name:        "groupcache_request_duration_seconds",
		Help:        "Total time spent in seconds doing groupcache requests.",
		Buckets:     instrument.DefBuckets,
		ConstLabels: prometheus.Labels{"name": name},
	}, []string{"operation"})

	g := &group{
		cache:         groupcache.NewGroup(name, 1<<30, getter),
		logger:        c.logger,
		wg:            &c.wg,
		fetchDuration: requestDuration.WithLabelValues("fetch"),
	}

	exp := groupcache_exporter.NewExporter(map[string]string{"name": name}, g)
	prometheus.WrapRegistererWithPrefix("loki_groupcache_", c.reg).MustRegister(exp)

	return g
}

func (c *group) Fetch(ctx context.Context, key string, dest groupcache.Sink) error {
	start := time.Now()
	defer c.fetchDuration.Observe(time.Since(start).Seconds())

	return c.cache.Get(ctx, key, dest)
}

func (c *group) Stop() {
	c.wg.Done()
}

func http2Transport(_ context.Context) http.RoundTripper {
	return &http2.Transport{
		DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
		AllowHTTP: true,
	}
}
