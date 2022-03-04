package compactor

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	loki_storage "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/objectclient"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/deletion"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	shipper_storage "github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

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

var (
	retentionEnabledStats = usagestats.NewString("compactor_retention_enabled")
	defaultRetentionStats = usagestats.NewString("compactor_default_retention")
)

type Config struct {
	WorkingDirectory          string          `yaml:"working_directory"`
	SharedStoreType           string          `yaml:"shared_store"`
	SharedStoreKeyPrefix      string          `yaml:"shared_store_key_prefix"`
	CompactionInterval        time.Duration   `yaml:"compaction_interval"`
	ApplyRetentionInterval    time.Duration   `yaml:"apply_retention_interval"`
	RetentionEnabled          bool            `yaml:"retention_enabled"`
	RetentionDeleteDelay      time.Duration   `yaml:"retention_delete_delay"`
	RetentionDeleteWorkCount  int             `yaml:"retention_delete_worker_count"`
	DeleteRequestCancelPeriod time.Duration   `yaml:"delete_request_cancel_period"`
	MaxCompactionParallelism  int             `yaml:"max_compaction_parallelism"`
	CompactorRing             util.RingConfig `yaml:"compactor_ring,omitempty"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.WorkingDirectory, "boltdb.shipper.compactor.working-directory", "", "Directory where files can be downloaded for compaction.")
	f.StringVar(&cfg.SharedStoreType, "boltdb.shipper.compactor.shared-store", "", "Shared store used for storing boltdb files. Supported types: gcs, s3, azure, swift, filesystem")
	f.StringVar(&cfg.SharedStoreKeyPrefix, "boltdb.shipper.compactor.shared-store.key-prefix", "index/", "Prefix to add to Object Keys in Shared store. Path separator(if any) should always be a '/'. Prefix should never start with a separator but should always end with it.")
	f.DurationVar(&cfg.CompactionInterval, "boltdb.shipper.compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.ApplyRetentionInterval, "boltdb.shipper.compactor.apply-retention-interval", 0, "Interval at which to apply/enforce retention. 0 means run at same interval as compaction. If non-zero, it should always be a multiple of compaction interval.")
	f.DurationVar(&cfg.RetentionDeleteDelay, "boltdb.shipper.compactor.retention-delete-delay", 2*time.Hour, "Delay after which chunks will be fully deleted during retention.")
	f.BoolVar(&cfg.RetentionEnabled, "boltdb.shipper.compactor.retention-enabled", false, "(Experimental) Activate custom (per-stream,per-tenant) retention.")
	f.IntVar(&cfg.RetentionDeleteWorkCount, "boltdb.shipper.compactor.retention-delete-worker-count", 150, "The total amount of worker to use to delete chunks.")
	f.DurationVar(&cfg.DeleteRequestCancelPeriod, "boltdb.shipper.compactor.delete-request-cancel-period", 24*time.Hour, "Allow cancellation of delete request until duration after they are created. Data would be deleted only after delete requests have been older than this duration. Ideally this should be set to at least 24h.")
	f.IntVar(&cfg.MaxCompactionParallelism, "boltdb.shipper.compactor.max-compaction-parallelism", 1, "Maximum number of tables to compact in parallel. While increasing this value, please make sure compactor has enough disk space allocated to be able to store and compact as many tables.")
	cfg.CompactorRing.RegisterFlagsWithPrefix("boltdb.shipper.compactor.", "collectors/", f)
}

// Validate verifies the config does not contain inappropriate values
func (cfg *Config) Validate() error {
	if cfg.MaxCompactionParallelism < 1 {
		return errors.New("max compaction parallelism must be >= 1")
	}
	if cfg.RetentionEnabled && cfg.ApplyRetentionInterval != 0 && cfg.ApplyRetentionInterval%cfg.CompactionInterval != 0 {
		return errors.New("interval for applying retention should either be set to a 0 or a multiple of compaction interval")
	}

	return shipper_util.ValidateSharedStoreKeyPrefix(cfg.SharedStoreKeyPrefix)
}

type Compactor struct {
	services.Service

	cfg                   Config
	indexStorageClient    shipper_storage.Client
	tableMarker           retention.TableMarker
	sweeper               *retention.Sweeper
	deleteRequestsStore   deletion.DeleteRequestsStore
	DeleteRequestsHandler *deletion.DeleteRequestHandler
	deleteRequestsManager *deletion.DeleteRequestsManager
	expirationChecker     retention.ExpirationChecker
	metrics               *metrics
	running               bool
	wg                    sync.WaitGroup

	// Ring used for running a single compactor
	ringLifecycler *ring.BasicLifecycler
	ring           *ring.Ring
	ringPollPeriod time.Duration

	// Subservices manager.
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher
}

func NewCompactor(cfg Config, storageConfig storage.Config, schemaConfig loki_storage.SchemaConfig, limits retention.Limits, clientMetrics storage.ClientMetrics, r prometheus.Registerer) (*Compactor, error) {
	retentionEnabledStats.Set("false")
	if cfg.RetentionEnabled {
		retentionEnabledStats.Set("true")
	}
	if limits != nil {
		defaultRetentionStats.Set(limits.DefaultLimits().RetentionPeriod.String())
	}
	if cfg.SharedStoreType == "" {
		return nil, errors.New("compactor shared_store_type must be specified")
	}

	compactor := &Compactor{
		cfg:            cfg,
		ringPollPeriod: 5 * time.Second,
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
	compactor.ring, err = ring.NewWithStoreClientAndStrategy(ringCfg, ringNameForServer, ringKey, ringStore, ring.NewIgnoreUnhealthyInstancesReplicationStrategy(), prometheus.WrapRegistererWithPrefix("cortex_", r), util_log.Logger)
	if err != nil {
		return nil, errors.Wrap(err, "create ring client")
	}

	compactor.subservices, err = services.NewManager(compactor.ringLifecycler, compactor.ring)
	if err != nil {
		return nil, err
	}
	compactor.subservicesWatcher = services.NewFailureWatcher()
	compactor.subservicesWatcher.WatchManager(compactor.subservices)

	if err := compactor.init(storageConfig, schemaConfig, limits, clientMetrics, r); err != nil {
		return nil, err
	}

	compactor.Service = services.NewBasicService(compactor.starting, compactor.loop, compactor.stopping)
	return compactor, nil
}

func (c *Compactor) init(storageConfig storage.Config, schemaConfig loki_storage.SchemaConfig, limits retention.Limits, clientMetrics storage.ClientMetrics, r prometheus.Registerer) error {
	objectClient, err := storage.NewObjectClient(c.cfg.SharedStoreType, storageConfig, clientMetrics)
	if err != nil {
		return err
	}

	err = chunk_util.EnsureDirectory(c.cfg.WorkingDirectory)
	if err != nil {
		return err
	}
	c.indexStorageClient = shipper_storage.NewIndexStorageClient(objectClient, c.cfg.SharedStoreKeyPrefix)
	c.metrics = newMetrics(r)

	if c.cfg.RetentionEnabled {
		var encoder objectclient.KeyEncoder
		if _, ok := objectClient.(*local.FSObjectClient); ok {
			encoder = objectclient.FSEncoder
		}

		chunkClient := objectclient.NewClient(objectClient, encoder, schemaConfig.SchemaConfig)

		retentionWorkDir := filepath.Join(c.cfg.WorkingDirectory, "retention")
		c.sweeper, err = retention.NewSweeper(retentionWorkDir, chunkClient, c.cfg.RetentionDeleteWorkCount, c.cfg.RetentionDeleteDelay, r)
		if err != nil {
			return err
		}

		deletionWorkDir := filepath.Join(c.cfg.WorkingDirectory, "deletion")

		c.deleteRequestsStore, err = deletion.NewDeleteStore(deletionWorkDir, c.indexStorageClient)
		if err != nil {
			return err
		}

		c.DeleteRequestsHandler = deletion.NewDeleteRequestHandler(c.deleteRequestsStore, time.Hour, r)
		c.deleteRequestsManager = deletion.NewDeleteRequestsManager(c.deleteRequestsStore, c.cfg.DeleteRequestCancelPeriod, r)

		c.expirationChecker = newExpirationChecker(retention.NewExpirationChecker(limits), c.deleteRequestsManager)

		c.tableMarker, err = retention.NewMarker(retentionWorkDir, schemaConfig, c.expirationChecker, chunkClient, r)
		if err != nil {
			return err
		}
	}

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
	if c.cfg.RetentionEnabled {
		defer c.deleteRequestsStore.Stop()
		defer c.deleteRequestsManager.Stop()
	}

	syncTicker := time.NewTicker(c.ringPollPeriod)
	defer syncTicker.Stop()

	var runningCtx context.Context
	var runningCancel context.CancelFunc

	for {
		select {
		case <-ctx.Done():
			if runningCancel != nil {
				runningCancel()
			}
			c.wg.Wait()
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
					go c.runCompactions(runningCtx)
					c.running = true
					c.metrics.compactorRunning.Set(1)
				}
			} else {
				// If running, shutdown
				if c.running {
					level.Info(util_log.Logger).Log("msg", "this instance should no longer run the compactor, stopping compactor")
					runningCancel()
					c.wg.Wait()
					c.running = false
					c.metrics.compactorRunning.Set(0)
					level.Info(util_log.Logger).Log("msg", "compactor stopped")
				}
			}
		}
	}
}

func (c *Compactor) runCompactions(ctx context.Context) {
	// To avoid races, wait 1 compaction interval before actually starting the compactor
	// this allows the ring to settle if there are a lot of ring changes and gives
	// time for existing compactors to shutdown before this starts to avoid
	// multiple compactors running at the same time.
	t := time.NewTimer(c.cfg.CompactionInterval)
	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("waiting %v for ring to stay stable and previous compactions to finish before starting compactor", c.cfg.CompactionInterval))
	select {
	case <-ctx.Done():
		return
	case <-t.C:
		level.Info(util_log.Logger).Log("msg", "compactor startup delay completed")
		break
	}

	lastRetentionRunAt := time.Unix(0, 0)
	runCompaction := func() {
		applyRetention := false
		if c.cfg.RetentionEnabled && time.Since(lastRetentionRunAt) >= c.cfg.ApplyRetentionInterval {
			level.Info(util_log.Logger).Log("msg", "applying retention with compaction")
			applyRetention = true
		}

		err := c.RunCompaction(ctx, applyRetention)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to run compaction", "err", err)
		}

		if applyRetention {
			lastRetentionRunAt = time.Now()
		}
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		runCompaction()

		ticker := time.NewTicker(c.cfg.CompactionInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				runCompaction()
			case <-ctx.Done():
				return
			}
		}
	}()
	if c.cfg.RetentionEnabled {
		c.wg.Add(1)
		go func() {
			// starts the chunk sweeper
			defer func() {
				c.sweeper.Stop()
				c.wg.Done()
			}()
			c.sweeper.Start()
			<-ctx.Done()
		}()
	}
	level.Info(util_log.Logger).Log("msg", "compactor started")
}

func (c *Compactor) stopping(_ error) error {
	return services.StopManagerAndAwaitStopped(context.Background(), c.subservices)
}

func (c *Compactor) CompactTable(ctx context.Context, tableName string, applyRetention bool) error {
	table, err := newTable(ctx, filepath.Join(c.cfg.WorkingDirectory, tableName), c.indexStorageClient,
		c.tableMarker, c.expirationChecker)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to initialize table for compaction", "table", tableName, "err", err)
		return err
	}

	interval := retention.ExtractIntervalFromTableName(tableName)
	intervalMayHaveExpiredChunks := false
	if applyRetention {
		intervalMayHaveExpiredChunks = c.expirationChecker.IntervalMayHaveExpiredChunks(interval, "")
	}

	err = table.compact(intervalMayHaveExpiredChunks)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to compact files", "table", tableName, "err", err)
		return err
	}
	return nil
}

func (c *Compactor) RunCompaction(ctx context.Context, applyRetention bool) error {
	status := statusSuccess
	start := time.Now()

	if applyRetention {
		c.expirationChecker.MarkPhaseStarted()
	}

	defer func() {
		c.metrics.compactTablesOperationTotal.WithLabelValues(status).Inc()
		runtime := time.Since(start)
		if status == statusSuccess {
			c.metrics.compactTablesOperationDurationSeconds.Set(runtime.Seconds())
			c.metrics.compactTablesOperationLastSuccess.SetToCurrentTime()
			if applyRetention {
				c.metrics.applyRetentionLastSuccess.SetToCurrentTime()
			}
		}

		if applyRetention {
			if status == statusSuccess {
				c.expirationChecker.MarkPhaseFinished()
			} else {
				c.expirationChecker.MarkPhaseFailed()
			}
		}
		if runtime > c.cfg.CompactionInterval {
			level.Warn(util_log.Logger).Log("msg", fmt.Sprintf("last compaction took %s which is longer than the compaction interval of %s, this can lead to duplicate compactors running if not running a standalone compactor instance.", runtime, c.cfg.CompactionInterval))
		}
	}()

	tables, err := c.indexStorageClient.ListTables(ctx)
	if err != nil {
		status = statusFailure
		return err
	}

	compactTablesChan := make(chan string)
	errChan := make(chan error)

	for i := 0; i < c.cfg.MaxCompactionParallelism; i++ {
		go func() {
			var err error
			defer func() {
				errChan <- err
			}()

			for {
				select {
				case tableName, ok := <-compactTablesChan:
					if !ok {
						return
					}

					level.Info(util_log.Logger).Log("msg", "compacting table", "table-name", tableName)
					err = c.CompactTable(ctx, tableName, applyRetention)
					if err != nil {
						return
					}
					level.Info(util_log.Logger).Log("msg", "finished compacting table", "table-name", tableName)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		for _, tableName := range tables {
			if tableName == deletion.DeleteRequestsTableName {
				// we do not want to compact or apply retention on delete requests table
				continue
			}

			select {
			case compactTablesChan <- tableName:
			case <-ctx.Done():
				return
			}
		}

		close(compactTablesChan)
	}()

	var firstErr error
	// read all the errors
	for i := 0; i < c.cfg.MaxCompactionParallelism; i++ {
		err := <-errChan
		if err != nil && firstErr == nil {
			status = statusFailure
			firstErr = err
		}
	}

	return firstErr
}

type expirationChecker struct {
	retentionExpiryChecker retention.ExpirationChecker
	deletionExpiryChecker  retention.ExpirationChecker
}

func newExpirationChecker(retentionExpiryChecker, deletionExpiryChecker retention.ExpirationChecker) retention.ExpirationChecker {
	return &expirationChecker{retentionExpiryChecker, deletionExpiryChecker}
}

func (e *expirationChecker) Expired(ref retention.ChunkEntry, now model.Time) (bool, []model.Interval) {
	if expired, nonDeletedIntervals := e.retentionExpiryChecker.Expired(ref, now); expired {
		return expired, nonDeletedIntervals
	}

	return e.deletionExpiryChecker.Expired(ref, now)
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

func (e *expirationChecker) IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool {
	return e.retentionExpiryChecker.IntervalMayHaveExpiredChunks(interval, userID) || e.deletionExpiryChecker.IntervalMayHaveExpiredChunks(interval, userID)
}

func (e *expirationChecker) DropFromIndex(ref retention.ChunkEntry, tableEndTime model.Time, now model.Time) bool {
	return e.retentionExpiryChecker.DropFromIndex(ref, tableEndTime, now) || e.deletionExpiryChecker.DropFromIndex(ref, tableEndTime, now)
}

func (c *Compactor) OnRingInstanceRegister(_ *ring.BasicLifecycler, ringDesc ring.Desc, instanceExists bool, instanceID string, instanceDesc ring.InstanceDesc) (ring.InstanceState, ring.Tokens) {
	// When we initialize the compactor instance in the ring we want to start from
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

func (c *Compactor) OnRingInstanceTokens(_ *ring.BasicLifecycler, _ ring.Tokens) {}
func (c *Compactor) OnRingInstanceStopping(_ *ring.BasicLifecycler)              {}
func (c *Compactor) OnRingInstanceHeartbeat(_ *ring.BasicLifecycler, _ *ring.Desc, _ *ring.InstanceDesc) {
}

func (c *Compactor) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	c.ring.ServeHTTP(w, req)
}
