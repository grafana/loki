package storegateway

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/store/storepb"
	"github.com/weaveworks/common/logging"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/storage/bucket"
	cortex_tsdb "github.com/cortexproject/cortex/pkg/storage/tsdb"
	"github.com/cortexproject/cortex/pkg/storegateway/storegatewaypb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/services"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

const (
	syncReasonInitial    = "initial"
	syncReasonPeriodic   = "periodic"
	syncReasonRingChange = "ring-change"

	// sharedOptionWithQuerier is a message appended to all config options that should be also
	// set on the querier in order to work correct.
	sharedOptionWithQuerier = " This option needs be set both on the store-gateway and querier when running in microservices mode."

	// ringAutoForgetUnhealthyPeriods is how many consecutive timeout periods an unhealthy instance
	// in the ring will be automatically removed.
	ringAutoForgetUnhealthyPeriods = 10
)

var (
	supportedShardingStrategies = []string{util.ShardingStrategyDefault, util.ShardingStrategyShuffle}

	// Validation errors.
	errInvalidShardingStrategy = errors.New("invalid sharding strategy")
	errInvalidTenantShardSize  = errors.New("invalid tenant shard size, the value must be greater than 0")
)

// Config holds the store gateway config.
type Config struct {
	ShardingEnabled  bool       `yaml:"sharding_enabled"`
	ShardingRing     RingConfig `yaml:"sharding_ring" doc:"description=The hash ring configuration. This option is required only if blocks sharding is enabled."`
	ShardingStrategy string     `yaml:"sharding_strategy"`
}

// RegisterFlags registers the Config flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ShardingRing.RegisterFlags(f)

	f.BoolVar(&cfg.ShardingEnabled, "store-gateway.sharding-enabled", false, "Shard blocks across multiple store gateway instances."+sharedOptionWithQuerier)
	f.StringVar(&cfg.ShardingStrategy, "store-gateway.sharding-strategy", util.ShardingStrategyDefault, fmt.Sprintf("The sharding strategy to use. Supported values are: %s.", strings.Join(supportedShardingStrategies, ", ")))
}

// Validate the Config.
func (cfg *Config) Validate(limits validation.Limits) error {
	if cfg.ShardingEnabled {
		if !util.StringsContain(supportedShardingStrategies, cfg.ShardingStrategy) {
			return errInvalidShardingStrategy
		}

		if cfg.ShardingStrategy == util.ShardingStrategyShuffle && limits.StoreGatewayTenantShardSize <= 0 {
			return errInvalidTenantShardSize
		}
	}

	return nil
}

// StoreGateway is the Cortex service responsible to expose an API over the bucket
// where blocks are stored, supporting blocks sharding and replication across a pool
// of store gateway instances (optional).
type StoreGateway struct {
	services.Service

	gatewayCfg Config
	storageCfg cortex_tsdb.BlocksStorageConfig
	logger     log.Logger
	stores     *BucketStores

	// Ring used for sharding blocks.
	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring

	// Subservices manager (ring, lifecycler)
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	bucketSync *prometheus.CounterVec
}

func NewStoreGateway(gatewayCfg Config, storageCfg cortex_tsdb.BlocksStorageConfig, limits *validation.Overrides, logLevel logging.Level, logger log.Logger, reg prometheus.Registerer) (*StoreGateway, error) {
	var ringStore kv.Client

	bucketClient, err := createBucketClient(storageCfg, logger, reg)
	if err != nil {
		return nil, err
	}

	if gatewayCfg.ShardingEnabled {
		ringStore, err = kv.NewClient(
			gatewayCfg.ShardingRing.KVStore,
			ring.GetCodec(),
			kv.RegistererWithKVName(reg, "store-gateway"),
		)
		if err != nil {
			return nil, errors.Wrap(err, "create KV store client")
		}
	}

	return newStoreGateway(gatewayCfg, storageCfg, bucketClient, ringStore, limits, logLevel, logger, reg)
}

