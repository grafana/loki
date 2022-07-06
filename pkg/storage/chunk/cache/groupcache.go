package cache

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/grafana/groupcache_exporter"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/server"

	"github.com/mailgun/groupcache/v2"

	"github.com/go-kit/kit/log/level"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	lokiutil "github.com/grafana/loki/pkg/util"
)

const (
	ringAutoForgetUnhealthyPeriods = 10
	ringNameForServer              = "groupcache"
	ringNumTokens                  = 1
	ringCheckPeriod                = 3 * time.Second

	GroupcacheRingKey = "groupcache"
)

var (
	ErrGroupcacheMiss = errors.New("cache miss")
)

type GroupCache struct {
	peerRing             *ring.Ring
	cache                *groupcache.Group
	pool                 *groupcache.HTTPPool
	cacheType            stats.CacheType
	stopChan             chan struct{}
	updateInterval       time.Duration
	logger               log.Logger
	listenPort           int
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

func NewGroupCache(rm *GroupcacheRingManager, server *server.Server, logger log.Logger, reg prometheus.Registerer) (*GroupCache, error) {
	addr := fmt.Sprintf("http://%s", rm.Addr)
	level.Info(logger).Log("msg", "groupcache local address set to", "addr", addr)

	pool := groupcache.NewHTTPPoolOpts(addr, &groupcache.HTTPPoolOptions{})
	server.HTTP.PathPrefix("/_groupcache/").Handler(pool)

	startCtx, cancel := context.WithCancel(context.Background())
	cache := &GroupCache{
		peerRing:             rm.Ring,
		pool:                 pool,
		logger:               logger,
		stopChan:             make(chan struct{}),
		updateInterval:       5 * time.Minute,
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

func (c *GroupCache) NewGroup(name string, ct stats.CacheType) Cache {
	// Return a known error on miss to track which keys need to be inserted
	missGetter := groupcache.GetterFunc(func(_ context.Context, _ string, _ groupcache.Sink) error {
		return ErrGroupcacheMiss
	})

	c.wg.Add(1)
	c.startWaitingForClose()

	g := &group{
		cache:     groupcache.NewGroup(name, 1<<30, missGetter),
		logger:    c.logger,
		wg:        &c.wg,
		cacheType: ct,
	}

	exp := groupcache_exporter.NewExporter(map[string]string{"cache_type": string(ct)}, g)
	c.reg.MustRegister(exp)

	return g
}

func (c *GroupCache) Stats() *groupcache.Stats {
	if c.cache == nil {
		return nil
	}

	return &c.cache.Stats
}

type group struct {
	cache     *groupcache.Group
	logger    log.Logger
	wg        *sync.WaitGroup
	cacheType stats.CacheType
}

// TODO(tpatterson): Issue the key requests in parallel?
func (c *group) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	values := make([][]byte, 0, len(keys))
	missed := make([]string, 0, len(keys))
	found := make([]string, 0, len(keys))

	var data []byte
	for _, key := range keys {
		// TODO: this allocates a new slice every time, there's some necessary optimization here
		if err := c.cache.Get(ctx, key, groupcache.AllocatingByteSliceSink(&data)); err != nil {
			if errors.Is(err, ErrGroupcacheMiss) {
				missed = append(missed, key)
				continue
			}

			level.Error(c.logger).Log("msg", "unable to fetch from groupcache", "err", err)
			return found, values, missed, err
		}

		found = append(found, key)
		values = append(values, data)
	}

	return found, values, missed, nil // count everything as a cache miss for now
}

// Store implements Cache.
func (c *group) Store(ctx context.Context, keys []string, values [][]byte) error {
	var err error
	for i, key := range keys {
		if cacheErr := c.cache.Set(ctx, key, values[i], time.Time{}, true); cacheErr != nil {
			level.Warn(c.logger).Log("msg", "failed to put to groupcache", "err", cacheErr)
			err = cacheErr
		}
	}

	return err
}

// Stop implements Cache.
func (c *group) Stop() {
	c.wg.Done()
}

func (c *group) GetCacheType() stats.CacheType {
	return c.cacheType
}
