package cache

import (
	"context"
	"flag"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/weaveworks/common/server"

	"github.com/mailgun/groupcache/v2"

	"github.com/go-kit/kit/log/level"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/logqlmodel/stats"

	"github.com/grafana/dskit/ring"
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
	peerRing       *ring.Ring
	cache          *groupcache.Group
	pool           *groupcache.HTTPPool
	cacheType      stats.CacheType
	stopChan       chan struct{}
	updateInterval time.Duration
	logger         log.Logger
	listenPort     int
	wg             sync.WaitGroup
}

// RingCfg is a wrapper for the Groupcache ring configuration plus the replication factor.
type RingCfg struct {
	lokiutil.RingConfig `yaml:",inline"`
}

type GroupCacheConfig struct {
	Ring RingCfg `yaml:"ring,omitempty"`

	Cache *GroupCache `yaml:"-"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *GroupCacheConfig) RegisterFlagsWithPrefix(prefix, _ string, f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix(prefix, "groupcache", f)
}

func NewGroupCache(rm *GroupcacheRingManager, server *server.Server, logger log.Logger) (*GroupCache, error) {
	pool := groupcache.NewHTTPPoolOpts(rm.Addr, &groupcache.HTTPPoolOptions{})
	server.HTTP.Path("/_groupcache").Handler(pool)

	cache := &GroupCache{
		peerRing:       rm.Ring,
		pool:           pool,
		logger:         logger,
		stopChan:       make(chan struct{}),
		updateInterval: 5 * time.Minute,
		wg:             sync.WaitGroup{},
	}

	go cache.updatePeers()
	go func() {
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

func (c *GroupCache) NewGroup(name string, ct stats.CacheType) Cache {
	// Return a known error on miss to track which keys need to be inserted
	missGetter := groupcache.GetterFunc(func(_ context.Context, _ string, _ groupcache.Sink) error {
		return ErrGroupcacheMiss
	})

	c.wg.Add(1)
	return &group{
		cache:     groupcache.NewGroup(name, 1<<30, missGetter),
		logger:    c.logger,
		wg:        &c.wg,
		cacheType: ct,
	}
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
		// bool is for putting things in the hotcache. Should we?
		if cacheErr := c.cache.Set(ctx, key, values[i], time.Time{}, false); cacheErr != nil {
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

// GroupcacheRingManager is a component instantiated before all the others and is responsible for the ring setup.
type GroupcacheRingManager struct {
	services.Service

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	Addr           string
	RingLifecycler *ring.BasicLifecycler
	Ring           *ring.Ring

	cfg Config

	log log.Logger
}

// NewRingManager is the recommended way of instantiating a GroupcacheRingManager.
//
// The other functions will assume the GroupcacheRingManager was instantiated through this function.
func NewgGroupcacheRingManager(cfg Config, log log.Logger, registerer prometheus.Registerer) (*GroupcacheRingManager, error) {
	rm := &GroupcacheRingManager{
		cfg: cfg, log: log,
	}

	// instantiate kv store for both modes.
	ringStore, err := kv.NewClient(
		rm.cfg.GroupCache.Ring.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "groupcache-ring-manager"),
		rm.log,
	)
	if err != nil {
		return nil, errors.Wrap(err, "groupcache ring manager create KV store client")
	}

	// instantiate ring for both mode modes.
	ringCfg := rm.cfg.GroupCache.Ring.ToRingConfig(1)
	rm.Ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, GroupcacheRingKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("loki_", registerer), rm.log)
	if err != nil {
		return nil, errors.Wrap(err, "groupcache ring manager create ring client")
	}

	if err := rm.startServerMode(ringStore, registerer); err != nil {
		return nil, err
	}
	return rm, nil
}

func (rm *GroupcacheRingManager) startServerMode(ringStore kv.Client, registerer prometheus.Registerer) error {
	lifecyclerCfg, err := rm.cfg.GroupCache.Ring.ToLifecyclerConfig(ringNumTokens, rm.log)
	if err != nil {
		return errors.Wrap(err, "invalid ring lifecycler config")
	}
	rm.Addr = lifecyclerCfg.Addr

	delegate := ring.BasicLifecyclerDelegate(rm)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, rm.log)
	delegate = ring.NewTokensPersistencyDelegate(rm.cfg.GroupCache.Ring.TokensFilePath, ring.JOINING, delegate, rm.log)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*rm.cfg.GroupCache.Ring.HeartbeatTimeout, delegate, rm.log)

	rm.RingLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, GroupcacheRingKey, ringStore, delegate, rm.log, registerer)
	if err != nil {
		return errors.Wrap(err, "groupcache ring manager create ring lifecycler")
	}

	svcs := []services.Service{rm.RingLifecycler, rm.Ring}
	rm.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return errors.Wrap(err, "new groupcache services manager in server mode")
	}

	rm.subservicesWatcher = services.NewFailureWatcher()
	rm.subservicesWatcher.WatchManager(rm.subservices)
	rm.Service = services.NewBasicService(rm.starting, rm.running, rm.stopping)

	return nil
}

// starting implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *GroupcacheRingManager) starting(ctx context.Context) (err error) {
	// In case this function will return error we want to unregister the instance
	// from the ring. We do it ensuring dependencies are gracefully stopped if they
	// were already started.
	defer func() {
		if err == nil || rm.subservices == nil {
			return
		}

		if stopErr := services.StopManagerAndAwaitStopped(context.Background(), rm.subservices); stopErr != nil {
			level.Error(rm.log).Log("msg", "failed to gracefully stop groupcache ring manager dependencies", "err", stopErr)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, rm.subservices); err != nil {
		return errors.Wrap(err, "unable to start groupcache ring manager subservices")
	}

	// The BasicLifecycler does not automatically move state to ACTIVE such that any additional work that
	// someone wants to do can be done before becoming ACTIVE. For groupcache we don't currently
	// have any additional work so we can become ACTIVE right away.
	// Wait until the ring client detected this instance in the JOINING
	// state to make sure that when we'll run the initial sync we already
	// know the tokens assigned to this instance.
	level.Info(rm.log).Log("msg", "waiting until groupcache is JOINING in the ring")
	if err := ring.WaitInstanceState(ctx, rm.Ring, rm.RingLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "groupcache is JOINING in the ring")

	if err = rm.RingLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
	}

	// Wait until the ring client detected this instance in the ACTIVE state to
	// make sure that when we'll run the loop it won't be detected as a ring
	// topology change.
	level.Info(rm.log).Log("msg", "waiting until groupcache is ACTIVE in the ring")
	if err := ring.WaitInstanceState(ctx, rm.Ring, rm.RingLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "groupcache is ACTIVE in the ring")

	return nil
}

// running implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *GroupcacheRingManager) running(ctx context.Context) error {
	t := time.NewTicker(ringCheckPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-rm.subservicesWatcher.Chan():
			return errors.Wrap(err, "running groupcache ring manager subservice failed")
		case <-t.C:
			continue
		}
	}
}

// stopping implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *GroupcacheRingManager) stopping(_ error) error {
	level.Debug(rm.log).Log("msg", "stopping groupcache ring manager")
	return services.StopManagerAndAwaitStopped(context.Background(), rm.subservices)
}

// ServeHTTP serves the HTTP route /groupcache/ring.
func (rm *GroupcacheRingManager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	rm.Ring.ServeHTTP(w, req)
}

func (rm *GroupcacheRingManager) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, instanceID string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	// When we initialize the groupcache instance in the ring we want to start from
	// a clean situation, so whatever is the state we set it JOINING, while we keep existing
	// tokens (if any) or the ones loaded from file.
	var tokens []uint32
	if instanceExists {
		tokens = instanceDesc.GetTokens()
	}

	takenTokens := ringDesc.GetTokens()
	newTokens := ring.GenerateTokens(ringNumTokens-len(tokens), takenTokens)

	// Tokens sorting will be enforced by the parent caller.
	tokens = append(tokens, newTokens...)

	return ring.JOINING, tokens
}

func (rm *GroupcacheRingManager) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (rm *GroupcacheRingManager) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (rm *GroupcacheRingManager) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}
