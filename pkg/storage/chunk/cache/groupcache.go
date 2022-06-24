package cache

import (
	"context"
	"flag"
	"time"

	util_log "github.com/grafana/loki/pkg/util/log"

	"github.com/go-kit/kit/log/level"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/logqlmodel/stats"

	"github.com/grafana/dskit/ring"
	"go.opencensus.io/plugin/ocgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/vimeo/galaxycache"
	gcgrpc "github.com/vimeo/galaxycache/grpc"
)

type GroupCache struct {
	peerRing       *ring.Ring
	universe       *galaxycache.Universe
	cache          *galaxycache.Galaxy
	cacheType      stats.CacheType
	stopChan       chan struct{}
	updateInterval time.Duration
	myUrl          string
	logger         log.Logger
	lifecycler     *ring.Lifecycler
}

const (
	GroupcacheRingKey  = "groupcacheRing"
	GroupcacheRingName = "groupcacheRing"
)

type GroupCacheConfig struct {
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`

	Cache *GroupCache
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *GroupCacheConfig) RegisterFlagsWithPrefix(prefix, _ string, f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix(prefix+"querier", f, util_log.Logger)
}

func NewGroupCache(ring *ring.Ring, lifecycler *ring.Lifecycler, logger log.Logger) (*GroupCache, error) {
	grpcProto := gcgrpc.NewGRPCFetchProtocol(grpc.WithTransportCredentials(insecure.NewCredentials()))
	u := galaxycache.NewUniverse(grpcProto, lifecycler.Addr)

	grpcServer := grpc.NewServer(grpc.StatsHandler(&ocgrpc.ServerHandler{}))
	gcgrpc.RegisterGRPCServer(u, grpcServer)

	cache := &GroupCache{
		peerRing:       ring,
		lifecycler:     lifecycler,
		universe:       u,
		logger:         logger,
		stopChan:       make(chan struct{}),
		updateInterval: 30 * time.Second,
	}

	return cache, nil
}

func (c *GroupCache) InitGroupCache(getter galaxycache.BackendGetter, ct stats.CacheType) {
	// TODO: configure the name
	c.cache = c.universe.NewGalaxy("the-cache", 1<<30, getter)
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

	if err := c.universe.Set(urls...); err != nil {
		level.Warn(c.logger).Log("msg", "error setting groupcache peer urls")
	}
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
	values := make([][]byte, len(keys))

	val := galaxycache.ByteCodec{}
	for _, key := range keys {
		if err := c.cache.Get(ctx, key, &val); err != nil {
			return nil, nil, nil, err
		}

		buf, err := val.MarshalBinary()
		if err != nil {
			return nil, nil, nil, err
		}

		values = append(values, buf)
	}

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

	if err := c.universe.Shutdown(); err != nil {
		level.Warn(c.logger).Log("msg", "error stopping groupcache")
	}
}

func (c *GroupCache) GetCacheType() stats.CacheType {
	return c.cacheType
}
