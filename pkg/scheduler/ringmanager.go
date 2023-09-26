package scheduler

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
	// ringAutoForgetUnhealthyPeriods is how many consecutive timeout periods an unhealthy instance
	// in the ring will be automatically removed.
	ringAutoForgetUnhealthyPeriods = 10

	// ringKey is the key under which we store the store gateways ring in the KVStore.
	ringKey = "scheduler"

	// ringNameForServer is the name of the ring used by the compactor server.
	ringNameForServer = "scheduler"

	// ringReplicationFactor should be 2 because we want 2 schedulers.
	ringReplicationFactor = 2

	// ringNumTokens sets our single token in the ring,
	// we only need to insert 1 token to be used for leader election purposes.
	ringNumTokens = 1

	// ringCheckPeriod is how often we check the ring to see if this instance is still in
	// the replicaset of instances to act as schedulers.
	ringCheckPeriod = 3 * time.Second
)

// RingManagerMode defines the different modes for the RingManager to execute.
//
// The RingManager and its modes are only relevant if the Scheduler discovery is done using ring.
type RingManagerMode int

const (
	// RingManagerModeReader is the RingManager mode executed by Loki components that want to discover Scheduler instances.
	// The RingManager in reader mode will have its own ring key-value store client, but it won't try to register itself in the ring.
	RingManagerModeReader RingManagerMode = iota

	// RingManagerModeMember is the RingManager mode execute by the Schedulers to register themselves in the ring.
	RingManagerModeMember
)

// RingManager is a component instantiated before all the others and is responsible for the ring setup.
//
// All Loki components that are involved with the Schedulers (including the Schedulers itself) will
// require a RingManager. However, the components that are clients of the Schedulers will run it in reader
// mode while the Schedulers itself will run the manager in member mode.
type RingManager struct {
	services.Service

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	RingLifecycler *ring.BasicLifecycler
	Ring           *ring.Ring
	managerMode    RingManagerMode

	cfg Config

	log log.Logger
}

// NewRingManager is the recommended way of instantiating a RingManager.
//
// The other functions will assume the RingManager was instantiated through this function.
func NewRingManager(managerMode RingManagerMode, cfg Config, log log.Logger, registerer prometheus.Registerer) (*RingManager, error) {
	rm := &RingManager{
		cfg: cfg, log: log, managerMode: managerMode,
	}

	if !cfg.UseSchedulerRing {
		return nil, fmt.Errorf("ring manager shouldn't be invoked when ring is not used for discovering schedulers")
	}

	// instantiate kv store for both modes.
	ringStore, err := kv.NewClient(
		rm.cfg.SchedulerRing.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "scheduler"),
		rm.log,
	)
	if err != nil {
		return nil, errors.Wrap(err, "scheduler ring manager create KV store client")
	}

	// instantiate ring for both mode modes.
	ringCfg := rm.cfg.SchedulerRing.ToRingConfig(ringReplicationFactor)
	rm.Ring, err = ring.NewWithStoreClientAndStrategy(
		ringCfg,
		ringNameForServer,
		ringKey,
		ringStore,
		ring.NewIgnoreUnhealthyInstancesReplicationStrategy(),
		prometheus.WrapRegistererWithPrefix("cortex_", registerer),
		rm.log,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ring client for scheduler ring manager")
	}

	if managerMode == RingManagerModeMember {
		if err := rm.startMemberMode(ringStore, registerer); err != nil {
			return nil, err
		}
		return rm, nil
	}

	if err := rm.startReaderMode(); err != nil {
		return nil, err
	}
	return rm, nil
}

func (rm *RingManager) startMemberMode(ringStore kv.Client, registerer prometheus.Registerer) error {
	lifecyclerCfg, err := rm.cfg.SchedulerRing.ToLifecyclerConfig(ringNumTokens, rm.log)
	if err != nil {
		return errors.Wrap(err, "invalid ring lifecycler config")
	}

	delegate := ring.BasicLifecyclerDelegate(rm)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, rm.log)
	delegate = ring.NewTokensPersistencyDelegate(rm.cfg.SchedulerRing.TokensFilePath, ring.JOINING, delegate, rm.log)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*rm.cfg.SchedulerRing.HeartbeatTimeout, delegate, rm.log)

	rm.RingLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, ringKey, ringStore, delegate, rm.log, registerer)
	if err != nil {
		return errors.Wrap(err, "failed to create ring lifecycler for scheduler ring manager")
	}

	svcs := []services.Service{rm.RingLifecycler, rm.Ring}
	rm.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return errors.Wrap(err, "failed to create services manager for scheduler ring manager in member mode")
	}

	rm.subservicesWatcher = services.NewFailureWatcher()
	rm.subservicesWatcher.WatchManager(rm.subservices)
	rm.Service = services.NewBasicService(rm.starting, rm.running, rm.stopping)

	return nil
}

func (rm *RingManager) startReaderMode() error {
	var err error

	svcs := []services.Service{rm.Ring}
	rm.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return errors.Wrap(err, "failed to create services manager for scheduler ring manager in reader mode")
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
			level.Error(rm.log).Log("msg", "failed to gracefully stop scheduler ring manager dependencies", "err", stopErr)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, rm.subservices); err != nil {
		return errors.Wrap(err, "unable to start scheduler ring manager subservices")
	}

	// The BasicLifecycler does not automatically move state to ACTIVE such that any additional work that
	// someone wants to do can be done before becoming ACTIVE. For the schedulers we don't currently
	// have any additional work so we can become ACTIVE right away.
	// Wait until the ring client detected this instance in the JOINING
	// state to make sure that when we'll run the initial sync we already
	// know the tokens assigned to this instance.
	level.Info(rm.log).Log("msg", "waiting until scheduler is JOINING in the ring")
	if err := ring.WaitInstanceState(ctx, rm.Ring, rm.RingLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "scheduler is JOINING in the ring")

	if err = rm.RingLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
	}

	// Wait until the ring client detected this instance in the ACTIVE state to
	// make sure that when we'll run the loop it won't be detected as a ring
	// topology change.
	level.Info(rm.log).Log("msg", "waiting until scheduler is ACTIVE in the ring")
	if err := ring.WaitInstanceState(ctx, rm.Ring, rm.RingLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
		return err
	}
	level.Info(rm.log).Log("msg", "scheduler is ACTIVE in the ring")

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
			return errors.Wrap(err, "running scheduler ring manager subservice failed")
		case <-t.C:
			continue
		}
	}
}

// stopping implements the Lifecycler interface and is one of the lifecycle hooks.
func (rm *RingManager) stopping(_ error) error {
	level.Debug(rm.log).Log("msg", "stopping scheduler ring manager")
	return services.StopManagerAndAwaitStopped(context.Background(), rm.subservices)
}

// ServeHTTP serves the HTTP route /scheduler/ring.
func (rm *RingManager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if rm.cfg.UseSchedulerRing {
		rm.Ring.ServeHTTP(w, req)
	} else {
		_, _ = w.Write([]byte("QueryScheduler running with '-query-scheduler.use-scheduler-ring' set to false."))
	}
}
