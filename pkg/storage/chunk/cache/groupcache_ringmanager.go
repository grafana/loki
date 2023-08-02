package cache

import (
	"context"
	"net/http"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/go-kit/log"
)

const (
	ringAutoForgetUnhealthyPeriods = 10
	ringNameForServer              = "groupcache"
	ringNumTokens                  = 1
	ringCheckPeriod                = 3 * time.Second

	GroupcacheRingKey = "groupcache"
)

// GroupcacheRingManager is a component instantiated before all the others and is responsible for the ring setup.
type GroupcacheRingManager struct {
	services.Service

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	addr           string
	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring

	cfg GroupCacheConfig

	log log.Logger
}

// NewRingManager is the recommended way of instantiating a GroupcacheRingManager.
//
// The other functions will assume the GroupcacheRingManager was instantiated through this function.
func NewgGroupcacheRingManager(cfg GroupCacheConfig, log log.Logger, registerer prometheus.Registerer) (*GroupcacheRingManager, error) {
	rm := &GroupcacheRingManager{
		cfg: cfg, log: log,
	}

	ringStore, err := kv.NewClient(
		rm.cfg.Ring.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "groupcache-ring-manager"),
		rm.log,
	)
	if err != nil {
		return nil, errors.Wrap(err, "groupcache ring manager create KV store client")
	}

	ringCfg := rm.cfg.Ring.ToRingConfig(1)
	rm.ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, GroupcacheRingKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("loki_", registerer), rm.log)
	if err != nil {
		return nil, errors.Wrap(err, "groupcache ring manager create ring client")
	}

	if err := rm.startRing(ringStore, registerer); err != nil {
		return nil, err
	}
	return rm, nil
}

func (rm *GroupcacheRingManager) startRing(ringStore kv.Client, registerer prometheus.Registerer) error {
	lifecyclerCfg, err := rm.cfg.Ring.ToLifecyclerConfig(ringNumTokens, rm.log)
	if err != nil {
		return errors.Wrap(err, "invalid ring lifecycler config")
	}
	rm.addr = lifecyclerCfg.Addr

	delegate := ring.BasicLifecyclerDelegate(rm)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, rm.log)
	delegate = ring.NewTokensPersistencyDelegate(rm.cfg.Ring.TokensFilePath, ring.JOINING, delegate, rm.log)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*rm.cfg.Ring.HeartbeatTimeout, delegate, rm.log)

	rm.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, GroupcacheRingKey, ringStore, delegate, rm.log, registerer)
	if err != nil {
		return errors.Wrap(err, "groupcache ring manager create ring lifecycler")
	}

	svcs := []services.Service{rm.ringLifecycler, rm.ring}
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
	if err := ring.WaitInstanceState(ctx, rm.ring, rm.ringLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "groupcache is JOINING in the ring")

	if err = rm.ringLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
	}

	// Wait until the ring client detected this instance in the ACTIVE state to
	// make sure that when we'll run the loop it won't be detected as a ring
	// topology change.
	level.Info(rm.log).Log("msg", "waiting until groupcache is ACTIVE in the ring")
	if err := ring.WaitInstanceState(ctx, rm.ring, rm.ringLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
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
	rm.ring.ServeHTTP(w, req)
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
	gen := ring.NewRandomTokenGenerator()
	newTokens := gen.GenerateTokens(ringNumTokens-len(tokens), takenTokens)

	// Tokens sorting will be enforced by the parent caller.
	tokens = append(tokens, newTokens...)

	return ring.JOINING, tokens
}

func (rm *GroupcacheRingManager) Addr() string {
	return rm.addr
}

func (rm *GroupcacheRingManager) Ring() ring.ReadRing {
	return rm.ring
}

func (rm *GroupcacheRingManager) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (rm *GroupcacheRingManager) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (rm *GroupcacheRingManager) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}
