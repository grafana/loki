package main

import (
	"context"
	"flag"
	"fmt"
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
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/ingester"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partitionring"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
)

const (
	metricsNamespace = "loki_streammetagen_"
	ingesterRingName = "ingester"
)

var (
	streamLabels = []string{"cluster", "namespace", "job", "instance"}
)

type Config struct {
	NumTenants       int
	QPSPerTenant     int
	StreamsPerTenant int

	LogLevel dskit_log.Level `yaml:"log_level"`

	MemberlistKV        memberlist.KVConfig
	Kafka               kafka.Config
	PartitionRingConfig partitionring.Config  `yaml:"partition_ring" category:"experimental"`
	LifecyclerConfig    ring.LifecyclerConfig `yaml:"lifecycler,omitempty" doc:"description=Configures how the lifecycle of the ingester will operate and where it will register for discovery."`
}

func (c *Config) RegisterFlags(f *flag.FlagSet, logger log.Logger) {
	f.IntVar(&c.NumTenants, "tenants", 1, "Number of tenants to generate metadata for")
	f.IntVar(&c.QPSPerTenant, "qps", 10, "Number of QPS per tenant")
	f.IntVar(&c.StreamsPerTenant, "streams", 100, "Number of streams per tenant")

	c.LogLevel.RegisterFlags(f)
	c.Kafka.RegisterFlags(f)
	c.PartitionRingConfig.RegisterFlagsWithPrefix("streammetagen_", f)
	c.LifecyclerConfig.RegisterFlagsWithPrefix("streammetagen_", f, logger)
	c.MemberlistKV.RegisterFlags(f)
}

type streamMetaGen struct {
	services.Service

	cfg    Config
	logger log.Logger

	// payload
	streams map[string][]distributor.KeyedStream

	// kafka
	writer *client.Producer

	// ring
	memberlistKV         *memberlist.KVInitService
	ring                 *ring.Ring
	partitionRing        ring.PartitionRingReader
	partitionRingWatcher *ring.PartitionRingWatcher

	// service
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// Service internals
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

func newStreamMetaGen(cfg Config, writer *client.Producer, logger log.Logger, reg prometheus.Registerer) (*streamMetaGen, error) {
	s := &streamMetaGen{
		cfg:    cfg,
		writer: writer,
		logger: logger,
	}

	var err error

	cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
		analytics.JSONCodec,
		ring.GetPartitionRingCodec(),
	}

	cfg.MemberlistKV.AdvertiseAddr, err = netutil.GetFirstAddressOf(cfg.LifecyclerConfig.InfNames, logger, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance address: %w", err)
	}

	// Init Memberlist KV
	provider := dns.NewProvider(logger, reg, dns.GolangResolverType)
	s.memberlistKV = memberlist.NewKVInitService(&cfg.MemberlistKV, logger, provider, reg)

	cfg.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = s.memberlistKV.GetMemberlistKV
	cfg.PartitionRingConfig.KVStore.MemberlistKV = s.memberlistKV.GetMemberlistKV

	// Init Ring
	s.ring, err = ring.New(cfg.LifecyclerConfig.RingConfig, ingesterRingName, ingester.RingKey, logger, reg)
	if err != nil {
		return nil, fmt.Errorf("creating ring: %w", err)
	}

	// Init KVStore client
	regKV := kv.RegistererWithKVName(reg, ingester.PartitionRingName+"-watcher")
	kvClient, err := kv.NewClient(cfg.PartitionRingConfig.KVStore, ring.GetPartitionRingCodec(), regKV, logger)
	if err != nil {
		return nil, fmt.Errorf("creating KV store for partitions ring watcher: %w", err)
	}

	// Init Partition Ring and Watcher
	s.partitionRingWatcher = ring.NewPartitionRingWatcher(ingester.PartitionRingName, ingester.PartitionRingKey, kvClient, logger, reg)
	s.partitionRing = ring.NewPartitionInstanceRing(s.partitionRingWatcher, s.ring, cfg.LifecyclerConfig.RingConfig.HeartbeatTimeout)

	// Init services
	srvs := []services.Service{
		s.memberlistKV,
		s.ring,
		s.partitionRingWatcher,
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

func (s *streamMetaGen) starting(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Generate streams for each tenant
	s.streams = make(map[string][]distributor.KeyedStream)
	for i := 0; i < s.cfg.NumTenants; i++ {
		tenantID := fmt.Sprintf("tenant-%d", i)
		s.streams[tenantID] = generateStreamsForTenant(tenantID, s.cfg.StreamsPerTenant)
	}

	return services.StartManagerAndAwaitHealthy(s.ctx, s.subservices)
}

func (s *streamMetaGen) running(ctx context.Context) error {
	// Create error channel to collect errors from goroutines
	errCh := make(chan error, s.cfg.NumTenants)

	// Start a goroutine for each tenant
	for tenantID, streams := range s.streams {
		s.wg.Add(1)
		go func(tenantID string, streams []distributor.KeyedStream) {
			defer s.wg.Done()

			// Create a ticker for rate limiting based on QPSPerTenant
			ticker := time.NewTicker(time.Second / time.Duration(s.cfg.QPSPerTenant))
			defer ticker.Stop()

			// Keep track of current stream index
			streamIdx := 0

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if streamIdx >= len(streams) {
						// Reset index to start over when we've gone through all streams
						streamIdx = 0
					}

					// Send single stream to Kafka
					err := s.sendStreamsToKafka(ctx, streams[streamIdx:streamIdx+1], tenantID)
					if err != nil {
						errCh <- errors.Wrapf(err, "failed to send stream for tenant %s", tenantID)
						return
					}

					streamIdx++
				}
			}
		}(tenantID, streams)
	}

	// Wait for context cancellation, subservice failure, or tenant error
	select {
	case <-ctx.Done():
		return nil
	case err := <-s.subservicesWatcher.Chan():
		return errors.Wrap(err, "streammetagen subservice failed")
	case err := <-errCh:
		return err
	}
}

