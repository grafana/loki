package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/jobqueue"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

// Here is how the generic compactor works:
// 1. Find the index type from table name using schemaPeriodForTable.
// 2. Find the registered IndexCompactor for the index type.
// 3. Build an instance of TableCompactor using IndexCompactor.NewIndexCompactor, with all the required information to do a compaction.
// 4. Run the compaction using TableCompactor.Compact, which would set the new/updated CompactedIndex for each IndexSet.
// 5. If retention is enabled, run retention on the CompactedIndex using its retention.IndexProcessor implementation.
// 6. Convert the CompactedIndex to a file using the IndexCompactor.ToIndexFile for uploading.
// 7. If we uploaded successfully, delete the old index files.

const (
	// ringAutoForgetUnhealthyPeriods is how many consecutive timeout periods an unhealthy instance
	// in the ring will be automatically removed.
	ringAutoForgetUnhealthyPeriods = 10

	// ringKey is the key under which we store the store gateways ring in the KVStore.
	ringKey = "compactor"

	// ringNameForServer is the name of the ring used by the compactor server.
	ringNameForServer = "compactor"

	// ringKeyOfLeader is a somewhat arbitrary ID to pull from the ring to see who will be elected the leader
	ringKeyOfLeader = 0

	// ringReplicationFactor should be 1 because we only want to pull back one node from the Ring
	ringReplicationFactor = 1

	// ringNumTokens sets our single token in the ring,
	// we only need to insert 1 token to be used for leader election purposes.
	ringNumTokens = 1
)

const (
	HorizontalScalingModeDisabled = "disabled"
	HorizontalScalingModeMain     = "main"
	HorizontalScalingModeWorker   = "worker"
)

var (
	retentionEnabledStats = analytics.NewString("compactor_retention_enabled")
	defaultRetentionStats = analytics.NewString("compactor_default_retention")

	errSchemaForTableNotFound = errors.New("schema for table not found")
)

type Compactor struct {
	services.Service

	cfg                       Config
	indexStorageClient        storage.Client
	tableMarker               retention.TableMarker
	sweeper                   *retention.Sweeper
	deleteRequestsStore       deletion.DeleteRequestsStore
	DeleteRequestsHandler     *deletion.DeleteRequestHandler
	DeleteRequestsGRPCHandler *deletion.GRPCRequestHandler
	deleteRequestsManager     *deletion.DeleteRequestsManager
	expirationChecker         retention.ExpirationChecker
	metrics                   *metrics
	running                   bool
	wg                        sync.WaitGroup
	indexCompactors           map[string]IndexCompactor
	schemaConfig              config.SchemaConfig
	limits                    Limits
	JobQueue                  *jobqueue.Queue

	tablesManager *tablesManager
	// Ring used for running a single compactor
	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring
	ringPollPeriod time.Duration

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// one for each period
	storeContainers map[config.DayTime]storeContainer
}

type storeContainer struct {
	tableMarker        retention.TableMarker
	sweeper            *retention.Sweeper
	indexStorageClient storage.Client
}

type Limits interface {
	deletion.Limits
	retention.Limits
	DefaultLimits() *validation.Limits
}