func newStoreGateway(gatewayCfg Config, storageCfg cortex_tsdb.BlocksStorageConfig, bucketClient objstore.Bucket, ringStore kv.Client, limits *validation.Overrides, logLevel logging.Level, logger log.Logger, reg prometheus.Registerer) (*StoreGateway, error) {
	var err error

	g := &StoreGateway{
		gatewayCfg: gatewayCfg,
		storageCfg: storageCfg,
		logger:     logger,
		bucketSync: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "cortex_storegateway_bucket_sync_total",
			Help: "Total number of times the bucket sync operation triggered.",
		}, []string{"reason"}),
	}

	// Init metrics.
	g.bucketSync.WithLabelValues(syncReasonInitial)
	g.bucketSync.WithLabelValues(syncReasonPeriodic)
	g.bucketSync.WithLabelValues(syncReasonRingChange)

	// Init sharding strategy.
	var shardingStrategy ShardingStrategy

	if gatewayCfg.ShardingEnabled {
		lifecyclerCfg, err := gatewayCfg.ShardingRing.ToLifecyclerConfig()
		if err != nil {
			return nil, errors.Wrap(err, "invalid ring lifecycler config")
		}

		// Define lifecycler delegates in reverse order (last to be called defined first because they're
		// chained via "next delegate").
		delegate := ring.BasicLifecyclerDelegate(g)
		delegate = ring.NewLeaveOnStoppingDelegate(delegate, logger)
		delegate = ring.NewTokensPersistencyDelegate(gatewayCfg.ShardingRing.TokensFilePath, ring.JOINING, delegate, logger)
		delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*gatewayCfg.ShardingRing.HeartbeatTimeout, delegate, logger)

		g.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, RingNameForServer, RingKey, ringStore, delegate, logger, reg)
		if err != nil {
			return nil, errors.Wrap(err, "create ring lifecycler")
		}

		ringCfg := gatewayCfg.ShardingRing.ToRingConfig()
		g.ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, RingNameForServer, RingKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy())
		if err != nil {
			return nil, errors.Wrap(err, "create ring client")
		}

		if reg != nil {
			reg.MustRegister(g.ring)
		}

		// Instance the right strategy.
		switch gatewayCfg.ShardingStrategy {
		case util.ShardingStrategyDefault:
			shardingStrategy = NewDefaultShardingStrategy(g.ring, lifecyclerCfg.Addr, logger)
		case util.ShardingStrategyShuffle:
			shardingStrategy = NewShuffleShardingStrategy(g.ring, lifecyclerCfg.ID, lifecyclerCfg.Addr, limits, logger)
		default:
			return nil, errInvalidShardingStrategy
		}
	} else {
		shardingStrategy = NewNoShardingStrategy()
	}

	g.stores, err = NewBucketStores(storageCfg, shardingStrategy, bucketClient, limits, logLevel, logger, extprom.WrapRegistererWith(prometheus.Labels{"component": "store-gateway"}, reg))
	if err != nil {
		return nil, errors.Wrap(err, "create bucket stores")
	}

	g.Service = services.NewBasicService(g.starting, g.running, g.stopping)

	return g, nil
}

func (g *StoreGateway) starting(ctx context.Context) (err error) {
	// In case this function will return error we want to unregister the instance
	// from the ring. We do it ensuring dependencies are gracefully stopped if they
	// were already started.
	defer func() {
		if err == nil || g.subservices == nil {
			return
		}

		if stopErr := services.StopManagerAndAwaitStopped(context.Background(), g.subservices); stopErr != nil {
			level.Error(g.logger).Log("msg", "failed to gracefully stop store-gateway dependencies", "err", stopErr)
		}
	}()

	if g.gatewayCfg.ShardingEnabled {
		// First of all we register the instance in the ring and wait
		// until the lifecycler successfully started.
		if g.subservices, err = services.NewManager(g.ringLifecycler, g.ring); err != nil {
			return errors.Wrap(err, "unable to start store-gateway dependencies")
		}

		g.subservicesWatcher = services.NewFailureWatcher()
		g.subservicesWatcher.WatchManager(g.subservices)

		if err = services.StartManagerAndAwaitHealthy(ctx, g.subservices); err != nil {
			return errors.Wrap(err, "unable to start store-gateway dependencies")
		}

		// Wait until the ring client detected this instance in the JOINING state to
		// make sure that when we'll run the initial sync we already know  the tokens
		// assigned to this instance.
		level.Info(g.logger).Log("msg", "waiting until store-gateway is JOINING in the ring")
		if err := ring.WaitInstanceState(ctx, g.ring, g.ringLifecycler.GetInstanceID(), ring.JOINING); err != nil {
			return err
		}
		level.Info(g.logger).Log("msg", "store-gateway is JOINING in the ring")
	}

	// At this point, if sharding is enabled, the instance is registered with some tokens
	// and we can run the initial synchronization.
	g.bucketSync.WithLabelValues(syncReasonInitial).Inc()
	if err = g.stores.InitialSync(ctx); err != nil {
		return errors.Wrap(err, "initial blocks synchronization")
	}

	if g.gatewayCfg.ShardingEnabled {
		// Now that the initial sync is done, we should have loaded all blocks
		// assigned to our shard, so we can switch to ACTIVE and start serving
		// requests.
		if err = g.ringLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
			return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
		}

		// Wait until the ring client detected this instance in the ACTIVE state to
		// make sure that when we'll run the loop it won't be detected as a ring
		// topology change.
		level.Info(g.logger).Log("msg", "waiting until store-gateway is ACTIVE in the ring")
		if err := ring.WaitInstanceState(ctx, g.ring, g.ringLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
			return err
		}
		level.Info(g.logger).Log("msg", "store-gateway is ACTIVE in the ring")
	}

	return nil
}

