package ingester

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/ingester/index"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/runtime"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	errUtil "github.com/grafana/loki/pkg/util"
	listutil "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

// ErrReadOnly is returned when the ingester is shutting down and a push was
// attempted.
var ErrReadOnly = errors.New("Ingester is shutting down")

var flushQueueLength = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "cortex_ingester_flush_queue_length",
	Help: "The total number of series pending in the flush queue.",
})

// Config for an ingester.
type Config struct {
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`

	// Config for transferring chunks.
	MaxTransferRetries int `yaml:"max_transfer_retries,omitempty"`

	ConcurrentFlushes   int               `yaml:"concurrent_flushes"`
	FlushCheckPeriod    time.Duration     `yaml:"flush_check_period"`
	FlushOpTimeout      time.Duration     `yaml:"flush_op_timeout"`
	RetainPeriod        time.Duration     `yaml:"chunk_retain_period"`
	MaxChunkIdle        time.Duration     `yaml:"chunk_idle_period"`
	BlockSize           int               `yaml:"chunk_block_size"`
	TargetChunkSize     int               `yaml:"chunk_target_size"`
	ChunkEncoding       string            `yaml:"chunk_encoding"`
	parsedEncoding      chunkenc.Encoding `yaml:"-"` // placeholder for validated encoding
	MaxChunkAge         time.Duration     `yaml:"max_chunk_age"`
	AutoForgetUnhealthy bool              `yaml:"autoforget_unhealthy"`

	// Synchronization settings. Used to make sure that ingesters cut their chunks at the same moments.
	SyncPeriod         time.Duration `yaml:"sync_period"`
	SyncMinUtilization float64       `yaml:"sync_min_utilization"`

	MaxReturnedErrors int `yaml:"max_returned_stream_errors"`

	// For testing, you can override the address and ID of this ingester.
	ingesterClientFactory func(cfg client.Config, addr string) (client.HealthAndIngesterClient, error)

	QueryStore                  bool          `yaml:"-"`
	QueryStoreMaxLookBackPeriod time.Duration `yaml:"query_store_max_look_back_period"`

	WAL WALConfig `yaml:"wal,omitempty"`

	ChunkFilterer storage.RequestChunkFilterer `yaml:"-"`

	IndexShards int `yaml:"index_shards"`
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlags(f)
	cfg.WAL.RegisterFlags(f)

	f.IntVar(&cfg.MaxTransferRetries, "ingester.max-transfer-retries", 10, "Number of times to try and transfer chunks before falling back to flushing. If set to 0 or negative value, transfers are disabled.")
	f.IntVar(&cfg.ConcurrentFlushes, "ingester.concurrent-flushes", 16, "")
	f.DurationVar(&cfg.FlushCheckPeriod, "ingester.flush-check-period", 30*time.Second, "")
	f.DurationVar(&cfg.FlushOpTimeout, "ingester.flush-op-timeout", 10*time.Second, "")
	f.DurationVar(&cfg.RetainPeriod, "ingester.chunks-retain-period", 15*time.Minute, "")
	f.DurationVar(&cfg.MaxChunkIdle, "ingester.chunks-idle-period", 30*time.Minute, "")
	f.IntVar(&cfg.BlockSize, "ingester.chunks-block-size", 256*1024, "")
	f.IntVar(&cfg.TargetChunkSize, "ingester.chunk-target-size", 0, "")
	f.StringVar(&cfg.ChunkEncoding, "ingester.chunk-encoding", chunkenc.EncGZIP.String(), fmt.Sprintf("The algorithm to use for compressing chunk. (%s)", chunkenc.SupportedEncoding()))
	f.DurationVar(&cfg.SyncPeriod, "ingester.sync-period", 0, "How often to cut chunks to synchronize ingesters.")
	f.Float64Var(&cfg.SyncMinUtilization, "ingester.sync-min-utilization", 0, "Minimum utilization of chunk when doing synchronization.")
	f.IntVar(&cfg.MaxReturnedErrors, "ingester.max-ignored-stream-errors", 10, "Maximum number of ignored stream errors to return. 0 to return all errors.")
	f.DurationVar(&cfg.MaxChunkAge, "ingester.max-chunk-age", time.Hour, "Maximum chunk age before flushing.")
	f.DurationVar(&cfg.QueryStoreMaxLookBackPeriod, "ingester.query-store-max-look-back-period", 0, "How far back should an ingester be allowed to query the store for data, for use only with boltdb-shipper index and filesystem object store. -1 for infinite.")
	f.BoolVar(&cfg.AutoForgetUnhealthy, "ingester.autoforget-unhealthy", false, "Enable to remove unhealthy ingesters from the ring after `ring.kvstore.heartbeat_timeout`")
	f.IntVar(&cfg.IndexShards, "ingester.index-shards", index.DefaultIndexShards, "Shard factor used in the ingesters for the in process reverse index. This MUST be evenly divisible by ALL schema shard factors or Loki will not start.")
}

func (cfg *Config) Validate() error {
	enc, err := chunkenc.ParseEncoding(cfg.ChunkEncoding)
	if err != nil {
		return err
	}
	cfg.parsedEncoding = enc

	if err = cfg.WAL.Validate(); err != nil {
		return err
	}

	if cfg.MaxTransferRetries > 0 && cfg.WAL.Enabled {
		return errors.New("the use of the write ahead log (WAL) is incompatible with chunk transfers. It's suggested to use the WAL. Please try setting ingester.max-transfer-retries to 0 to disable transfers")
	}

	if cfg.IndexShards <= 0 {
		return fmt.Errorf("Invalid ingester index shard factor: %d", cfg.IndexShards)
	}

	return nil
}

// Ingester builds chunks for incoming log streams.
type Ingester struct {
	services.Service

	cfg           Config
	clientConfig  client.Config
	tenantConfigs *runtime.TenantConfigs

	shutdownMtx  sync.Mutex // Allows processes to grab a lock and prevent a shutdown
	instancesMtx sync.RWMutex
	instances    map[string]*instance
	readonly     bool

	lifecycler        *ring.Lifecycler
	lifecyclerWatcher *services.FailureWatcher

	store           ChunkStore
	periodicConfigs []chunk.PeriodConfig

	loopDone    sync.WaitGroup
	loopQuit    chan struct{}
	tailersQuit chan struct{}

	// One queue per flush thread.  Fingerprint is used to
	// pick a queue.
	flushQueues     []*util.PriorityQueue
	flushQueuesDone sync.WaitGroup

	limiter *Limiter

	// Denotes whether the ingester should flush on shutdown.
	// Currently only used by the WAL to signal when the disk is full.
	flushOnShutdownSwitch *OnceSwitch

	// Only used by WAL & flusher to coordinate backpressure during replay.
	replayController *replayController

	metrics *ingesterMetrics

	wal WAL

	chunkFilter storage.RequestChunkFilterer
}

// ChunkStore is the interface we need to store chunks.
type ChunkStore interface {
	Put(ctx context.Context, chunks []chunk.Chunk) error
	SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error)
	SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error)
	GetChunkRefs(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*chunk.Fetcher, error)
	GetSchemaConfigs() []chunk.PeriodConfig
}

// New makes a new Ingester.
func New(cfg Config, clientConfig client.Config, store ChunkStore, limits *validation.Overrides, configs *runtime.TenantConfigs, registerer prometheus.Registerer) (*Ingester, error) {
	if cfg.ingesterClientFactory == nil {
		cfg.ingesterClientFactory = client.New
	}

	metrics := newIngesterMetrics(registerer)

	i := &Ingester{
		cfg:                   cfg,
		clientConfig:          clientConfig,
		tenantConfigs:         configs,
		instances:             map[string]*instance{},
		store:                 store,
		periodicConfigs:       store.GetSchemaConfigs(),
		loopQuit:              make(chan struct{}),
		flushQueues:           make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		tailersQuit:           make(chan struct{}),
		metrics:               metrics,
		flushOnShutdownSwitch: &OnceSwitch{},
	}
	i.replayController = newReplayController(metrics, cfg.WAL, &replayFlusher{i})

	if cfg.WAL.Enabled {
		if err := os.MkdirAll(cfg.WAL.Dir, os.ModePerm); err != nil {
			return nil, err
		}
	}

	wal, err := newWAL(cfg.WAL, registerer, metrics, newIngesterSeriesIter(i))
	if err != nil {
		return nil, err
	}
	i.wal = wal

	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "ingester", ring.IngesterRingKey, !cfg.WAL.Enabled || cfg.WAL.FlushOnShutdown, registerer)
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)

	// Now that the lifecycler has been created, we can create the limiter
	// which depends on it.
	i.limiter = NewLimiter(limits, metrics, i.lifecycler, cfg.LifecyclerConfig.RingConfig.ReplicationFactor)

	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)

	i.setupAutoForget()

	if i.cfg.ChunkFilterer != nil {
		i.SetChunkFilterer(i.cfg.ChunkFilterer)
	}

	return i, nil
}

func (i *Ingester) SetChunkFilterer(chunkFilter storage.RequestChunkFilterer) {
	i.chunkFilter = chunkFilter
}

// setupAutoForget looks for ring status if `AutoForgetUnhealthy` is enabled
// when enabled, unhealthy ingesters that reach `ring.kvstore.heartbeat_timeout` are removed from the ring every `HeartbeatPeriod`
func (i *Ingester) setupAutoForget() {
	if !i.cfg.AutoForgetUnhealthy {
		return
	}

	go func() {
		ctx := context.Background()
		err := i.Service.AwaitRunning(ctx)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("autoforget received error %s, autoforget is disabled", err.Error()))
			return
		}

		level.Info(util_log.Logger).Log("msg", fmt.Sprintf("autoforget is enabled and will remove unhealthy instances from the ring after %v with no heartbeat", i.cfg.LifecyclerConfig.RingConfig.HeartbeatTimeout))

		ticker := time.NewTicker(i.cfg.LifecyclerConfig.HeartbeatPeriod)
		defer ticker.Stop()

		var forgetList []string
		for range ticker.C {
			err := i.lifecycler.KVStore.CAS(ctx, ring.IngesterRingKey, func(in interface{}) (out interface{}, retry bool, err error) {
				forgetList = forgetList[:0]
				if in == nil {
					return nil, false, nil
				}

				ringDesc, ok := in.(*ring.Desc)
				if !ok {
					level.Warn(util_log.Logger).Log("msg", fmt.Sprintf("autoforget saw a KV store value that was not `ring.Desc`, got `%T`", in))
					return nil, false, nil
				}

				for id, ingester := range ringDesc.Ingesters {
					if !ingester.IsHealthy(ring.Reporting, i.cfg.LifecyclerConfig.RingConfig.HeartbeatTimeout, time.Now()) {
						if i.lifecycler.ID == id {
							level.Warn(util_log.Logger).Log("msg", fmt.Sprintf("autoforget has seen our ID `%s` as unhealthy in the ring, network may be partitioned, skip forgeting ingesters this round", id))
							return nil, false, nil
						}
						forgetList = append(forgetList, id)
					}
				}

				if len(forgetList) == len(ringDesc.Ingesters)-1 {
					level.Warn(util_log.Logger).Log("msg", fmt.Sprintf("autoforget have seen %d unhealthy ingesters out of %d, network may be partioned, skip forgeting ingesters this round", len(forgetList), len(ringDesc.Ingesters)))
					forgetList = forgetList[:0]
					return nil, false, nil
				}

				if len(forgetList) > 0 {
					for _, id := range forgetList {
						ringDesc.RemoveIngester(id)
					}
					return ringDesc, true, nil
				}
				return nil, false, nil
			})
			if err != nil {
				level.Warn(util_log.Logger).Log("msg", err)
				continue
			}

			for _, id := range forgetList {
				level.Info(util_log.Logger).Log("msg", fmt.Sprintf("autoforget removed ingester %v from the ring because it was not healthy after %v", id, i.cfg.LifecyclerConfig.RingConfig.HeartbeatTimeout))
			}
			i.metrics.autoForgetUnhealthyIngestersTotal.Add(float64(len(forgetList)))
		}
	}()
}

func (i *Ingester) starting(ctx context.Context) error {
	if i.cfg.WAL.Enabled {
		start := time.Now()

		// Ignore retain period during wal replay.
		oldRetain := i.cfg.RetainPeriod
		i.cfg.RetainPeriod = 0

		// Disable the in process stream limit checks while replaying the WAL.
		// It is re-enabled in the recover's Close() method.
		i.limiter.DisableForWALReplay()

		recoverer := newIngesterRecoverer(i)

		i.metrics.walReplayActive.Set(1)

		endReplay := func() func() {
			var once sync.Once
			return func() {
				once.Do(func() {
					level.Info(util_log.Logger).Log("msg", "closing recoverer")
					recoverer.Close()

					elapsed := time.Since(start)

					i.metrics.walReplayActive.Set(0)
					i.metrics.walReplayDuration.Set(elapsed.Seconds())
					i.cfg.RetainPeriod = oldRetain
					level.Info(util_log.Logger).Log("msg", "WAL recovery finished", "time", elapsed.String())
				})
			}
		}()
		defer endReplay()

		level.Info(util_log.Logger).Log("msg", "recovering from checkpoint")
		checkpointReader, checkpointCloser, err := newCheckpointReader(i.cfg.WAL.Dir)
		if err != nil {
			return err
		}
		defer checkpointCloser.Close()

		checkpointRecoveryErr := RecoverCheckpoint(checkpointReader, recoverer)
		if checkpointRecoveryErr != nil {
			i.metrics.walCorruptionsTotal.WithLabelValues(walTypeCheckpoint).Inc()
			level.Error(util_log.Logger).Log(
				"msg",
				`Recovered from checkpoint with errors. Some streams were likely not recovered due to WAL checkpoint file corruptions (or WAL file deletions while Loki is running). No administrator action is needed and data loss is only a possibility if more than (replication factor / 2 + 1) ingesters suffer from this.`,
				"elapsed", time.Since(start).String(),
			)
		}
		level.Info(util_log.Logger).Log(
			"msg", "recovered WAL checkpoint recovery finished",
			"elapsed", time.Since(start).String(),
			"errors", checkpointRecoveryErr != nil,
		)

		level.Info(util_log.Logger).Log("msg", "recovering from WAL")
		segmentReader, segmentCloser, err := newWalReader(i.cfg.WAL.Dir, -1)
		if err != nil {
			return err
		}
		defer segmentCloser.Close()

		segmentRecoveryErr := RecoverWAL(segmentReader, recoverer)
		if segmentRecoveryErr != nil {
			i.metrics.walCorruptionsTotal.WithLabelValues(walTypeSegment).Inc()
			level.Error(util_log.Logger).Log(
				"msg",
				"Recovered from WAL segments with errors. Some streams and/or entries were likely not recovered due to WAL segment file corruptions (or WAL file deletions while Loki is running). No administrator action is needed and data loss is only a possibility if more than (replication factor / 2 + 1) ingesters suffer from this.",
				"elapsed", time.Since(start).String(),
			)
		}
		level.Info(util_log.Logger).Log(
			"msg", "WAL segment recovery finished",
			"elapsed", time.Since(start).String(),
			"errors", segmentRecoveryErr != nil,
		)

		endReplay()

		i.wal.Start()
	}

	i.InitFlushQueues()

	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	err := i.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = i.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}

	// start our loop
	i.loopDone.Add(1)
	go i.loop()
	return nil
}

func (i *Ingester) running(ctx context.Context) error {
	var serviceError error
	select {
	// wait until service is asked to stop
	case <-ctx.Done():
	// stop
	case err := <-i.lifecyclerWatcher.Chan():
		serviceError = fmt.Errorf("lifecycler failed: %w", err)
	}

	// close tailers before stopping our loop
	close(i.tailersQuit)
	for _, instance := range i.getInstances() {
		instance.closeTailers()
	}

	close(i.loopQuit)
	i.loopDone.Wait()
	return serviceError
}

// Called after running exits, when Ingester transitions to Stopping state.
// At this point, loop no longer runs, but flushers are still running.
func (i *Ingester) stopping(_ error) error {
	i.stopIncomingRequests()
	var errs errUtil.MultiError
	errs.Add(i.wal.Stop())

	if i.flushOnShutdownSwitch.Get() {
		i.lifecycler.SetFlushOnShutdown(true)
	}
	errs.Add(services.StopAndAwaitTerminated(context.Background(), i.lifecycler))

	// Normally, flushers are stopped via lifecycler (in transferOut), but if lifecycler fails,
	// we better stop them.
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}
	i.flushQueuesDone.Wait()

	return errs.Err()
}

func (i *Ingester) loop() {
	defer i.loopDone.Done()

	flushTicker := time.NewTicker(i.cfg.FlushCheckPeriod)
	defer flushTicker.Stop()

	for {
		select {
		case <-flushTicker.C:
			i.sweepUsers(false, true)

		case <-i.loopQuit:
			return
		}
	}
}

// ShutdownHandler triggers the following set of operations in order:
//     * Change the state of ring to stop accepting writes.
//     * Flush all the chunks.
func (i *Ingester) ShutdownHandler(w http.ResponseWriter, r *http.Request) {
	originalState := i.lifecycler.FlushOnShutdown()
	// We want to flush the chunks if transfer fails irrespective of original flag.
	i.lifecycler.SetFlushOnShutdown(true)
	_ = services.StopAndAwaitTerminated(context.Background(), i)
	i.lifecycler.SetFlushOnShutdown(originalState)
	w.WriteHeader(http.StatusNoContent)
}

// Push implements logproto.Pusher.
func (i *Ingester) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	instanceID, err := tenant.ID(ctx)
	if err != nil {
		return nil, err
	} else if i.readonly {
		return nil, ErrReadOnly
	}

	instance := i.getOrCreateInstance(instanceID)
	err = instance.Push(ctx, req)
	return &logproto.PushResponse{}, err
}

func (i *Ingester) getOrCreateInstance(instanceID string) *instance {
	inst, ok := i.getInstanceByID(instanceID)
	if ok {
		return inst
	}

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()
	inst, ok = i.instances[instanceID]
	if !ok {
		inst = newInstance(&i.cfg, instanceID, i.limiter, i.tenantConfigs, i.wal, i.metrics, i.flushOnShutdownSwitch, i.chunkFilter)
		i.instances[instanceID] = inst
	}
	return inst
}

// Query the ingests for log streams matching a set of matchers.
func (i *Ingester) Query(req *logproto.QueryRequest, queryServer logproto.Querier_QueryServer) error {
	// initialize stats collection for ingester queries and set grpc trailer with stats.
	ctx := stats.NewContext(queryServer.Context())
	defer stats.SendAsTrailer(ctx, queryServer)

	instanceID, err := tenant.ID(ctx)
	if err != nil {
		return err
	}

	instance := i.getOrCreateInstance(instanceID)
	itrs, err := instance.Query(ctx, logql.SelectLogParams{QueryRequest: req})
	if err != nil {
		return err
	}

	if start, end, ok := buildStoreRequest(i.cfg, req.Start, req.End, time.Now()); ok {
		storeReq := logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
			Selector:  req.Selector,
			Direction: req.Direction,
			Start:     start,
			End:       end,
			Limit:     req.Limit,
			Shards:    req.Shards,
		}}
		storeItr, err := i.store.SelectLogs(ctx, storeReq)
		if err != nil {
			return err
		}

		itrs = append(itrs, storeItr)
	}

	heapItr := iter.NewHeapIterator(ctx, itrs, req.Direction)

	defer listutil.LogErrorWithContext(ctx, "closing iterator", heapItr.Close)

	return sendBatches(ctx, heapItr, queryServer, req.Limit)
}

// QuerySample the ingesters for series from logs matching a set of matchers.
func (i *Ingester) QuerySample(req *logproto.SampleQueryRequest, queryServer logproto.Querier_QuerySampleServer) error {
	// initialize stats collection for ingester queries and set grpc trailer with stats.
	ctx := stats.NewContext(queryServer.Context())
	defer stats.SendAsTrailer(ctx, queryServer)

	instanceID, err := tenant.ID(ctx)
	if err != nil {
		return err
	}

	instance := i.getOrCreateInstance(instanceID)
	itrs, err := instance.QuerySample(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
	if err != nil {
		return err
	}

	if start, end, ok := buildStoreRequest(i.cfg, req.Start, req.End, time.Now()); ok {
		storeReq := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
			Start:    start,
			End:      end,
			Selector: req.Selector,
			Shards:   req.Shards,
		}}
		storeItr, err := i.store.SelectSamples(ctx, storeReq)
		if err != nil {
			return err
		}

		itrs = append(itrs, storeItr)
	}

	heapItr := iter.NewHeapSampleIterator(ctx, itrs)

	defer listutil.LogErrorWithContext(ctx, "closing iterator", heapItr.Close)

	return sendSampleBatches(ctx, heapItr, queryServer)
}

// boltdbShipperMaxLookBack returns a max look back period only if active index type is boltdb-shipper.
// max look back is limited to from time of boltdb-shipper config.
// It considers previous periodic config's from time if that also has index type set to boltdb-shipper.
func (i *Ingester) boltdbShipperMaxLookBack() time.Duration {
	activePeriodicConfigIndex := storage.ActivePeriodConfig(i.periodicConfigs)
	activePeriodicConfig := i.periodicConfigs[activePeriodicConfigIndex]
	if activePeriodicConfig.IndexType != shipper.BoltDBShipperType {
		return 0
	}

	startTime := activePeriodicConfig.From
	if activePeriodicConfigIndex != 0 && i.periodicConfigs[activePeriodicConfigIndex-1].IndexType == shipper.BoltDBShipperType {
		startTime = i.periodicConfigs[activePeriodicConfigIndex-1].From
	}

	maxLookBack := time.Since(startTime.Time.Time())
	return maxLookBack
}

// GetChunkIDs is meant to be used only when using an async store like boltdb-shipper.
func (i *Ingester) GetChunkIDs(ctx context.Context, req *logproto.GetChunkIDsRequest) (*logproto.GetChunkIDsResponse, error) {
	orgID, err := tenant.ID(ctx)
	if err != nil {
		return nil, err
	}

	boltdbShipperMaxLookBack := i.boltdbShipperMaxLookBack()
	if boltdbShipperMaxLookBack == 0 {
		return &logproto.GetChunkIDsResponse{}, nil
	}

	reqStart := req.Start
	reqStart = adjustQueryStartTime(boltdbShipperMaxLookBack, reqStart, time.Now())

	// parse the request
	start, end := listutil.RoundToMilliseconds(reqStart, req.End)
	matchers, err := logql.ParseMatchers(req.Matchers)
	if err != nil {
		return nil, err
	}

	// get chunk references
	chunksGroups, _, err := i.store.GetChunkRefs(ctx, orgID, start, end, matchers...)
	if err != nil {
		return nil, err
	}

	// build the response
	resp := logproto.GetChunkIDsResponse{ChunkIDs: []string{}}
	for _, chunks := range chunksGroups {
		for _, chk := range chunks {
			resp.ChunkIDs = append(resp.ChunkIDs, chk.ExternalKey())
		}
	}

	return &resp, nil
}

// Label returns the set of labels for the stream this ingester knows about.
func (i *Ingester) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	userID, err := tenant.ID(ctx)
	if err != nil {
		return nil, err
	}

	instance := i.getOrCreateInstance(userID)
	resp, err := instance.Label(ctx, req)
	if err != nil {
		return nil, err
	}

	// Only continue if the active index type is boltdb-shipper or QueryStore flag is true.
	boltdbShipperMaxLookBack := i.boltdbShipperMaxLookBack()
	if boltdbShipperMaxLookBack == 0 && !i.cfg.QueryStore {
		return resp, nil
	}

	// Only continue if the store is a chunk.Store
	var cs chunk.Store
	var ok bool
	if cs, ok = i.store.(chunk.Store); !ok {
		return resp, nil
	}

	maxLookBackPeriod := i.cfg.QueryStoreMaxLookBackPeriod
	if boltdbShipperMaxLookBack != 0 {
		maxLookBackPeriod = boltdbShipperMaxLookBack
	}
	// Adjust the start time based on QueryStoreMaxLookBackPeriod.
	start := adjustQueryStartTime(maxLookBackPeriod, *req.Start, time.Now())
	if start.After(*req.End) {
		// The request is older than we are allowed to query the store, just return what we have.
		return resp, nil
	}
	from, through := model.TimeFromUnixNano(start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano())
	var storeValues []string
	if req.Values {
		storeValues, err = cs.LabelValuesForMetricName(ctx, userID, from, through, "logs", req.Name)
		if err != nil {
			return nil, err
		}
	} else {
		storeValues, err = cs.LabelNamesForMetricName(ctx, userID, from, through, "logs")
		if err != nil {
			return nil, err
		}
	}

	return &logproto.LabelResponse{
		Values: listutil.MergeStringLists(resp.Values, storeValues),
	}, nil
}

// Series queries the ingester for log stream identifiers (label sets) matching a set of matchers
func (i *Ingester) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	instanceID, err := tenant.ID(ctx)
	if err != nil {
		return nil, err
	}

	instance := i.getOrCreateInstance(instanceID)
	return instance.Series(ctx, req)
}

// Check implements grpc_health_v1.HealthCheck.
func (*Ingester) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// Watch implements grpc_health_v1.HealthCheck.
func (*Ingester) Watch(*grpc_health_v1.HealthCheckRequest, grpc_health_v1.Health_WatchServer) error {
	return nil
}

// ReadinessHandler is used to indicate to k8s when the ingesters are ready for
// the addition removal of another ingester. Returns 204 when the ingester is
// ready, 500 otherwise.
func (i *Ingester) CheckReady(ctx context.Context) error {
	if s := i.State(); s != services.Running && s != services.Stopping {
		return fmt.Errorf("ingester not ready: %v", s)
	}
	return i.lifecycler.CheckReady(ctx)
}

func (i *Ingester) getInstanceByID(id string) (*instance, bool) {
	i.instancesMtx.RLock()
	defer i.instancesMtx.RUnlock()

	inst, ok := i.instances[id]
	return inst, ok
}

func (i *Ingester) getInstances() []*instance {
	i.instancesMtx.RLock()
	defer i.instancesMtx.RUnlock()

	instances := make([]*instance, 0, len(i.instances))
	for _, instance := range i.instances {
		instances = append(instances, instance)
	}
	return instances
}

// Tail logs matching given query
func (i *Ingester) Tail(req *logproto.TailRequest, queryServer logproto.Querier_TailServer) error {
	select {
	case <-i.tailersQuit:
		return errors.New("Ingester is stopping")
	default:
	}

	instanceID, err := tenant.ID(queryServer.Context())
	if err != nil {
		return err
	}

	instance := i.getOrCreateInstance(instanceID)
	tailer, err := newTailer(instanceID, req.Query, queryServer)
	if err != nil {
		return err
	}

	if err := instance.addNewTailer(queryServer.Context(), tailer); err != nil {
		return err
	}
	tailer.loop()
	return nil
}

// TailersCount returns count of active tail requests from a user
func (i *Ingester) TailersCount(ctx context.Context, in *logproto.TailersCountRequest) (*logproto.TailersCountResponse, error) {
	instanceID, err := tenant.ID(ctx)
	if err != nil {
		return nil, err
	}

	resp := logproto.TailersCountResponse{}

	instance, ok := i.getInstanceByID(instanceID)
	if ok {
		resp.Count = instance.openTailersCount()
	}

	return &resp, nil
}

// buildStoreRequest returns a store request from an ingester request, returns nit if QueryStore is set to false in configuration.
// The request may be truncated due to QueryStoreMaxLookBackPeriod which limits the range of request to make sure
// we only query enough to not miss any data and not add too to many duplicates by covering the who time range in query.
func buildStoreRequest(cfg Config, start, end, now time.Time) (time.Time, time.Time, bool) {
	if !cfg.QueryStore {
		return time.Time{}, time.Time{}, false
	}
	start = adjustQueryStartTime(cfg.QueryStoreMaxLookBackPeriod, start, now)

	if start.After(end) {
		return time.Time{}, time.Time{}, false
	}
	return start, end, true
}

func adjustQueryStartTime(maxLookBackPeriod time.Duration, start, now time.Time) time.Time {
	if maxLookBackPeriod > 0 {
		oldestStartTime := now.Add(-maxLookBackPeriod)
		if oldestStartTime.After(start) {
			return oldestStartTime
		}
	}
	return start
}
