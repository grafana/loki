package cache

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/grafana/dskit/middleware"

	"github.com/grafana/dskit/tenant"

	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/instrument"

	"github.com/golang/groupcache"
	"github.com/grafana/groupcache_exporter"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"

	"github.com/grafana/dskit/server"
	lokiutil "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	orgHeaderKey = "X-Scope-OrgID"
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

func NewGroupCache(rm ringManager, server *server.Server, HTTPAuthMiddleware middleware.Interface, logger log.Logger, reg prometheus.Registerer) (*GroupCache, error) {
	addr := fmt.Sprintf("http://%s", rm.Addr())
	level.Info(logger).Log("msg", "groupcache local address set to", "addr", addr)

	pool := groupcache.NewHTTPPoolOpts(addr, &groupcache.HTTPPoolOptions{})
	pool.Transport = orgIDTransport

	server.HTTP.PathPrefix("/_groupcache/").Handler(HTTPAuthMiddleware.Wrap(pool))

	startCtx, cancel := context.WithCancel(context.Background())
	cache := &GroupCache{
		pool:                 pool,
		peerRing:             rm.Ring(),
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

func orgIDTransport(ctx context.Context) http.RoundTripper {
	return &orgIDRoundTripper{
		ctx: ctx,
	}
}

type orgIDRoundTripper struct {
	ctx context.Context
}

func (r orgIDRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	orgID, err := tenant.TenantID(r.ctx)
	if err != nil {
		return nil, err
	}

	headers, ok := r.ctx.Value("headers").(http.Header)
	if !ok {
		err = fmt.Errorf("expected http.Header got %T", r.ctx.Value("headers"))
		level.Error(util_log.Logger).Log("msg", "error propagating headers", "err", err)
		return nil, err
	}

	for k, h := range headers {
		for _, v := range h {
			req.Header.Set(k, v)
		}
	}

	req.Header.Set(orgHeaderKey, orgID)
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "peer load failed", "err", err)
		return nil, err
	}

	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body) // no consequence if this fails
		level.Error(util_log.Logger).Log("msg", "unexpected peer load response", "resp_code", resp.StatusCode, "body", string(body))
	}

	return resp, err
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
		cache:         groupcache.NewGroup(name, 0, getter), // 0 Cache size means this is just a singleflight
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