func (g *StoreGateway) running(ctx context.Context) error {
	var ringTickerChan <-chan time.Time
	var ringLastState ring.ReplicationSet

	// Apply a jitter to the sync frequency in order to increase the probability
	// of hitting the shared cache (if any).
	syncTicker := time.NewTicker(util.DurationWithJitter(g.storageCfg.BucketStore.SyncInterval, 0.2))
	defer syncTicker.Stop()

	if g.gatewayCfg.ShardingEnabled {
		ringLastState, _ = g.ring.GetAllHealthy(BlocksSync) // nolint:errcheck
		ringTicker := time.NewTicker(util.DurationWithJitter(g.gatewayCfg.ShardingRing.RingCheckPeriod, 0.2))
		defer ringTicker.Stop()
		ringTickerChan = ringTicker.C
	}

	for {
		select {
		case <-syncTicker.C:
			g.syncStores(ctx, syncReasonPeriodic)
		case <-ringTickerChan:
			// We ignore the error because in case of error it will return an empty
			// replication set which we use to compare with the previous state.
			currRingState, _ := g.ring.GetAllHealthy(BlocksSync) // nolint:errcheck

			if ring.HasReplicationSetChanged(ringLastState, currRingState) {
				ringLastState = currRingState
				g.syncStores(ctx, syncReasonRingChange)
			}
		case <-ctx.Done():
			return nil
		case err := <-g.subservicesWatcher.Chan():
			return errors.Wrap(err, "store gateway subservice failed")
		}
	}
}

func (g *StoreGateway) stopping(_ error) error {
	if g.subservices != nil {
		return services.StopManagerAndAwaitStopped(context.Background(), g.subservices)
	}
	return nil
}

func (g *StoreGateway) syncStores(ctx context.Context, reason string) {
	level.Info(g.logger).Log("msg", "synchronizing TSDB blocks for all users", "reason", reason)
	g.bucketSync.WithLabelValues(reason).Inc()

	if err := g.stores.SyncBlocks(ctx); err != nil {
		level.Warn(g.logger).Log("msg", "failed to synchronize TSDB blocks", "reason", reason, "err", err)
	} else {
		level.Info(g.logger).Log("msg", "successfully synchronized TSDB blocks for all users", "reason", reason)
	}
}

func (g *StoreGateway) Series(req *storepb.SeriesRequest, srv storegatewaypb.StoreGateway_SeriesServer) error {
	return g.stores.Series(req, srv)
}

// LabelNames implements the Storegateway proto service.
func (g *StoreGateway) LabelNames(ctx context.Context, req *storepb.LabelNamesRequest) (*storepb.LabelNamesResponse, error) {
	return g.stores.LabelNames(ctx, req)
}

// LabelValues implements the Storegateway proto service.
func (g *StoreGateway) LabelValues(ctx context.Context, req *storepb.LabelValuesRequest) (*storepb.LabelValuesResponse, error) {
	return g.stores.LabelValues(ctx, req)
}

func (g *StoreGateway) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, instanceID string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	// When we initialize the store-gateway instance in the ring we want to start from
	// a clean situation, so whatever is the state we set it JOINING, while we keep existing
	// tokens (if any) or the ones loaded from file.
	var tokens []uint32
	if instanceExists {
		tokens = instanceDesc.GetTokens()
	}

	takenTokens := ringDesc.GetTokens()
	newTokens := ring.GenerateTokens(RingNumTokens-len(tokens), takenTokens)

	// Tokens sorting will be enforced by the parent caller.
	tokens = append(tokens, newTokens...)

	return ring.JOINING, tokens
}

func (g *StoreGateway) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (g *StoreGateway) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (g *StoreGateway) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}

func createBucketClient(cfg cortex_tsdb.BlocksStorageConfig, logger log.Logger, reg prometheus.Registerer) (objstore.Bucket, error) {
	bucketClient, err := bucket.NewClient(context.Background(), cfg.Bucket, "store-gateway", logger, reg)
	if err != nil {
		return nil, errors.Wrap(err, "create bucket client")
	}

	return bucketClient, nil
}