func NewCompactor(
	cfg Config,
	objectStoreClients map[config.DayTime]client.ObjectClient,
	deleteStoreClient client.ObjectClient,
	schemaConfig config.SchemaConfig,
	limits Limits,
	indexUpdatePropagationMaxDelay time.Duration,
	r prometheus.Registerer,
	metricsNamespace string,
) (*Compactor, error) {
	retentionEnabledStats.Set("false")
	if cfg.RetentionEnabled {
		retentionEnabledStats.Set("true")
	}
	if limits != nil {
		defaultRetentionStats.Set(limits.DefaultLimits().RetentionPeriod.String())
	}

	compactor := &Compactor{
		cfg:             cfg,
		ringPollPeriod:  5 * time.Second,
		indexCompactors: map[string]IndexCompactor{},
		schemaConfig:    schemaConfig,
		limits:          limits,
	}

	ringStore, err := kv.NewClient(
		cfg.CompactorRing.KVStore,
		ring.GetCodec(),
		kv.RegistererWithKVName(prometheus.WrapRegistererWithPrefix("loki_", r), "compactor"),
		util_log.Logger,
	)
	if err != nil {
		return nil, errors.Wrap(err, "create KV store client")
	}
	lifecyclerCfg, err := cfg.CompactorRing.ToLifecyclerConfig(ringNumTokens, util_log.Logger)
	if err != nil {
		return nil, errors.Wrap(err, "invalid ring lifecycler config")
	}

	// Define lifecycler delegates in reverse order (last to be called defined first because they're
	// chained via "next delegate").
	delegate := ring.BasicLifecyclerDelegate(compactor)
	delegate = ring.NewLeaveOnStoppingDelegate(delegate, util_log.Logger)
	delegate = ring.NewTokensPersistencyDelegate(cfg.CompactorRing.TokensFilePath, ring.JOINING, delegate, util_log.Logger)
	delegate = ring.NewAutoForgetDelegate(ringAutoForgetUnhealthyPeriods*cfg.CompactorRing.HeartbeatTimeout, delegate, util_log.Logger)

	compactor.ringLifecycler, err = ring.NewBasicLifecycler(lifecyclerCfg, ringNameForServer, ringKey, ringStore, delegate, util_log.Logger, r)
	if err != nil {
		return nil, errors.Wrap(err, "create ring lifecycler")
	}

	ringCfg := cfg.CompactorRing.ToRingConfig(ringReplicationFactor)
	compactor.ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, ringKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", r), util_log.Logger)
	if err != nil {
		return nil, errors.Wrap(err, "create ring client")
	}

	compactor.subservices, err = services.NewManager(compactor.ringLifecycler, compactor.ring)
	if err != nil {
		return nil, err
	}
	compactor.subservicesWatcher = services.NewFailureWatcher()
	compactor.subservicesWatcher.WatchManager(compactor.subservices)

	if err := compactor.init(objectStoreClients, deleteStoreClient, schemaConfig, indexUpdatePropagationMaxDelay, limits, r); err != nil {
		return nil, fmt.Errorf("init compactor: %w", err)
	}

	compactor.Service = services.NewBasicService(compactor.starting, compactor.loop, compactor.stopping)
	return compactor, nil
}

