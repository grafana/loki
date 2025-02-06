package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/dns"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
	dskit_log "github.com/grafana/dskit/log"
	"github.com/grafana/dskit/netutil"
	"github.com/grafana/dskit/ring"
	ringclient "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/limits/frontend"
	frontendclient "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

const (
	metricsNamespace = "loki_stream_metadata_generator_"
	ingesterRingName = "ingester"
)

type metrics struct {
	activeStreamsTotal   *prometheus.CounterVec
	kafkaWriteLatency    prometheus.Histogram
	kafkaWriteBytesTotal prometheus.Counter
}

func newMetrics(reg prometheus.Registerer) *metrics {
	return &metrics{
		activeStreamsTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "active_streams_total",
			Help: "The total number of active streams",
		}, []string{"tenant"}),
		kafkaWriteLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Name:                            "kafka_write_metadata_latency_seconds",
			Help:                            "Latency to write an incoming request to the ingest metadata storage.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMinResetDuration: 1 * time.Hour,
			NativeHistogramMaxBucketNumber:  100,
			Buckets:                         prometheus.DefBuckets,
		}),
		kafkaWriteBytesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "kafka_write_metadata_bytes_total",
			Help:      "Total number of bytes sent to the ingest metadata storage.",
		}),
	}
}

type config struct {
	NumTenants                int      `yaml:"num_tenants"`
	QPSPerTenant              int      `yaml:"qps_per_tenant"`
	BatchSize                 int      `yaml:"batch_size"`
	StreamsPerTenant          int      `yaml:"streams_per_tenant"`
	StreamLabels              []string `yaml:"stream_labels"`
	MaxGlobalStreamsPerTenant int      `yaml:"max_global_streams_per_tenant"`

	LogLevel       dskit_log.Level `yaml:"log_level,omitempty"`
	HTTPListenPort int             `yaml:"http_listen_port,omitempty"`

	// Memberlist config
	MemberlistKV memberlist.KVConfig `yaml:"memberlist,omitempty"`

	// Kafka & partition ring config
	Kafka               kafka.Config         `yaml:"kafka,omitempty"`
	PartitionRingConfig partitionring.Config `yaml:"partition_ring,omitempty"`

	// Ingester ring config
	IngesterLifecyclerConfig ring.LifecyclerConfig `yaml:"ingester_lifecycler,omitempty"`

	// Frontend ring config
	FrontendLifecyclerConfig ring.LifecyclerConfig `yaml:"frontend_lifecycler,omitempty"`
	FrontendClientConfig     frontendclient.Config `yaml:"frontend_client,omitempty"`
}

type streamLabelsFlag []string

func (s *streamLabelsFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *streamLabelsFlag) Set(value string) error {
	*s = strings.Split(value, ",")
	return nil
}

func (c *config) RegisterFlags(f *flag.FlagSet, logger log.Logger) {
	f.IntVar(&c.NumTenants, "tenants.total", 1, "Number of tenants to generate metadata for")
	f.IntVar(&c.QPSPerTenant, "tenants.qps", 10, "Number of QPS per tenant")
	f.IntVar(&c.BatchSize, "tenants.streams.batch-size", 100, "Number of streams to send to Kafka per tick")
	f.IntVar(&c.StreamsPerTenant, "tenants.streams.total", 100, "Number of streams per tenant")
	f.IntVar(&c.MaxGlobalStreamsPerTenant, "tenants.max-global-streams", 1000, "Maximum number of global streams per tenant")
	f.IntVar(&c.HTTPListenPort, "http-listen-port", 3100, "HTTP Listener port")

	// Set default stream labels
	defaultLabels := []string{"cluster", "namespace", "job", "instance"}
	c.StreamLabels = defaultLabels

	// Create a custom flag for stream labels
	streamLabels := (*streamLabelsFlag)(&c.StreamLabels)
	f.Var(streamLabels, "stream.labels", fmt.Sprintf("The labels to generate for each stream (comma-separated) (default: %s)", strings.Join(defaultLabels, ",")))

	c.LogLevel.RegisterFlags(f)
	c.Kafka.RegisterFlags(f)
	c.PartitionRingConfig.RegisterFlagsWithPrefix("", f)
	c.IngesterLifecyclerConfig.RegisterFlagsWithPrefix("ingester-ring.", f, logger)
	c.FrontendLifecyclerConfig.RegisterFlagsWithPrefix("frontend-ring.", f, logger)
	c.FrontendClientConfig.RegisterFlagsWithPrefix("frontend", f)
	c.MemberlistKV.RegisterFlags(f)
}

