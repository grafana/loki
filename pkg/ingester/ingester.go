package ingester

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/user"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/util/validation"
)

// ErrReadOnly is returned when the ingester is shutting down and a push was
// attempted.
var ErrReadOnly = errors.New("Ingester is shutting down")

var readinessProbeSuccess = []byte("Ready")

var flushQueueLength = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "cortex_ingester_flush_queue_length",
	Help: "The total number of series pending in the flush queue.",
})

// Config for an ingester.
type Config struct {
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`

	// Config for transferring chunks.
	MaxTransferRetries int `yaml:"max_transfer_retries,omitempty"`

	ConcurrentFlushes int           `yaml:"concurrent_flushes"`
	FlushCheckPeriod  time.Duration `yaml:"flush_check_period"`
	FlushOpTimeout    time.Duration `yaml:"flush_op_timeout"`
	RetainPeriod      time.Duration `yaml:"chunk_retain_period"`
	MaxChunkIdle      time.Duration `yaml:"chunk_idle_period"`
	BlockSize         int           `yaml:"chunk_block_size"`
	TargetChunkSize   int           `yaml:"chunk_target_size"`
	ChunkEncoding     string        `yaml:"chunk_encoding"`
	MaxChunkAge       time.Duration `yaml:"max_chunk_age"`

	// Synchronization settings. Used to make sure that ingesters cut their chunks at the same moments.
	SyncPeriod         time.Duration `yaml:"sync_period"`
	SyncMinUtilization float64       `yaml:"sync_min_utilization"`

	MaxReturnedErrors int `yaml:"max_returned_stream_errors"`

	// For testing, you can override the address and ID of this ingester.
	ingesterClientFactory func(cfg client.Config, addr string) (grpc_health_v1.HealthClient, error)
}

// RegisterFlags registers the flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlags(f)

	f.IntVar(&cfg.MaxTransferRetries, "ingester.max-transfer-retries", 10, "Number of times to try and transfer chunks before falling back to flushing. If set to 0 or negative value, transfers are disabled.")
	f.IntVar(&cfg.ConcurrentFlushes, "ingester.concurrent-flushed", 16, "")
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
}

// Ingester builds chunks for incoming log streams.
type Ingester struct {
	cfg          Config
	clientConfig client.Config

	shutdownMtx  sync.Mutex // Allows processes to grab a lock and prevent a shutdown
	instancesMtx sync.RWMutex
	instances    map[string]*instance
	readonly     bool

	lifecycler *ring.Lifecycler
	store      ChunkStore

	done     sync.WaitGroup
	quit     chan struct{}
	quitting chan struct{}

	// One queue per flush thread.  Fingerprint is used to
	// pick a queue.
	flushQueues     []*util.PriorityQueue
	flushQueuesDone sync.WaitGroup

	limiter *Limiter
	factory func() chunkenc.Chunk
}

// ChunkStore is the interface we need to store chunks.
type ChunkStore interface {
	Put(ctx context.Context, chunks []chunk.Chunk) error
}

// New makes a new Ingester.
func New(cfg Config, clientConfig client.Config, store ChunkStore, limits *validation.Overrides) (*Ingester, error) {
	if cfg.ingesterClientFactory == nil {
		cfg.ingesterClientFactory = client.New
	}
	enc, err := chunkenc.ParseEncoding(cfg.ChunkEncoding)
	if err != nil {
		return nil, err
	}

	i := &Ingester{
		cfg:          cfg,
		clientConfig: clientConfig,
		instances:    map[string]*instance{},
		store:        store,
		quit:         make(chan struct{}),
		flushQueues:  make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		quitting:     make(chan struct{}),
		factory: func() chunkenc.Chunk {
			return chunkenc.NewMemChunkSize(enc, cfg.BlockSize, cfg.TargetChunkSize)
		},
	}

	i.flushQueuesDone.Add(cfg.ConcurrentFlushes)
	for j := 0; j < cfg.ConcurrentFlushes; j++ {
		i.flushQueues[j] = util.NewPriorityQueue(flushQueueLength)
		go i.flushLoop(j)
	}

	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "ingester", ring.IngesterRingKey, true)
	if err != nil {
		return nil, err
	}

	i.lifecycler.Start()

	// Now that the lifecycler has been created, we can create the limiter
	// which depends on it.
	i.limiter = NewLimiter(limits, i.lifecycler, cfg.LifecyclerConfig.RingConfig.ReplicationFactor)

	i.done.Add(1)
	go i.loop()

	return i, nil
}

func (i *Ingester) loop() {
	defer i.done.Done()

	flushTicker := time.NewTicker(i.cfg.FlushCheckPeriod)
	defer flushTicker.Stop()

	for {
		select {
		case <-flushTicker.C:
			i.sweepUsers(false)

		case <-i.quit:
			return
		}
	}
}

// Shutdown stops the ingester.
func (i *Ingester) Shutdown() {
	close(i.quit)
	i.done.Wait()

	i.lifecycler.Shutdown()
}

// Stopping helps cleaning up resources before actual shutdown
func (i *Ingester) Stopping() {
	close(i.quitting)
	for _, instance := range i.getInstances() {
		instance.closeTailers()
	}
}

// Push implements logproto.Pusher.
func (i *Ingester) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	instanceID, err := user.ExtractOrgID(ctx)
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
		inst = newInstance(&i.cfg, instanceID, i.factory, i.limiter, i.cfg.SyncPeriod, i.cfg.SyncMinUtilization)
		i.instances[instanceID] = inst
	}
	return inst
}

// Query the ingests for log streams matching a set of matchers.
func (i *Ingester) Query(req *logproto.QueryRequest, queryServer logproto.Querier_QueryServer) error {
	instanceID, err := user.ExtractOrgID(queryServer.Context())
	if err != nil {
		return err
	}

	instance := i.getOrCreateInstance(instanceID)
	return instance.Query(req, queryServer)
}

// Label returns the set of labels for the stream this ingester knows about.
func (i *Ingester) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	instanceID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	instance := i.getOrCreateInstance(instanceID)
	return instance.Label(ctx, req)
}

// Series queries the ingester for log stream identifiers (label sets) matching a set of matchers
func (i *Ingester) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	instanceID, err := user.ExtractOrgID(ctx)
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
// the addition removal of another ingester. Returns 200 when the ingester is
// ready, 500 otherwise.
func (i *Ingester) ReadinessHandler(w http.ResponseWriter, r *http.Request) {
	if err := i.lifecycler.CheckReady(r.Context()); err != nil {
		http.Error(w, "Not ready: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(readinessProbeSuccess); err != nil {
		level.Error(util.Logger).Log("msg", "error writing success message", "error", err)
	}
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
	case <-i.quitting:
		return errors.New("Ingester is stopping")
	default:
	}

	instanceID, err := user.ExtractOrgID(queryServer.Context())
	if err != nil {
		return err
	}

	instance := i.getOrCreateInstance(instanceID)
	tailer, err := newTailer(instanceID, req.Query, queryServer)
	if err != nil {
		return err
	}

	instance.addNewTailer(tailer)
	tailer.loop()
	return nil
}

// TailersCount returns count of active tail requests from a user
func (i *Ingester) TailersCount(ctx context.Context, in *logproto.TailersCountRequest) (*logproto.TailersCountResponse, error) {
	instanceID, err := user.ExtractOrgID(ctx)
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