func (c *Compactor) init(
	objectStoreClients map[config.DayTime]client.ObjectClient,
	deleteStoreClient client.ObjectClient,
	schemaConfig config.SchemaConfig,
	indexUpdatePropagationMaxDelay time.Duration,
	limits Limits,
	r prometheus.Registerer,
) error {
	err := chunk_util.EnsureDirectory(c.cfg.WorkingDirectory)
	if err != nil {
		return err
	}

	if c.cfg.RetentionEnabled {
		if deleteStoreClient == nil {
			return fmt.Errorf("delete store client not initialised when retention is enabled")
		}

		if err := c.initDeletes(deleteStoreClient, indexUpdatePropagationMaxDelay, r, limits); err != nil {
			return fmt.Errorf("failed to init delete store: %w", err)
		}
	} else {
		c.expirationChecker = retention.NeverExpiringExpirationChecker(limits)
	}

	legacyMarkerDirs := make(map[string]struct{})
	c.storeContainers = make(map[config.DayTime]storeContainer, len(objectStoreClients))
	for from, objectClient := range objectStoreClients {
		period, err := schemaConfig.SchemaForTime(from.Time)
		if err != nil {
			return err
		}

		var sc storeContainer
		sc.indexStorageClient = storage.NewIndexStorageClient(objectClient, period.IndexTables.PathPrefix)

		if c.cfg.RetentionEnabled {
			var (
				raw              client.ObjectClient
				encoder          client.KeyEncoder
				name             = fmt.Sprintf("%s_%s", period.ObjectType, period.From.String())
				retentionWorkDir = filepath.Join(c.cfg.WorkingDirectory, "retention", name)
				r                = prometheus.WrapRegistererWith(prometheus.Labels{"from": name}, r)
			)

			// given that compaction can now run on multiple periods, marker files are stored under /retention/{objectStoreType}_{periodFrom}/markers/
			// if any markers are found in the common markers dir (/retention/markers/) or store specific markers dir (/retention/{objectStoreType}/markers/), copy them to the period specific dirs
			// chunk would be removed by the sweeper if it belongs to a given period or no-op if it doesn't exist.
			if err := retention.CopyMarkers(filepath.Join(c.cfg.WorkingDirectory, "retention"), retentionWorkDir); err != nil {
				return fmt.Errorf("failed to move common markers to period specific dir: %w", err)
			}

			if err := retention.CopyMarkers(filepath.Join(c.cfg.WorkingDirectory, "retention", period.ObjectType), retentionWorkDir); err != nil {
				return fmt.Errorf("failed to move store markers to period specific dir: %w", err)
			}
			// remove markers from the store dir after copying them to period specific dirs.
			legacyMarkerDirs[period.ObjectType] = struct{}{}

			if casted, ok := objectClient.(client.PrefixedObjectClient); ok {
				raw = casted.GetDownstream()
			} else {
				raw = objectClient
			}
			if _, ok := raw.(*local.FSObjectClient); ok {
				encoder = client.FSEncoder
			}
			chunkClient := client.NewClient(objectClient, encoder, schemaConfig)

			sc.sweeper, err = retention.NewSweeper(retentionWorkDir, chunkClient, c.cfg.RetentionDeleteWorkCount, c.cfg.RetentionDeleteDelay, c.cfg.RetentionBackoffConfig, r)
			if err != nil {
				return fmt.Errorf("failed to init sweeper: %w", err)
			}

			sc.tableMarker, err = retention.NewMarker(retentionWorkDir, c.expirationChecker, c.cfg.RetentionTableTimeout, chunkClient, r)
			if err != nil {
				return fmt.Errorf("failed to init table marker: %w", err)
			}
		}

		c.storeContainers[from] = sc
	}

	if c.cfg.RetentionEnabled {
		// remove legacy markers
		for store := range legacyMarkerDirs {
			if err := os.RemoveAll(filepath.Join(c.cfg.WorkingDirectory, "retention", store, retention.MarkersFolder)); err != nil {
				return fmt.Errorf("remove old markers from store dir: %w", err)
			}
		}

		if err := os.RemoveAll(filepath.Join(c.cfg.WorkingDirectory, "retention", retention.MarkersFolder)); err != nil {
			return fmt.Errorf("remove old markers: %w", err)
		}
	}

	c.metrics = newMetrics(r)
	c.tablesManager = newTablesManager(c.cfg, c.storeContainers, c.indexCompactors, c.schemaConfig, c.expirationChecker, c.metrics)

	if c.cfg.RetentionEnabled {
		if err := c.deleteRequestsManager.Init(c.tablesManager); err != nil {
			return err
		}
	}

	if c.cfg.HorizontalScalingMode == HorizontalScalingModeMain {
		c.JobQueue = jobqueue.NewQueue(r)
		if c.cfg.RetentionEnabled {
			if err := c.JobQueue.RegisterBuilder(grpc.JOB_TYPE_DELETION, c.deleteRequestsManager.JobBuilder(), c.cfg.JobsConfig.Deletion.Timeout, c.cfg.JobsConfig.Deletion.MaxRetries); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Compactor) initDeletes(objectClient client.ObjectClient, indexUpdatePropagationMaxDelay time.Duration, r prometheus.Registerer, limits Limits) error {
	deletionWorkDir := filepath.Join(c.cfg.WorkingDirectory, "deletion")
	indexStorageClient := storage.NewIndexStorageClient(objectClient, c.cfg.DeleteRequestStoreKeyPrefix)
	store, err := deletion.NewDeleteRequestsStore(
		deletion.DeleteRequestsStoreDBType(c.cfg.DeleteRequestStoreDBType),
		deletionWorkDir,
		indexStorageClient,
		deletion.DeleteRequestsStoreDBType(c.cfg.BackupDeleteRequestStoreDBType),
		indexUpdatePropagationMaxDelay,
	)
	if err != nil {
		return err
	}

	c.deleteRequestsStore = store

	c.DeleteRequestsHandler = deletion.NewDeleteRequestHandler(
		c.deleteRequestsStore,
		c.cfg.DeleteMaxInterval,
		c.cfg.DeleteRequestCancelPeriod,
		r,
	)

	c.DeleteRequestsGRPCHandler = deletion.NewGRPCRequestHandler(c.deleteRequestsStore, limits)

	c.deleteRequestsManager, err = deletion.NewDeleteRequestsManager(
		deletionWorkDir,
		c.deleteRequestsStore,
		c.cfg.DeleteRequestCancelPeriod,
		c.cfg.DeleteBatchSize,
		limits,
		c.cfg.HorizontalScalingMode == HorizontalScalingModeMain,
		client.NewPrefixedObjectClient(objectClient, c.cfg.JobsConfig.Deletion.DeletionManifestStorePrefix),
		r,
	)
	if err != nil {
		return err
	}

	c.expirationChecker = newExpirationChecker(retention.NewExpirationChecker(limits), c.deleteRequestsManager)
	return nil
}

func (c *Compactor) starting(ctx context.Context) (err error) {
	// In case this function will return error we want to unregister the instance
	// from the ring. We do it ensuring dependencies are gracefully stopped if they
	// were already started.
	defer func() {
		if err == nil || c.subservices == nil {
			return
		}

		if stopErr := services.StopManagerAndAwaitStopped(context.Background(), c.subservices); stopErr != nil {
			level.Error(util_log.Logger).Log("msg", "failed to gracefully stop compactor dependencies", "err", stopErr)
		}
	}()

	if err := services.StartManagerAndAwaitHealthy(ctx, c.subservices); err != nil {
		return errors.Wrap(err, "unable to start compactor subservices")
	}

	// The BasicLifecycler does not automatically move state to ACTIVE such that any additional work that
	// someone wants to do can be done before becoming ACTIVE. For the query compactor we don't currently
	// have any additional work so we can become ACTIVE right away.

	// Wait until the ring client detected this instance in the JOINING state to
	// make sure that when we'll run the initial sync we already know  the tokens
	// assigned to this instance.
	level.Info(util_log.Logger).Log("msg", "waiting until compactor is JOINING in the ring")
	if err := ring.WaitInstanceState(ctx, c.ring, c.ringLifecycler.GetInstanceID(), ring.JOINING); err != nil {
		return err
	}
	level.Info(util_log.Logger).Log("msg", "compactor is JOINING in the ring")

	// Change ring state to ACTIVE
	if err = c.ringLifecycler.ChangeState(ctx, ring.ACTIVE); err != nil {
		return errors.Wrapf(err, "switch instance to %s in the ring", ring.ACTIVE)
	}

	// Wait until the ring client detected this instance in the ACTIVE state to
	// make sure that when we'll run the loop it won't be detected as a ring
	// topology change.
	level.Info(util_log.Logger).Log("msg", "waiting until compactor is ACTIVE in the ring")
	if err := ring.WaitInstanceState(ctx, c.ring, c.ringLifecycler.GetInstanceID(), ring.ACTIVE); err != nil {
		return err
	}
	level.Info(util_log.Logger).Log("msg", "compactor is ACTIVE in the ring")

	return nil
}

func (c *Compactor) loop(ctx context.Context) error {
	if c.cfg.RunOnce {
		level.Info(util_log.Logger).Log("msg", "running single compaction")
		err := c.tablesManager.runCompaction(ctx, false)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "compaction encountered an error", "err", err)
		}
		level.Info(util_log.Logger).Log("msg", "single compaction finished")
		level.Info(util_log.Logger).Log("msg", "interrupt or terminate the process to finish")

		// Wait for Loki to shutdown.
		<-ctx.Done()
		level.Info(util_log.Logger).Log("msg", "compactor exiting")
		return nil
	}

	if c.cfg.RetentionEnabled {
		if c.deleteRequestsStore != nil {
			defer c.deleteRequestsStore.Stop()
		}
	}

	syncTicker := time.NewTicker(c.ringPollPeriod)
	defer syncTicker.Stop()

	var runningCtx context.Context
	var runningCancel context.CancelFunc
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			if runningCancel != nil {
				runningCancel()
			}
			wg.Wait()
			level.Info(util_log.Logger).Log("msg", "compactor exiting")
			return nil
		case <-syncTicker.C:
			bufDescs, bufHosts, bufZones := ring.MakeBuffersForGet()
			rs, err := c.ring.Get(ringKeyOfLeader, ring.Write, bufDescs, bufHosts, bufZones)
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "error asking ring for who should run the compactor, will check again", "err", err)
				continue
			}

			addrs := rs.GetAddresses()
			if len(addrs) != 1 {
				level.Error(util_log.Logger).Log("msg", "too many addresses (more that one) return when asking the ring who should run the compactor, will check again")
				continue
			}
			if c.ringLifecycler.GetInstanceAddr() == addrs[0] {
				// If not running, start
				if !c.running {
					level.Info(util_log.Logger).Log("msg", "this instance has been chosen to run the compactor, starting compactor")
					runningCtx, runningCancel = context.WithCancel(ctx)

					wg.Add(1)
					go func() {
						defer wg.Done()
						c.tablesManager.start(runningCtx)
					}()

					if c.deleteRequestsManager != nil {
						wg.Add(1)
						go func() {
							defer wg.Done()
							c.deleteRequestsManager.Start(runningCtx)
						}()
					}

					if c.cfg.HorizontalScalingMode == HorizontalScalingModeMain {
						wg.Add(1)
						go func() {
							defer wg.Done()
							c.JobQueue.Start(runningCtx)
						}()
					}

					c.running = true
					c.metrics.compactorRunning.Set(1)
				}
			} else {
				// If running, shutdown
				if c.running {
					level.Info(util_log.Logger).Log("msg", "this instance should no longer run the compactor, stopping compactor")
					runningCancel()
					wg.Wait()
					c.running = false
					c.metrics.compactorRunning.Set(0)
					level.Info(util_log.Logger).Log("msg", "compactor stopped")
				}
			}
		}
	}
}