type generator struct {
	services.Service

	cfg     config
	logger  log.Logger
	metrics *metrics

	// payload
	streams map[string][]distributor.KeyedStream

	// kafka
	writer               *client.Producer
	partitionRing        ring.PartitionRingReader
	partitionRingWatcher *ring.PartitionRingWatcher
	partitionRingKV      kv.Client

	// ring
	memberlistKV *memberlist.KVInitService
	ingesterRing *ring.Ring
	frontendRing *ring.Ring

	frontentClientPool *ringclient.Pool

	// service
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// Service internals
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func newStreamMetaGen(cfg config, writer *client.Producer, logger log.Logger, reg prometheus.Registerer) (*generator, error) {
	s := &generator{
		cfg:     cfg,
		writer:  writer,
		logger:  logger,
		metrics: newMetrics(reg),
	}

	var err error

	cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
		analytics.JSONCodec,
		ring.GetPartitionRingCodec(),
	}

	cfg.MemberlistKV.AdvertiseAddr, err = netutil.GetFirstAddressOf(cfg.IngesterLifecyclerConfig.InfNames, logger, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance address: %w", err)
	}

	// Init Memberlist KV
	provider := dns.NewProvider(logger, reg, dns.GolangResolverType)
	s.memberlistKV = memberlist.NewKVInitService(&cfg.MemberlistKV, logger, provider, reg)

	cfg.IngesterLifecyclerConfig.RingConfig.KVStore.MemberlistKV = s.memberlistKV.GetMemberlistKV
	cfg.PartitionRingConfig.KVStore.MemberlistKV = s.memberlistKV.GetMemberlistKV
	cfg.FrontendLifecyclerConfig.RingConfig.KVStore.MemberlistKV = s.memberlistKV.GetMemberlistKV

	// Init KVStore client
	regKV := kv.RegistererWithKVName(reg, ingester.PartitionRingName+"-watcher")
	kvClient, err := kv.NewClient(cfg.PartitionRingConfig.KVStore, ring.GetPartitionRingCodec(), regKV, logger)
	if err != nil {
		return nil, fmt.Errorf("creating KV store for partitions ring watcher: %w", err)
	}

	// Init partition ingester ring
	s.ingesterRing, err = ring.New(cfg.IngesterLifecyclerConfig.RingConfig, ingesterRingName, ingester.RingKey, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("creating ingester ring: %w", err)
	}

	// Init Partition Ring and Watcher
	s.partitionRingKV = kvClient
	s.partitionRingWatcher = ring.NewPartitionRingWatcher(ingester.PartitionRingName, ingester.PartitionRingKey, s.partitionRingKV, logger, reg)
	s.partitionRing = ring.NewPartitionInstanceRing(s.partitionRingWatcher, s.ingesterRing, cfg.IngesterLifecyclerConfig.RingConfig.HeartbeatTimeout)

	// Init Frontend Ring
	s.frontendRing, err = ring.New(cfg.FrontendLifecyclerConfig.RingConfig, frontend.RingName, frontend.RingKey, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("creating ingest limits frontend ring: %w", err)
	}

	factory := ringclient.PoolAddrFunc(func(addr string) (ringclient.PoolClient, error) {
		return frontendclient.NewIngestLimitsFrontendClient(cfg.FrontendClientConfig, addr)
	})

	s.frontentClientPool = frontendclient.NewIngestLimitsFrontendClientPool(frontend.RingName, cfg.FrontendClientConfig.PoolConfig, s.frontendRing, factory, logger)

	// Init services
	srvs := []services.Service{
		s.memberlistKV,
		s.ingesterRing,
		s.partitionRingWatcher,
		s.frontendRing,
		s.frontentClientPool,
	}
	s.subservices, err = services.NewManager(srvs...)
	if err != nil {
		return nil, fmt.Errorf("creating subservices: %w", err)
	}

	s.subservicesWatcher = services.NewFailureWatcher()
	s.subservicesWatcher.WatchManager(s.subservices)

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

var frontendReadOp = ring.NewOp([]ring.InstanceState{ring.ACTIVE}, nil)

func (s *generator) getFrontendClient() (*frontendclient.IngestLimitsFrontendClient, error) {
	instances, err := s.frontendRing.GetAllHealthy(frontendReadOp)
	if err != nil {
		return nil, fmt.Errorf("failed to get ingest limits frontend instances: %w", err)
	}

	rand.Shuffle(len(instances.Instances), func(i, j int) {
		instances.Instances[i], instances.Instances[j] = instances.Instances[j], instances.Instances[i]
	})

	client, err := s.frontentClientPool.GetClientForInstance(instances.Instances[0])
	if err != nil {
		return nil, fmt.Errorf("failed to get ingest limits frontend client: %w", err)
	}

	return client.(*frontendclient.IngestLimitsFrontendClient), nil
}

func (s *generator) starting(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Generate streams for each tenant
	s.streams = make(map[string][]distributor.KeyedStream)
	for i := 0; i < s.cfg.NumTenants; i++ {
		tenantID := fmt.Sprintf("tenant-%d", i)
		s.streams[tenantID] = generateStreamsForTenant(tenantID, s.cfg.StreamsPerTenant, s.cfg.StreamLabels)
	}

	return services.StartManagerAndAwaitHealthy(s.ctx, s.subservices)
}

func (s *generator) running(ctx context.Context) error {
	// Create error channel to collect errors from goroutines
	errCh := make(chan error, s.cfg.NumTenants)

	// Start a goroutine for each tenant
	for tenantID, streams := range s.streams {
		s.wg.Add(1)
		go func(tenantID string, streams []distributor.KeyedStream) {
			defer s.wg.Done()

			client, err := s.getFrontendClient()
			if err != nil {
				errCh <- err
				return
			}

			userCtx, err := user.InjectIntoGRPCRequest(user.InjectOrgID(ctx, tenantID))
			if err != nil {
				errCh <- err
				return
			}

			// Create a ticker for rate limiting based on QPSPerTenant
			ticker := time.NewTicker(time.Second / time.Duration(s.cfg.QPSPerTenant))
			defer ticker.Stop()

			// Keep track of current stream index and whether we've completed first pass
			streamIdx := 0
			firstPassComplete := false

			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					if streamIdx >= len(streams) {
						streamIdx = 0
						firstPassComplete = true
					}

					batchSize := s.cfg.BatchSize
					if streamIdx+batchSize > len(streams) {
						batchSize = len(streams) - streamIdx
					}

					streamsBatch := streams[streamIdx : streamIdx+batchSize]

					var streamMetadata []*logproto.StreamMetadataWithSize
					for _, stream := range streamsBatch {
						streamMetadata = append(streamMetadata, &logproto.StreamMetadataWithSize{
							StreamHash: stream.HashNoShard,
						})
					}

					req := &logproto.ExceedsLimitsRequest{
						Tenant:  tenantID,
						Streams: streamMetadata,
					}

					// Check if the stream exceeds limits
					if client != nil {
						resp, err := client.ExceedsLimits(userCtx, req)
						if err != nil {
							errCh <- errors.Wrapf(err, "failed to check limits for tenant %s", tenantID)
							return
						}

						if len(resp.RejectedStreams) > 0 {
							var rejectedStreamsMsg string
							for _, rejectedStream := range resp.RejectedStreams {
								rejectedStreamsMsg += fmt.Sprintf("%d (%s), ", rejectedStream.StreamHash, rejectedStream.Reason)
							}

							level.Info(s.logger).Log("msg", "Stream exceeds limits", "tenant", tenantID, "stream", streamIdx, "rejected", rejectedStreamsMsg)
						}
					}

					// Send single stream to Kafka
					s.sendStreamsToKafka(ctx, streamsBatch, tenantID, errCh)
					level.Info(s.logger).Log("msg", "Sent streams to Kafka", "tenant", tenantID, "batch_size", batchSize, "stream_idx", streamIdx, "time", t.Format(time.RFC3339))

					// Only increment during the first pass
					if !firstPassComplete {
						s.metrics.activeStreamsTotal.WithLabelValues(tenantID).Add(float64(batchSize))
					}

					streamIdx += batchSize
				}
			}
		}(tenantID, streams)
	}

	// Wait for context cancellation, subservice failure, or tenant error
	select {
	case <-ctx.Done():
		return nil
	case err := <-s.subservicesWatcher.Chan():
		return errors.Wrap(err, "stream-metadata-generator subservice failed")
	case err := <-errCh:
		level.Error(s.logger).Log("msg", "stream-metadata-generator error", "err", err)
		return err
	}
}

