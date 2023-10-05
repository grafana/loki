package indexgateway

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
	ringNameForServer              = "index-gateway"
	ringNumTokens                  = 128
	ringCheckPeriod                = 3 * time.Second

	// RingIdentifier is used as a unique name to register the Index Gateway ring.
	RingIdentifier = "index-gateway"

	// RingKey is the name of the key used to register the different Index Gateway instances in the key-value store.
	RingKey = "index-gateway"
)

// ManagerMode defines the different modes for the RingManager to execute.
//
// The RingManager and its modes are only relevant if the IndexGateway is running in ring mode.
type ManagerMode int

const (
	// ClientMode is the RingManager mode executed by Loki components that are clients of the IndexGateway.
	// The RingManager in client will have its own ring key-value store but it won't try to register itself in the ring.
	ClientMode ManagerMode = iota

	// ServerMode is the RingManager mode execute by the IndexGateway.
	// The RingManager in server mode will register itself in the ring.
	ServerMode
)

// RingManager is a component instantiated before all the others and is responsible for the ring setup.
//
// All Loki components that are involved with the IndexGateway (including the IndexGateway itself) will
// require a RingManager. However, the components that are clients of the IndexGateway will ran it in client
// mode while the IndexGateway itself will ran the manager in server mode.
type RingManager struct {
	services.Service

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	RingLifecycler *ring.BasicLifecycler
	Ring           *ring.Ring
	Mode           ManagerMode

	cfg Config

	log log.Logger
}

// NewRingManager is the recommended way of instantiating a RingManager.
//
// The other functions will assume the RingManager was instantiated through this function.
func NewRingManager(mode ManagerMode, cfg Config, log log.Logger, registerer prometheus.Registerer) (*RingManager, error) {
	rm := &RingManager{
		cfg: cfg, log: log, Mode: mode,
	}

	if cfg.Mode != RingMode {
		return nil, fmt.Errorf("ring manager shouldn't be invoked when index gateway not in ring mode")
	}

	// instantiate kv store for both modes.
	ringStore, err := kv.NewClient(
		rm.cfg.Ring.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "index-gateway-ring-manager"),
		rm.log,
	)
	if err != nil {
		return nil, errors.Wrap(err, "index gateway ring manager create KV store client")
	}

	// instantiate ring for both mode modes.
	ringCfg := rm.cfg.Ring.ToRingConfig(rm.cfg.Ring.ReplicationFactor)
	rm.Ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, RingKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("loki_", registerer), rm.log)
	if err != nil {
		return nil, errors.Wrap(err, "index gateway ring manager create ring client")
	}

	if mode == ServerMode {
		if err := rm.startServerMode(ringStore, registerer); err != nil {
			return nil, err
		}
		return rm, nil
	}

	if err := rm.startClientMode(); err != nil {
		return nil, err
	}
	return rm, nil
}

func (rm *RingManager) startServerMode(ringStore kv.Client, registerer prometheus.Registerer) error {
	lifecyclerCfg, err := rm.cfg.Ring.ToLifecyclerConfig(ringNumTokens, rm.log)
	if err != nil {
		return errors.Wrap(err, "invalid ring lifecycler config")
	}

	delegate := ring.BasicLifecyclerDelegate(rm)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, rm.log)
	delegate = ring.NewTokensPersistencyDelegate(rm.cfg.Ring.TokensFilePath, ring.JOINING, delegate, rm.log)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*rm.cfg.Ring.HeartbeatTimeout, delegate, rm.log)

	rm.RingLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, RingKey, ringStore, delegate, rm.log, registerer)
	if err != nil {
		return errors.Wrap(err, "index gateway ring manager create ring lifecycler")
	}

	svcs := []services.Service{rm.RingLifecycler, rm.Ring}
	rm.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return errors.Wrap(err, "new index gateway services manager in server mode")
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
		return errors.Wrap(err, "new index gateway services manager in client mode")
	}

	rm.subservicesWatcher = services.NewFailureWatcher()
	rm.subservicesWatcher.WatchManager(rm.subservices)

	rm.Service = services.NewIdleService(func(ctx context.Context) error {
		return services.StartManagerAndAwaitHealthy(ctx, rm.subservices)
	}, func(failureCase error) error {
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
			level.Error(rm.log).Log("msg", "failed to gracefully stop index gateway ring manager dependencies", "err", stopErr)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, rm.subservices); err != nil {
		return errors.Wrap(err, "unable to start index gateway ring manager subservices")
	}

	// The BasicLifecycler does not automatically move state to ACTIVE such that any additional work that
	// someone wants to do can be done before becoming ACTIVE. For the index gateway we don't currently
	// have any additional work so we can become ACTIVE right away.
	// Wait until the ring client detected this instance in the JOINING
	// state to make sure that when we'll run the initial sync we already
	// know the tokens assigned to this instance.
	level.Info(rm.log).Log("msg", "waiting until index gateway is JOINING in the ring")
	if err := ring.WaitInstanceState(ctx, rm.Ring, rm.RingLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "index gateway is JOINING in the ring")

	if err = rm.RingLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
	}

	// Wait until the ring client detected this instance in the ACTIVE state to
	// make sure that when we'll run the loop it won't be detected as a ring
	// topology change.
	level.Info(rm.log).Log("msg", "waiting until index gateway is ACTIVE in the ring")
	if err := ring.WaitInstanceState(ctx, rm.Ring, rm.RingLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "index gateway is ACTIVE in the ring")

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
			return errors.Wrap(err, "running index gateway ring manager subservice failed")
		case <-t.C:
			continue
		}
	}
}

// stopping implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *RingManager) stopping(_ error) error {
	level.Debug(rm.log).Log("msg", "stopping index gateway ring manager")
	return services.StopManagerAndAwaitStopped(context.Background(), rm.subservices)
}

// ServeHTTP serves the HTTP route /indexgateway/ring.
func (rm *RingManager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if rm.cfg.Mode == RingMode {
		rm.Ring.ServeHTTP(w, req)
	} else {
		_, _ = w.Write([]byte("IndexGateway running with 'useIndexGatewayRing' disabled."))
	}
}
