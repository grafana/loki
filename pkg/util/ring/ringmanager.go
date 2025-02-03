package ring

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	ringAutoForgetUnhealthyPeriods = 10
	RingCheckPeriod                = 3 * time.Second
)

// ManagerMode defines the different modes for the RingManager to execute.
//
// The RingManager and its modes are only relevant if the Bloom Gateway is running in ring mode.
type ManagerMode int

const (
	// ClientMode is the RingManager mode executed by Loki components that are clients of the Bloom Gateway.
	// The RingManager in client will have its own ring key-value store but it won't try to register itself in the ring.
	ClientMode ManagerMode = iota

	// ServerMode is the RingManager mode execute by the Bloom Gateway.
	// The RingManager in server mode will register itself in the ring.
	ServerMode
)

// RingManager is a component instantiated before all the others and is responsible for the ring setup.
//
// All Loki components that are involved with the Bloom Gateway (including the Bloom Gateway itself) will
// require a RingManager. However, the components that are clients of the Bloom Gateway will ran it in client
// mode while the Bloom Gateway itself will ran the manager in server mode.
type RingManager struct { // nolint:revive
	services.Service

	Mode ManagerMode

	name   string
	rf     int
	tokens int

	cfg    RingConfig
	logger log.Logger

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	RingLifecycler *ring.BasicLifecycler
	Ring           *ring.Ring
}

// NewRingManager instantiates a new RingManager instance.
// The other functions will assume the RingManager was instantiated through this function.
func NewRingManager(name string, mode ManagerMode, cfg RingConfig, rf int, tokens int, logger log.Logger, registerer prometheus.Registerer) (*RingManager, error) {
	rm := &RingManager{
		cfg: cfg, logger: logger, Mode: mode, name: name, tokens: tokens, rf: rf,
	}

	// instantiate kv store for both modes.
	ringStore, err := kv.NewClient(
		rm.cfg.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), fmt.Sprintf("%s-ring-manager", name)),
		rm.logger,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "%s ring manager create KV store client", name)
	}

	// instantiate ring for both mode modes.
	ringCfg := rm.cfg.ToRingConfig(rf)
	rm.Ring, err = ring.NewWithStoreClientAndStrategy(
		ringCfg,
		name,
		name,
		ringStore,
		ring.NewIgnoreUnhealthyInstancesReplicationStrategy(),
		prometheus.WrapRegistererWithPrefix("loki_", registerer),
		rm.logger,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "%s ring manager create ring client", name)
	}

	switch mode {
	case ServerMode:
		if err := rm.startServerMode(ringStore, registerer); err != nil {
			return nil, err
		}
	case ClientMode:
		if err := rm.startClientMode(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("starting %s ring in unsupported mode %v", name, mode)
	}

	return rm, nil
}

func (rm *RingManager) startServerMode(ringStore kv.Client, registerer prometheus.Registerer) error {
	lifecyclerCfg, err := rm.cfg.ToLifecyclerConfig(rm.tokens, rm.logger)
	if err != nil {
		return errors.Wrap(err, "invalid ring lifecycler config")
	}

	delegate := ring.BasicLifecyclerDelegate(rm)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, rm.logger)
	delegate = ring.NewTokensPersistencyDelegate(rm.cfg.TokensFilePath, ring.JOINING, delegate, rm.logger)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*rm.cfg.HeartbeatTimeout, delegate, rm.logger)

	rm.RingLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, rm.name, rm.name, ringStore, delegate, rm.logger, registerer)
	if err != nil {
		return errors.Wrapf(err, "%s ring manager create ring lifecycler", rm.name)
	}

	svcs := []services.Service{rm.RingLifecycler, rm.Ring}
	rm.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return errors.Wrapf(err, "new %s services manager in server mode", rm.name)
	}

	rm.subservicesWatcher = services.NewFailureWatcher()
	rm.subservicesWatcher.WatchManager(rm.subservices)
	rm.Service = services.NewBasicService(rm.starting, rm.running, rm.stopping)

	return nil
}

func (rm *RingManager) startClientMode() error {
	var err error

	svcs := []services.Service{rm.Ring}
	rm.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return errors.Wrapf(err, "new %s services manager in client mode", rm.name)
	}

	rm.subservicesWatcher = services.NewFailureWatcher()
	rm.subservicesWatcher.WatchManager(rm.subservices)

	rm.Service = services.NewIdleService(func(ctx context.Context) error {
		return services.StartManagerAndAwaitHealthy(ctx, rm.subservices)
	}, func(_ error) error {
		return services.StopManagerAndAwaitStopped(context.Background(), rm.subservices)
	})

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
			level.Error(rm.logger).Log("msg", "failed to gracefully stop ring manager dependencies", "name", rm.name, "err", stopErr)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, rm.subservices); err != nil {
		return errors.Wrapf(err, "unable to start %s ring manager subservices", rm.name)
	}

	// The BasicLifecycler does not automatically move state to ACTIVE such that any additional work that
	// someone wants to do can be done before becoming ACTIVE. For the bloom gateway we don't currently
	// have any additional work so we can become ACTIVE right away.
	// Wait until the ring client detected this instance in the JOINING
	// state to make sure that when we'll run the initial sync we already
	// know the tokens assigned to this instance.
	level.Info(rm.logger).Log("msg", fmt.Sprintf("waiting until %s is JOINING in the ring", rm.name))
	if err := ring.WaitInstanceState(ctx, rm.Ring, rm.RingLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(rm.logger).Log("msg", fmt.Sprintf("%s is JOINING in the ring", rm.name))

	if err = rm.RingLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
	}

	// Wait until the ring client detected this instance in the ACTIVE state to
	// make sure that when we'll run the loop it won't be detected as a ring
	// topology change.
	level.Info(rm.logger).Log("msg", fmt.Sprintf("waiting until %s is ACTIVE in the ring", rm.name))
	if err := ring.WaitInstanceState(ctx, rm.Ring, rm.RingLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
		return err
	}
	level.Info(rm.logger).Log("msg", fmt.Sprintf("%s is ACTIVE in the ring", rm.name))

	return nil
}

// running implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *RingManager) running(ctx context.Context) error {
	t := time.NewTicker(RingCheckPeriod)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-rm.subservicesWatcher.Chan():
			return errors.Wrapf(err, "running %s ring manager subservice failed", rm.name)
		case <-t.C:
			continue
		}
	}
}

// stopping implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *RingManager) stopping(_ error) error {
	level.Debug(rm.logger).Log("msg", fmt.Sprintf("stopping %s ring manager", rm.name))
	return services.StopManagerAndAwaitStopped(context.Background(), rm.subservices)
}

// ServeHTTP serves the HTTP route /bloomgateway/ring.
func (rm *RingManager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	rm.Ring.ServeHTTP(w, req)
}

func (rm *RingManager) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, _ string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	// When we initialize the index gateway instance in the ring we want to start from
	// a clean situation, so whatever is the state we set it JOINING, while we keep existing
	// tokens (if any) or the ones loaded from file.
	var tokens []uint32
	if instanceExists {
		tokens = instanceDesc.GetTokens()
	}

	takenTokens := ringDesc.GetTokens()
	gen := ring.NewRandomTokenGenerator()
	newTokens := gen.GenerateTokens(rm.tokens-len(tokens), takenTokens)

	// Tokens sorting will be enforced by the parent caller.
	tokens = append(tokens, newTokens...)

	return ring.JOINING, tokens
}

func (rm *RingManager) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {
}

func (rm *RingManager) OnRingInstanceStopping(_ *ring.BasicLifecycler) {
}

func (rm *RingManager) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}