func (s *generator) sendStreamsToKafka(ctx context.Context, streams []distributor.KeyedStream, tenant string, errCh chan error) {
	for _, stream := range streams {
		go func(stream distributor.KeyedStream) {
			partitionID, err := s.partitionRing.PartitionRing().ActivePartitionForKey(stream.RingToken)
			if err != nil {
				errCh <- fmt.Errorf("failed to find active partition for stream: %w", err)
				return
			}

			startTime := time.Now()

			// Add metadata record
			metadataRecord := kafka.EncodeStreamMetadata(partitionID, s.cfg.Kafka.Topic, tenant, stream.HashNoShard)

			// Send to Kafka
			produceResults := s.writer.ProduceSync(ctx, []*kgo.Record{metadataRecord})

			if count, sizeBytes := successfulProduceRecordsStats(produceResults); count > 0 {
				s.metrics.kafkaWriteLatency.Observe(time.Since(startTime).Seconds())
				s.metrics.kafkaWriteBytesTotal.Add(float64(sizeBytes))
			}

			// Check for errors
			for _, result := range produceResults {
				if result.Err != nil {
					errCh <- fmt.Errorf("failed to write stream metadata to kafka: %w", result.Err)
					return
				}
			}
		}(stream)
	}
}

func successfulProduceRecordsStats(results kgo.ProduceResults) (count, sizeBytes int) {
	for _, res := range results {
		if res.Err == nil && res.Record != nil {
			count++
			sizeBytes += len(res.Record.Value)
		}
	}

	return
}

