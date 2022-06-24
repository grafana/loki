package singleflight

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
	ringNameForServer              = "singleflight"
	ringNumTokens                  = 1
	ringCheckPeriod                = 3 * time.Second

	SingleFlightRingKey = "singleflight"
)

// RingManager is a component instantiated before all the others and is responsible for the ring setup.
type RingManager struct {
	services.Service

	SingleFlight *SingleFlight

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	addr           string
	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring

	cfg Config

	log log.Logger
}

// NewRingManager is the recommended way of instantiating a RingManager.
//
// The other functions will assume the RingManager was instantiated through this function.
func NewRingManager(cfg Config, log log.Logger, registerer prometheus.Registerer) (*RingManager, error) {
	rm := &RingManager{
		cfg: cfg, log: log,
	}

	ringStore, err := kv.NewClient(
		rm.cfg.Ring.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "singleflight-ring-manager"),
		rm.log,
	)
	if err != nil {
		return nil, errors.Wrap(err, "singleflight ring manager create KV store client")
	}

	ringCfg := rm.cfg.Ring.ToRingConfig(1)
	rm.ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, SingleFlightRingKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("loki_", registerer), rm.log)
	if err != nil {
		return nil, errors.Wrap(err, "singleflight ring manager create ring client")
	}

	if err := rm.startRing(ringStore, registerer); err != nil {
		return nil, err
	}
	return rm, nil
}

func (rm *RingManager) startRing(ringStore kv.Client, registerer prometheus.Registerer) error {
	lifecyclerCfg, err := rm.cfg.Ring.ToLifecyclerConfig(ringNumTokens, rm.log)
	if err != nil {
		return errors.Wrap(err, "invalid ring lifecycler config")
	}
	rm.addr = lifecyclerCfg.Addr

	delegate := ring.BasicLifecyclerDelegate(rm)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, rm.log)
	delegate = ring.NewTokensPersistencyDelegate(rm.cfg.Ring.TokensFilePath, ring.JOINING, delegate, rm.log)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*rm.cfg.Ring.HeartbeatTimeout, delegate, rm.log)

	rm.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, SingleFlightRingKey, ringStore, delegate, rm.log, registerer)
	if err != nil {
		return errors.Wrap(err, "singleflight ring manager create ring lifecycler")
	}

	svcs := []services.Service{rm.ringLifecycler, rm.ring}
	rm.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return errors.Wrap(err, "new singleflight services manager in server mode")
	}

	rm.subservicesWatcher = services.NewFailureWatcher()
	rm.subservicesWatcher.WatchManager(rm.subservices)
	rm.Service = services.NewBasicService(rm.starting, rm.running, rm.stopping)

	return nil
}

// starting implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *RingManager) starting(ctx context.Context) (err error) {
	// In case this function will return error we want to unregister the instance
	// from the ring. We do it ensuring dependencies are gracefully stopped if they
	// were already started.
	defer func() {
		if err == nil || rm.subservices == nil {
			return
		}

		if stopErr := services.StopManagerAndAwaitStopped(context.Background(), rm.subservices); stopErr != nil {
			level.Error(rm.log).Log("msg", "failed to gracefully stop singleflight ring manager dependencies", "err", stopErr)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, rm.subservices); err != nil {
		return errors.Wrap(err, "unable to start singleflight ring manager subservices")
	}

	// The BasicLifecycler does not automatically move state to ACTIVE such that any additional work that
	// someone wants to do can be done before becoming ACTIVE. For singleflight we don't currently
	// have any additional work so we can become ACTIVE right away.
	// Wait until the ring client detected this instance in the JOINING
	// state to make sure that when we'll run the initial sync we already
	// know the tokens assigned to this instance.
	level.Info(rm.log).Log("msg", "waiting until singleflight is JOINING in the ring")
	if err := ring.WaitInstanceState(ctx, rm.ring, rm.ringLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "singleflight is JOINING in the ring")

	if err = rm.ringLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
	}

	// Wait until the ring client detected this instance in the ACTIVE state to
	// make sure that when we'll run the loop it won't be detected as a ring
	// topology change.
	level.Info(rm.log).Log("msg", "waiting until singleflight is ACTIVE in the ring")
	if err := ring.WaitInstanceState(ctx, rm.ring, rm.ringLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "singleflight is ACTIVE in the ring")

	return nil
}

// running implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *RingManager) running(ctx context.Context) error {
	t := time.NewTicker(ringCheckPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-rm.subservicesWatcher.Chan():
			return errors.Wrap(err, "running singleflight ring manager subservice failed")
		case <-t.C:
			continue
		}
	}
}

// stopping implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *RingManager) stopping(_ error) error {
	level.Debug(rm.log).Log("msg", "stopping singleflight")

	_ = rm.SingleFlight.Stop()
	return services.StopManagerAndAwaitStopped(context.Background(), rm.subservices)
}

// ServeHTTP serves the HTTP route /singleflight/ring.
func (rm *RingManager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	rm.ring.ServeHTTP(w, req)
}

func (rm *RingManager) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, instanceID string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	// When we initialize the singleflight instance in the ring we want to start from
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

func (rm *RingManager) Addr() string {
	return rm.addr
}

func (rm *RingManager) Ring() ring.ReadRing {
	return rm.ring
}

func (rm *RingManager) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (rm *RingManager) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (rm *RingManager) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}
