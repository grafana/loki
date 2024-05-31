package pattern

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc/health/grpc_health_v1"

	ring_client "github.com/grafana/dskit/ring/client"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/clientpool"
	"github.com/grafana/loki/v3/pkg/pattern/metric"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"

	loki_iter "github.com/grafana/loki/v3/pkg/iter"
	pattern_iter "github.com/grafana/loki/v3/pkg/pattern/iter"
)

const readBatchSize = 1024

type Config struct {
	Enabled           bool                  `yaml:"enabled,omitempty" doc:"description=Whether the pattern ingester is enabled."`
	LifecyclerConfig  ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the pattern ingester will operate and where it will register for discovery."`
	ClientConfig      clientpool.Config     `yaml:"client_config,omitempty" doc:"description=Configures how the pattern ingester will connect to the ingesters."`
	ConcurrentFlushes int                   `yaml:"concurrent_flushes"`
	FlushCheckPeriod  time.Duration         `yaml:"flush_check_period"`

	MetricAggregation metric.AggregationConfig `yaml:"metric_aggregation,omitempty" doc:"description=Configures the metric aggregation and storage behavior of the pattern ingester."`
	// For testing.
	factory ring_client.PoolFactory `yaml:"-"`
}

// RegisterFlags registers pattern ingester related flags.
func (cfg *Config) RegisterFlags(fs *flag.FlagSet) {
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("pattern-ingester.", fs, util_log.Logger)
	cfg.ClientConfig.RegisterFlags(fs)
	cfg.MetricAggregation.RegisterFlagsWithPrefix(fs, "pattern-ingester.")

	fs.BoolVar(&cfg.Enabled, "pattern-ingester.enabled", false, "Flag to enable or disable the usage of the pattern-ingester component.")
	fs.IntVar(&cfg.ConcurrentFlushes, "pattern-ingester.concurrent-flushes", 32, "How many flushes can happen concurrently from each stream.")
	fs.DurationVar(&cfg.FlushCheckPeriod, "pattern-ingester.flush-check-period", 30*time.Second, "How often should the ingester see if there are any blocks to flush. The first flush check is delayed by a random time up to 0.8x the flush check period. Additionally, there is +/- 1% jitter added to the interval.")
}

func (cfg *Config) Validate() error {
	if cfg.LifecyclerConfig.RingConfig.ReplicationFactor != 1 {
		return errors.New("pattern ingester replication factor must be 1")
	}
	return cfg.LifecyclerConfig.Validate()
}

type Ingester struct {
	services.Service
	lifecycler *ring.Lifecycler

	lifecyclerWatcher *services.FailureWatcher

	cfg        Config
	registerer prometheus.Registerer
	logger     log.Logger

	instancesMtx sync.RWMutex
	instances    map[string]*instance

	// One queue per flush thread.  Fingerprint is used to
	// pick a queue.
	flushQueues     []*util.PriorityQueue
	flushQueuesDone sync.WaitGroup
	loopDone        sync.WaitGroup
	loopQuit        chan struct{}

	metrics *ingesterMetrics
}

func New(
	cfg Config,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
) (*Ingester, error) {
	metrics := newIngesterMetrics(registerer, metricsNamespace)
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	i := &Ingester{
		cfg:         cfg,
		logger:      log.With(logger, "component", "pattern-ingester"),
		registerer:  registerer,
		metrics:     metrics,
		instances:   make(map[string]*instance),
		flushQueues: make([]*util.PriorityQueue, cfg.ConcurrentFlushes),
		loopQuit:    make(chan struct{}),
	}
	i.Service = services.NewBasicService(i.starting, i.running, i.stopping)
	var err error
	i.lifecycler, err = ring.NewLifecycler(cfg.LifecyclerConfig, i, "pattern-ingester", "pattern-ring", true, i.logger, registerer)
	if err != nil {
		return nil, err
	}

	i.lifecyclerWatcher = services.NewFailureWatcher()
	i.lifecyclerWatcher.WatchService(i.lifecycler)

	return i, nil
}

// ServeHTTP implements the pattern ring status page.
func (i *Ingester) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i.lifecycler.ServeHTTP(w, r)
}

func (i *Ingester) starting(ctx context.Context) error {
	// pass new context to lifecycler, so that it doesn't stop automatically when Ingester's service context is done
	err := i.lifecycler.StartAsync(context.Background())
	if err != nil {
		return err
	}

	err = i.lifecycler.AwaitRunning(ctx)
	if err != nil {
		return err
	}
	i.initFlushQueues()
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

	close(i.loopQuit)
	i.loopDone.Wait()
	return serviceError
}

func (i *Ingester) stopping(_ error) error {
	err := services.StopAndAwaitTerminated(context.Background(), i.lifecycler)
	for _, flushQueue := range i.flushQueues {
		flushQueue.Close()
	}
	i.flushQueuesDone.Wait()
	return err
}

