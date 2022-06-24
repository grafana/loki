package cache

import (
	"context"
	"flag"
	"time"

	"github.com/weaveworks/common/server"

	"github.com/mailgun/groupcache/v2"

	util_log "github.com/grafana/loki/pkg/util/log"

	"github.com/go-kit/kit/log/level"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/logqlmodel/stats"

	"github.com/grafana/dskit/ring"
	lokiutil "github.com/grafana/loki/pkg/util"
)

type GroupCache struct {
	peerRing       *ring.Ring
	cache          *groupcache.Group
	pool           *groupcache.HTTPPool
	cacheType      stats.CacheType
	stopChan       chan struct{}
	updateInterval time.Duration
	logger         log.Logger
}

const (
	GroupcacheRingKey  = "groupcacheRing"
	GroupcacheRingName = "groupcacheRing"
)

type GroupCacheConfig struct {
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`
	RingConfig       lokiutil.RingConfig   `yaml:"ring,omitempty"`
	Cache            *GroupCache           `yaml:"-"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *GroupCacheConfig) RegisterFlagsWithPrefix(prefix, _ string, f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix(prefix+"groupcache", f, util_log.Logger)
}

func NewGroupCache(ring *ring.Ring, lifecycler *ring.Lifecycler, server *server.Server, logger log.Logger) (*GroupCache, error) {
	pool := groupcache.NewHTTPPoolOpts(lifecycler.Addr, &groupcache.HTTPPoolOptions{})
	server.HTTP.Path("/_groupcache").Handler(pool)

	cache := &GroupCache{
		peerRing:       ring,
		pool:           pool,
		logger:         logger,
		stopChan:       make(chan struct{}),
		updateInterval: 30 * time.Second,
	}

	return cache, nil
}

func (c *GroupCache) InitGroupCache(name string, getter groupcache.GetterFunc, ct stats.CacheType) {
	c.cache = groupcache.NewGroup(name, 1<<30, getter)
	c.cacheType = ct

	go c.updatePeers()
}

func (c *GroupCache) updatePeers() {
	c.update()

	t := time.NewTimer(c.updateInterval)
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
		level.Warn(c.logger).Log("msg", "unable to get groupcache peer urls")
	}

	level.Info(c.logger).Log("msg", "got groupcache peers", "peers", urls)
	c.pool.Set(urls...)
}

func (c *GroupCache) peerUrls() ([]string, error) {
	replicationSet, err := c.peerRing.GetReplicationSetForOperation(ring.Write)
	if err != nil {
		return nil, err
	}

	var addrs []string
	for _, i := range replicationSet.Instances {
		addrs = append(addrs, i.Addr)
	}
	return addrs, nil
}

// Fetch gets the information from groupcache. Because groupcache is a read-through cache, there will never be missing
// chunks. If they're not already in groupcache, it'll go to the store and get them
// TODO: Issue the key requests in parallel, groupcache should be able to handle that
func (c *GroupCache) Fetch(ctx context.Context, keys []string) ([]string, [][]byte, []string, error) {
	// TODO: do the things
	//values := make([][]byte, len(keys))
	//
	//for _, key := range keys {
	//	if err := c.cache.Get(ctx, key, &val); err != nil {
	//		return nil, nil, nil, err
	//	}
	//
	//	buf, err := val.MarshalBinary()
	//	if err != nil {
	//		return nil, nil, nil, err
	//	}
	//
	//	values = append(values, buf)
	//}
	//
	return nil, nil, keys, nil // count everything as a cache miss for now
}

// Store implements Cache.
func (c *GroupCache) Store(ctx context.Context, keys []string, values [][]byte) error {
	// TODO: This is a NoOp in groupcache
	return nil
}

// Stop implements Cache.
func (c *GroupCache) Stop() {
	close(c.stopChan)
}

func (c *GroupCache) GetCacheType() stats.CacheType {
	return c.cacheType
}