func (c *Compactor) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), c.subservices)
}

func (c *Compactor) RegisterIndexCompactor(indexType string, indexCompactor IndexCompactor) {
	c.indexCompactors[indexType] = indexCompactor
}

func (c *Compactor) TablesManager() TablesManager {
	return c.tablesManager
}

type expirationChecker struct {
	retentionExpiryChecker retention.ExpirationChecker
	deletionExpiryChecker  retention.ExpirationChecker
}

func newExpirationChecker(retentionExpiryChecker, deletionExpiryChecker retention.ExpirationChecker) retention.ExpirationChecker {
	return &expirationChecker{retentionExpiryChecker, deletionExpiryChecker}
}

func (e *expirationChecker) Expired(userID []byte, chk retention.Chunk, lbls labels.Labels, seriesID []byte, tableName string, now model.Time) (bool, filter.Func) {
	if expired, nonDeletedIntervals := e.retentionExpiryChecker.Expired(userID, chk, lbls, seriesID, tableName, now); expired {
		return expired, nonDeletedIntervals
	}

	return e.deletionExpiryChecker.Expired(userID, chk, lbls, seriesID, tableName, now)
}

func (e *expirationChecker) MarkPhaseStarted() {
	e.retentionExpiryChecker.MarkPhaseStarted()
	e.deletionExpiryChecker.MarkPhaseStarted()
}

