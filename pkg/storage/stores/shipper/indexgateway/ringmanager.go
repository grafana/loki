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
	loki_util "github.com/grafana/loki/pkg/util"
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

type ringTenantBuffers struct {
	bufDescs []ring.InstanceDesc
	bufHosts []string
	bufZones []string
}

type RingManager struct {
	services.Service

	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	RingLifecycler *ring.BasicLifecycler
	Ring           *ring.Ring

	buffers ringTenantBuffers
	cfg     Config

	log log.Logger
}

func NewRingManager(cfg Config, log log.Logger, registerer prometheus.Registerer) (*RingManager, error) {
	rm := &RingManager{
		cfg: cfg, log: log,
	}

	if cfg.Mode != RingMode {
		return rm, nil
	}

	ringStore, err := kv.NewClient(
		cfg.Ring.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", registerer), "index-gateway"),
		log,
	)
	if err != nil {
		return nil, errors.Wrap(err, "create KV store client")
	}

	lifecyclerCfg, err := cfg.Ring.ToLifecyclerConfig(ringNumTokens, log)
	if err != nil {
		return nil, errors.Wrap(err, "invalid ring lifecycler config")
	}

	delegate := ring.BasicLifecyclerDelegate(rm)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, log)
	delegate = ring.NewTokensPersistencyDelegate(cfg.Ring.TokensFilePath, ring.JOINING, delegate, log)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*cfg.Ring.HeartbeatTimeout, delegate, log)

	rm.RingLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, RingKey, ringStore, delegate, log, registerer)
	if err != nil {
		return nil, errors.Wrap(err, "index gateway create ring lifecycler")
	}

	ringCfg := cfg.Ring.ToRingConfig(cfg.Ring.ReplicationFactor)
	rm.Ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, RingKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("loki_", registerer), log)
	if err != nil {
		return nil, errors.Wrap(err, "index gateway create ring client")
	}

	svcs := []services.Service{rm.RingLifecycler, rm.Ring}
	rm.subservices, err = services.NewManager(svcs...)
	if err != nil {
		return nil, fmt.Errorf("new index gateway services manager: %w", err)
	}

	rm.subservicesWatcher = services.NewFailureWatcher()
	rm.subservicesWatcher.WatchManager(rm.subservices)
	rm.Service = services.NewBasicService(rm.starting, rm.running, rm.stopping)
	rm.buffers.bufDescs, rm.buffers.bufHosts, rm.buffers.bufZones = ring.MakeBuffersForGet()

	return rm, nil
}

// starting implements the Lifecycler interface and is one of the lifecycle hooks.
//
// Only invoked if the Index Gateway is in ring mode.
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
//
// Only invoked if the Index Gateway is in ring mode.
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
//
// Only invoked if the Index Gateway is in ring mode.
func (rm *RingManager) stopping(_ error) error {
	level.Debug(rm.log).Log("msg", "stopping index gateway ring manager")
	return services.StopManagerAndAwaitStopped(context.Background(), rm.subservices)
}

func (rm *RingManager) TenantInBoundaries(tenant string) bool {
	if rm.cfg.Mode != RingMode {
		return true
	}

	return loki_util.IsAssignedKey(rm.Ring, rm.RingLifecycler, tenant, rm.buffers.bufDescs, rm.buffers.bufHosts, rm.buffers.bufZones)
}

// ServeHTTP serves the HTTP route /indexgateway/ring.
func (rm *RingManager) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if rm.cfg.Mode == RingMode {
		rm.Ring.ServeHTTP(w, req)
	} else {
		w.Write([]byte("IndexGateway running with 'useIndexGatewayRing' disabled."))
	}
}
