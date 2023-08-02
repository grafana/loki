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

	"github.com/weaveworks/common/instrument"

	"github.com/prometheus/client_golang/prometheus/promauto"

	"golang.org/x/net/http2"

	"github.com/grafana/groupcache_exporter"
	"github.com/mailgun/groupcache/v2"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/server"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	lokiutil "github.com/grafana/loki/pkg/util"
)

var (
	ErrGroupcacheMiss = errors.New("cache miss")
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
	cacheType     stats.CacheType
	fetchDuration prometheus.Observer
	storeDuration prometheus.Observer
}

func (c *GroupCache) NewGroup(name string, ct stats.CacheType) Cache {
	// Return a known error on miss to track which keys need to be inserted
	// TODO, setup a getter that does an http request and returns the result
	missGetter := groupcache.GetterFunc(func(_ context.Context, _ string, _ groupcache.Sink) error {
		return ErrGroupcacheMiss
	})

	c.wg.Add(1)
	c.startWaitingForClose()

	requestDuration := promauto.With(c.reg).NewHistogramVec(prometheus.HistogramOpts{
		Namespace:   "loki",
		Name:        "groupcache_request_duration_seconds",
		Help:        "Total time spent in seconds doing groupcache requests.",
		Buckets:     instrument.DefBuckets,
		ConstLabels: prometheus.Labels{"cache_type": string(ct)},
	}, []string{"operation"})

	g := &group{
		cache:         groupcache.NewGroup(name, 1<<30, missGetter),
		logger:        c.logger,
		wg:            &c.wg,
		cacheType:     ct,
		fetchDuration: requestDuration.WithLabelValues("fetch"),
		storeDuration: requestDuration.WithLabelValues("store"),
	}

	exp := groupcache_exporter.NewExporter(map[string]string{"cache_type": string(ct)}, g)
	prometheus.WrapRegistererWithPrefix("loki_groupcache_", c.reg).MustRegister(exp)

	return g
}

func (c *group) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	// TODO: This needs to change to support what I want to do with requests/results
	var (
		start  = time.Now()
		values = make([][]byte, 0, len(keys))
		missed = make([]string, 0, len(keys))
		found  = make([]string, 0, len(keys))
		data   = groupcache.ByteView{}
		sink   = groupcache.ByteViewSink(&data)
	)

	for _, key := range keys {
		if err := c.cache.Get(ctx, key, sink); err != nil {
			if errors.Is(err, ErrGroupcacheMiss) {
				missed = append(missed, key)
				continue
			}

			level.Error(c.logger).Log("msg", "unable to fetch from groupcache", "err", err)
			return found, values, missed, err
		}

		found = append(found, key)
		values = append(values, data.ByteSlice())
	}

	c.fetchDuration.Observe(time.Since(start).Seconds())
	return found, values, missed, nil
}

func (c *group) Store(ctx context.Context, keys []string, values [][]byte) error {
	start := time.Now()

	var lastErr error
	for i, key := range keys {
		if err := c.cache.Set(ctx, key, values[i], time.Time{}, true); err != nil {
			level.Warn(c.logger).Log("msg", "failed to put to groupcache", "err", err)
			lastErr = err
		}
	}

	c.storeDuration.Observe(time.Since(start).Seconds())
	return lastErr
}

func (c *group) Stop() {
	c.wg.Done()
}

func (c *group) GetCacheType() stats.CacheType {
	return c.cacheType
}

func http2Transport(_ context.Context) http.RoundTripper {
	return &http2.Transport{
		DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial(network, addr)
		},
		AllowHTTP: true,
	}
}