func (e *expirationChecker) MarkPhaseFailed() {
	e.retentionExpiryChecker.MarkPhaseFailed()
	e.deletionExpiryChecker.MarkPhaseFailed()
}

func (e *expirationChecker) MarkPhaseFinished() {
	e.retentionExpiryChecker.MarkPhaseFinished()
	e.deletionExpiryChecker.MarkPhaseFinished()
}

func (e *expirationChecker) MarkPhaseTimedOut() {
	e.retentionExpiryChecker.MarkPhaseTimedOut()
	e.deletionExpiryChecker.MarkPhaseTimedOut()
}

func (e *expirationChecker) IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool {
	return e.retentionExpiryChecker.IntervalMayHaveExpiredChunks(interval, userID) || e.deletionExpiryChecker.IntervalMayHaveExpiredChunks(interval, userID)
}

func (e *expirationChecker) DropFromIndex(userID []byte, chk retention.Chunk, labels labels.Labels, tableEndTime model.Time, now model.Time) bool {
	return e.retentionExpiryChecker.DropFromIndex(userID, chk, labels, tableEndTime, now) || e.deletionExpiryChecker.DropFromIndex(userID, chk, labels, tableEndTime, now)
}

func (e *expirationChecker) CanSkipSeries(userID []byte, lbls labels.Labels, seriesID []byte, seriesStart model.Time, tableName string, now model.Time) bool {
	return e.retentionExpiryChecker.CanSkipSeries(userID, lbls, seriesID, seriesStart, tableName, now) && e.deletionExpiryChecker.CanSkipSeries(userID, lbls, seriesID, seriesStart, tableName, now)
}

func (e *expirationChecker) MarkSeriesAsProcessed(userID, seriesID []byte, lbls labels.Labels, tableName string) error {
	if err := e.retentionExpiryChecker.MarkSeriesAsProcessed(userID, seriesID, lbls, tableName); err != nil {
		return err
	}

	return e.deletionExpiryChecker.MarkSeriesAsProcessed(userID, seriesID, lbls, tableName)
}

func (c *Compactor) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, _ string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	// When we initialize the compactor instance in the ring we want to start from
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

func (c *Compactor) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (c *Compactor) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (c *Compactor) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}

func SortTablesByRange(tables []string) {
	tableRanges := make(map[string]model.Interval)
	for _, table := range tables {
		tableRanges[table] = retention.ExtractIntervalFromTableName(table)
	}

	sort.Slice(tables, func(i, j int) bool {
		// less than if start time is after produces a most recent first sort order
		return tableRanges[tables[i]].Start.After(tableRanges[tables[j]].Start)
	})
}

func SchemaPeriodForTable(cfg config.SchemaConfig, tableName string) (config.PeriodConfig, bool) {
	tableInterval := retention.ExtractIntervalFromTableName(tableName)
	schemaCfg, err := cfg.SchemaForTime(tableInterval.Start)
	if err != nil || schemaCfg.IndexTables.TableFor(tableInterval.Start) != tableName {
		return config.PeriodConfig{}, false
	}

	return schemaCfg, true
}