func (s *generator) stopping(_ error) error {
	s.cancel()
	s.wg.Wait()

	if s.writer != nil {
		s.writer.Close()
	}

	return services.StopManagerAndAwaitStopped(context.Background(), s.subservices)
}

func newKafkaWriter(cfg kafka.Config, logger log.Logger, reg prometheus.Registerer) (*client.Producer, error) {
	// Create a new Kafka client with writer configuration
	// Using same settings as distributor for max inflight requests
	maxInflightProduceRequests := 20

	// Create the Kafka client
	kafkaClient, err := client.NewWriterClient(cfg, maxInflightProduceRequests, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	// Create a producer with 100MB buffer limit
	producer := client.NewProducer(kafkaClient, cfg.ProducerMaxBufferedBytes, reg)
	return producer, nil
}

func generateStreamsForTenant(tenantID string, streamsPerTenant int, streamLabels []string) []distributor.KeyedStream {
	streams := make([]distributor.KeyedStream, streamsPerTenant)

	for i := 0; i < streamsPerTenant; i++ {
		// Generate static label values for this stream
		labelValues := make([]string, len(streamLabels))
		for j, label := range streamLabels {
			labelValues[j] = fmt.Sprintf("%s=\"%s-%d-%d\"", label, label, i, j)
		}

		// Create the labels string in the format {label1="value1",label2="value2"}
		labelsStr := fmt.Sprintf("{%s}", strings.Join(labelValues, ","))

		// Parse the labels to get the hash
		lbs := labels.FromMap(map[string]string{})
		for j, label := range streamLabels {
			lbs = append(lbs, labels.Label{Name: label, Value: fmt.Sprintf("%s-%d-%d", label, i, j)})
		}
		sort.Sort(lbs)

		// Create the stream
		stream := logproto.Stream{
			Labels: labelsStr,
			Hash:   lbs.Hash(),
		}

		// Create the keyed stream
		streams[i] = distributor.KeyedStream{
			RingToken:   lokiring.TokenFor(tenantID, labelsStr),
			HashNoShard: stream.Hash,
			Stream:      stream,
		}
	}

	return streams
}

func main() {
	logger := log.NewLogfmtLogger(os.Stdout)

	cfg := config{}
	cfg.RegisterFlags(flag.CommandLine, logger)
	flag.Parse()

	logger = level.NewFilter(logger, cfg.LogLevel.Option)

	if err := util.LogConfig(&cfg); err != nil {
		level.Error(logger).Log("msg", "Error printing config", "err", err)
		os.Exit(1)
	}

	// Create a new registry for Kafka metrics
	promReg := prometheus.NewRegistry()
	reg := prometheus.WrapRegistererWithPrefix(metricsNamespace, promReg)

	// Create the Kafka writer
	writer, err := newKafkaWriter(cfg.Kafka, logger, reg)
	if err != nil {
		level.Error(logger).Log("msg", "Error creating Kafka writer", "err", err)
		os.Exit(1)
	}
	defer writer.Close()

	// Create and start the stream metadata generator service
	gen, err := newStreamMetaGen(cfg, writer, logger, reg)
	if err != nil {
		level.Error(logger).Log("msg", "Error creating stream metadata generator", "err", err)
		os.Exit(1)
	}

	// Start the service and wait for it to be ready
	if err := services.StartAndAwaitRunning(context.Background(), gen); err != nil {
		level.Error(logger).Log("msg", "Error starting stream metadata generator", "err", err)
		os.Exit(1)
	}

	// Create HTTP server for metrics
	httpListenAddr := fmt.Sprintf(":%d", cfg.HTTPListenPort)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))
	mux.Handle("/memberlist", gen.memberlistKV)
	mux.Handle("/ring", gen.ingesterRing)
	mux.Handle("/ingest-limits-ring", gen.frontendRing)
	mux.Handle("/partition-ring", ring.NewPartitionRingPageHandler(
		gen.partitionRingWatcher,
		ring.NewPartitionRingEditor(
			ingester.PartitionRingKey,
			gen.partitionRingKV,
		),
	))

	server := &http.Server{
		Addr:    httpListenAddr,
		Handler: mux,
	}

	// Start HTTP server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			level.Error(logger).Log("msg", "Error starting metrics server", "err", err)
			os.Exit(1)
		}
	}()

	level.Info(logger).Log("msg", "Started metrics server", "addr", httpListenAddr)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	// Gracefully shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		level.Error(logger).Log("msg", "Error shutting down metrics server", "err", err)
	}

	// Stop the service gracefully
	if err := services.StopAndAwaitTerminated(context.Background(), gen); err != nil {
		level.Error(logger).Log("msg", "Error stopping stream metadata generator", "err", err)
		os.Exit(1)
	}
}