func (s *streamMetaGen) sendStreamsToKafka(ctx context.Context, streams []distributor.KeyedStream, tenant string) error {
	for _, stream := range streams {
		partitionID, err := s.partitionRing.PartitionRing().ActivePartitionForKey(stream.RingToken)
		if err != nil {
			return fmt.Errorf("failed to find active partition for stream: %w", err)
		}

		// Add metadata record
		metadataRecord := kafka.EncodeStreamMetadata(partitionID, s.cfg.Kafka.Topic, tenant, stream.HashNoShard)

		// Send to Kafka
		produceResults := s.writer.ProduceSync(ctx, []*kgo.Record{metadataRecord})

		// Check for errors
		for _, result := range produceResults {
			if result.Err != nil {
				return fmt.Errorf("failed to write stream metadata to kafka: %w", result.Err)
			}
		}
	}

	return nil
}

func (s *streamMetaGen) stopping(_ error) error {
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
	producer := client.NewProducer(kafkaClient, 100*1024*1024, reg)
	return producer, nil
}

func generateStreamsForTenant(tenantID string, streamsPerTenant int) []distributor.KeyedStream {
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

	// Add HTTP server flags
	var httpListenAddr string
	flag.StringVar(&httpListenAddr, "http.listen-addr", ":9090", "HTTP server listen address for metrics")

	cfg := Config{}
	cfg.RegisterFlags(flag.CommandLine, logger)
	flag.Parse()

	logger = level.NewFilter(logger, cfg.LogLevel.Option)

	if err := util.PrintConfig(os.Stdout, &cfg); err != nil {
		logger.Log("msg", "Error printing config", "err", err)
		os.Exit(1)
	}

	// Create a new registry for Kafka metrics
	promReg := prometheus.NewRegistry()
	reg := prometheus.WrapRegistererWithPrefix(metricsNamespace, promReg)

	// Create the Kafka writer
	writer, err := newKafkaWriter(cfg.Kafka, logger, reg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Kafka writer: %v\n", err)
		os.Exit(1)
	}
	defer writer.Close()

	// Create and start the stream metadata generator service
	gen, err := newStreamMetaGen(cfg, writer, logger, reg)
	if err != nil {
		logger.Log("msg", "Error creating stream metadata generator", "err", err)
		os.Exit(1)
	}

	// Start the service and wait for it to be ready
	if err := services.StartAndAwaitRunning(context.Background(), gen); err != nil {
		logger.Log("msg", "Error starting stream metadata generator", "err", err)
		os.Exit(1)
	}

	// Create HTTP server for metrics
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	server := &http.Server{
		Addr:    httpListenAddr,
		Handler: mux,
	}

	// Start HTTP server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Log("msg", "Error starting metrics server", "err", err)
			os.Exit(1)
		}
	}()

	logger.Log("msg", "Started metrics server", "addr", httpListenAddr)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	// Gracefully shutdown HTTP server
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Log("msg", "Error shutting down metrics server", "err", err)
	}

	// Stop the service gracefully
	if err := services.StopAndAwaitTerminated(context.Background(), gen); err != nil {
		logger.Log("msg", "Error stopping stream metadata generator", "err", err)
		os.Exit(1)
	}
}