func (i *Ingester) loop() {
	defer i.loopDone.Done()

	// Delay the first flush operation by up to 0.8x the flush time period.
	// This will ensure that multiple ingesters started at the same time do not
	// flush at the same time. Flushing at the same time can cause concurrently
	// writing the same chunk to object storage, which in AWS S3 leads to being
	// rate limited.
	jitter := time.Duration(rand.Int63n(int64(float64(i.cfg.FlushCheckPeriod.Nanoseconds()) * 0.8)))
	initialDelay := time.NewTimer(jitter)
	defer initialDelay.Stop()

	level.Info(i.logger).Log("msg", "sleeping for initial delay before starting periodic flushing", "delay", jitter)

	select {
	case <-initialDelay.C:
		// do nothing and continue with flush loop
	case <-i.loopQuit:
		// ingester stopped while waiting for initial delay
		return
	}

	// Add +/- 20% of flush interval as jitter.
	// The default flush check period is 30s so max jitter will be 6s.
	j := i.cfg.FlushCheckPeriod / 5
	flushTicker := util.NewTickerWithJitter(i.cfg.FlushCheckPeriod, j)
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

func (i *Ingester) TransferOut(_ context.Context) error {
	// todo may be.
	return ring.ErrTransferDisabled
}

func (i *Ingester) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return &logproto.PushResponse{}, err
	}
	return &logproto.PushResponse{}, instance.Push(ctx, req)
}

func (i *Ingester) Query(req *logproto.QueryPatternsRequest, stream logproto.Pattern_QueryServer) error {
	ctx := stream.Context()
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return err
	}
	iterator, err := instance.Iterator(ctx, req)
	if err != nil {
		return err
	}
	defer util.LogErrorWithContext(ctx, "closing iterator", iterator.Close)
	return sendPatternSample(ctx, iterator, stream)
}

func (i *Ingester) QuerySample(
	req *logproto.QuerySamplesRequest,
	stream logproto.Pattern_QuerySampleServer,
) error {
	ctx := stream.Context()
	instanceID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}
	instance, err := i.GetOrCreateInstance(instanceID)
	if err != nil {
		return err
	}

	expr, err := syntax.ParseSampleExpr(req.Query)
	if err != nil {
		return err
	}

	iterator, err := instance.QuerySample(ctx, expr, req) // this is returning a first value of 0,0
	if err != nil {
		return err
	}

	// TODO(twhitney): query store
	// if start, end, ok := buildStoreRequest(i.cfg, req.Start, req.End, time.Now()); ok {
	// 	storeReq := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
	// 		Start:    start,
	// 		End:      end,
	// 		Selector: req.Selector,
	// 		Shards:   req.Shards,
	// 		Deletes:  req.Deletes,
	// 		Plan:     req.Plan,
	// 	}}
	// 	storeItr, err := i.store.SelectSamples(ctx, storeReq)
	// 	if err != nil {
	// 		util.LogErrorWithContext(ctx, "closing iterator", it.Close)
	// 		return err
	// 	}

	// 	it = iter.NewMergeSampleIterator(ctx, []iter.SampleIterator{it, storeItr})
	// }

	defer util.LogErrorWithContext(ctx, "closing iterator", iterator.Close)
	return sendMetricSamples(ctx, iterator, stream)
}

func sendPatternSample(ctx context.Context, it pattern_iter.Iterator, stream logproto.Pattern_QueryServer) error {
	for ctx.Err() == nil {
		batch, err := pattern_iter.ReadBatch(it, readBatchSize)
		if err != nil {
			return err
		}
		if err := stream.Send(batch); err != nil && err != context.Canceled {
			return err
		}
		if len(batch.Series) == 0 {
			return nil
		}
	}
	return nil
}

func sendMetricSamples(
	ctx context.Context,
	it loki_iter.SampleIterator,
	stream logproto.Pattern_QuerySampleServer,
) error {
	for ctx.Err() == nil {
		batch, err := pattern_iter.ReadMetricsBatch(it, readBatchSize)
		if err != nil {
			return err
		}
		if err := stream.Send(batch); err != nil && err != context.Canceled {
			return err
		}
		if len(batch.Series) == 0 {
			return nil
		}
	}
	return nil
}

func (i *Ingester) GetOrCreateInstance(instanceID string) (*instance, error) { //nolint:revive
	inst, ok := i.getInstanceByID(instanceID)
	if ok {
		return inst, nil
	}

	i.instancesMtx.Lock()
	defer i.instancesMtx.Unlock()
	inst, ok = i.instances[instanceID]
	if !ok {
		var err error
		inst, err = newInstance(instanceID, i.logger, i.metrics, i.cfg.MetricAggregation)
		if err != nil {
			return nil, err
		}
		i.instances[instanceID] = inst
	}
	return inst, nil
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
