package generator

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/dns"
	"github.com/grafana/dskit/kv/codec"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/netutil"
	"github.com/grafana/dskit/ring"
	ringclient "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/analytics"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/limits/frontend"
	frontend_client "github.com/grafana/loki/v3/pkg/limits/frontend/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"
	distributor_client "github.com/grafana/loki/v3/tools/stream-generator/distributor/client"
)

type Generator struct {
	services.Service

	cfg     Config
	logger  log.Logger
	metrics *metrics

	// payload
	streams map[string][]distributor.KeyedStream

	// active streams
	activeStreams    []int
	activeStreamsMtx []activeStreamsLock

	// kafka
	writer *client.Producer

	// ring
	memberlistKV       *memberlist.KVInitService
	frontendRing       *ring.Ring
	frontentClientPool *ringclient.Pool

	distributorClient *distributor_client.Client

	// service
	subservices        *services.Manager
	subservicesWatcher *services.FailureWatcher

	// Service internals
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}

type activeStreamsLock struct {
	sync.RWMutex
	// Padding to avoid multiple locks being on the same cache line.
	_ [40]byte
}

func New(cfg Config, logger log.Logger, reg prometheus.Registerer) (*Generator, error) {
	s := &Generator{
		cfg:              cfg,
		logger:           logger,
		metrics:          newMetrics(reg),
		activeStreams:    make([]int, cfg.NumTenants),
		activeStreamsMtx: make([]activeStreamsLock, cfg.NumTenants),
	}

	var err error

	cfg.MemberlistKV.Codecs = []codec.Codec{
		ring.GetCodec(),
		analytics.JSONCodec,
		ring.GetPartitionRingCodec(),
	}

	provider := dns.NewProvider(logger, reg, dns.GolangResolverType)
	cfg.MemberlistKV.AdvertiseAddr, err = netutil.GetFirstAddressOf(cfg.LifecyclerConfig.InfNames, logger, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance address: %w", err)
	}

	s.memberlistKV = memberlist.NewKVInitService(&cfg.MemberlistKV, logger, provider, reg)
	cfg.LifecyclerConfig.RingConfig.KVStore.MemberlistKV = s.memberlistKV.GetMemberlistKV

	srvs := []services.Service{
		s.memberlistKV,
	}

	switch cfg.PushMode {
	// Setup services for push mode via the kafka topic
	case PushStreamMetadataOnly:
		s.writer, err = newKafkaWriter(cfg.Kafka, logger, reg)
		if err != nil {
			return nil, fmt.Errorf("error creating Kafka writer: %w", err)
		}

		// Init Frontend Ring
		s.frontendRing, err = ring.New(cfg.LifecyclerConfig.RingConfig, frontend.RingName, frontend.RingKey, logger, reg)
		if err != nil {
			return nil, fmt.Errorf("creating ingest limits frontend ring: %w", err)
		}

		factory := ringclient.PoolAddrFunc(func(addr string) (ringclient.PoolClient, error) {
			return frontend_client.NewClient(cfg.FrontendClientConfig, addr)
		})

		s.frontentClientPool = frontend_client.NewPool(frontend.RingName, cfg.FrontendClientConfig.PoolConfig, s.frontendRing, factory, logger)
		srvs = append(srvs, s.frontendRing, s.frontentClientPool)

	// Setup services for push mode via distributor
	case PushStream:
		// Do nothing here as per distributor clients are not
		// discovered through the ring.
		s.distributorClient, err = distributor_client.New(cfg.DistributorClientConfig)
		if err != nil {
			return nil, fmt.Errorf("error creating distributor client: %w", err)
		}
	}

	// Init services
	s.subservices, err = services.NewManager(srvs...)
	if err != nil {
		return nil, fmt.Errorf("creating subservices: %w", err)
	}

	s.subservicesWatcher = services.NewFailureWatcher()
	s.subservicesWatcher.WatchManager(s.subservices)

	s.Service = services.NewBasicService(s.starting, s.running, s.stopping)
	return s, nil
}

func (s *Generator) starting(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)

	// Calculate optimal QPS to match the desired rate
	s.cfg.QPSPerTenant = calculateOptimalQPS(s.cfg.DesiredRate, s.cfg.CreateBatchSize, s.logger)
	level.Info(s.logger).Log("msg", fmt.Sprintf("Adjusted QPS per tenant to %d to match desired rate of %d bytes/s",
		s.cfg.QPSPerTenant, s.cfg.DesiredRate))

	// Generate streams for each tenant
	s.streams = make(map[string][]distributor.KeyedStream)
	for i := range s.cfg.NumTenants {
		tenantID := fmt.Sprintf("tenant-%d", i)

		if s.cfg.TenantPrefix != "" {
			tenantID = fmt.Sprintf("%s-%d", s.cfg.TenantPrefix, i)
		}

		s.streams[tenantID] = generateStreamsForTenant(tenantID, s.cfg.StreamsPerTenant, s.cfg.StreamLabels)
	}

	return services.StartManagerAndAwaitHealthy(s.ctx, s.subservices)
}

func (s *Generator) running(ctx context.Context) error {
	// Create error channel to collect errors from goroutines
	errCh := make(chan error, s.cfg.NumTenants)

	// Start goroutines for each tenant:
	// - create: creates new streams in intervals
	// - keepAlive: keeps existing streams alive by re-sending them to the backend
	lockIdx := 0
	for tenant, streams := range s.streams {
		go s.create(ctx, tenant, lockIdx, streams, errCh)
		go s.keepAlive(ctx, tenant, lockIdx, streams, errCh)
		lockIdx++
	}

	// Wait for context cancellation, subservice failure, or tenant error
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-s.subservicesWatcher.Chan():
			return errors.Wrap(err, "stream-generator subservice failed")
		case err := <-errCh:
			level.Error(s.logger).Log("msg", "stream-generator error", "err", err)
		}
	}
}

func (s *Generator) stopping(_ error) error {
	s.cancel()
	s.wg.Wait()

	if s.writer != nil {
		s.writer.Close()
	}

	if s.distributorClient != nil {
		s.distributorClient.Close()
	}

	return services.StopManagerAndAwaitStopped(context.Background(), s.subservices)
}

func (s *Generator) GetMemberlist() *memberlist.KVInitService {
	return s.memberlistKV
}

func (s *Generator) GetFrontendRing() *ring.Ring {
	return s.frontendRing
}

func (s *Generator) create(ctx context.Context, tenant string, lockIdx int, streams []distributor.KeyedStream, errCh chan<- error) {
	s.wg.Add(1)
	defer s.wg.Done()

	createT := time.NewTicker(s.cfg.CreateNewStreamsInterval)
	total := len(streams)

	for {
		select {
		case <-ctx.Done():
			return
		case <-createT.C:
			func() {
				s.activeStreamsMtx[lockIdx].Lock()
				defer s.activeStreamsMtx[lockIdx].Unlock()

				activeStreams := s.activeStreams[lockIdx]

				if activeStreams >= total {
					createT.Stop()
					return
				}

				batchSize := s.cfg.CreateBatchSize
				if activeStreams+batchSize > total {
					batchSize = total - activeStreams
				}

				batch := streams[activeStreams : activeStreams+batchSize]
				s.pushStreams(ctx, tenant, batch, errCh)

				s.metrics.streamsCreatedTotal.WithLabelValues(tenant).Inc()
				s.activeStreams[lockIdx] += batchSize
			}()
		}
	}
}

func (s *Generator) keepAlive(ctx context.Context, tenant string, lockIdx int, streams []distributor.KeyedStream, errCh chan<- error) {
	s.wg.Add(1)
	defer s.wg.Done()

	// Create a aliveT to create new streams in intervals
	aliveT := time.NewTicker(time.Second / time.Duration(s.cfg.QPSPerTenant))
	defer aliveT.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-aliveT.C:
			func() {
				s.activeStreamsMtx[lockIdx].RLock()
				defer s.activeStreamsMtx[lockIdx].RUnlock()

				if s.activeStreams[lockIdx] < s.cfg.CreateBatchSize {
					// Skip until first batch is created
					return
				}

				batch := streams[:s.activeStreams[lockIdx]-1]
				s.pushStreams(ctx, tenant, batch, errCh)
				s.metrics.streamsKeepAliveTotal.WithLabelValues(tenant).Inc()
			}()
		}
	}
}

func (s *Generator) pushStreams(ctx context.Context, tenant string, streams []distributor.KeyedStream, errCh chan<- error) {
	switch s.cfg.PushMode {
	case PushStreamMetadataOnly:
		s.sendStreamMetadata(ctx, tenant, streams, errCh)
	case PushStream:
		s.sendStreams(ctx, tenant, streams, errCh)
	}
}

// calculateOptimalQPS calculates the optimal QPS to achieve the desired ingestion rate
func calculateOptimalQPS(desiredRate, batchSize int, logger log.Logger) int {
	// Calculate bytes per stream for normal streams
	normalStreamRate := normalLogSize * entriesPerStream

	// First, calculate QPS assuming all normal streams
	var optimalQPS int
	if batchSize > 0 {
		// Calculate QPS needed if all streams are normal size
		optimalQPS = int(math.Ceil(float64(desiredRate) / (float64(batchSize) * float64(normalStreamRate))))

		// Check if this QPS would exceed the desired rate
		normalRate := optimalQPS * batchSize * normalStreamRate

		for normalRate > desiredRate && optimalQPS > 1 {
			optimalQPS--
			normalRate = optimalQPS * batchSize * normalStreamRate
		}
	}

	// Calculate the expected rate with this QPS
	expectedRate := optimalQPS * batchSize * normalStreamRate

	level.Info(logger).Log("msg", "Calculated optimal QPS", "optimalQPS", optimalQPS, "desiredRate", desiredRate, "expectedRate", expectedRate)

	return optimalQPS
}

func generateStreamsForTenant(tenantID string, streamsPerTenant int, streamLabels []string) []distributor.KeyedStream {
	streams := make([]distributor.KeyedStream, streamsPerTenant)

	for i := range streamsPerTenant {
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

		// Create the stream with multiple entries
		stream := logproto.Stream{
			Labels:  labelsStr,
			Hash:    lbs.Hash(),
			Entries: make([]logproto.Entry, 0, entriesPerStream),
		}

		// Generate entries for this stream
		for j := range entriesPerStream {
			// Generate log line with the specified size
			logLine := generateLogLine(i, j, normalLogSize)

			// Add entry to stream
			stream.Entries = append(stream.Entries, logproto.Entry{
				Timestamp: time.Now(),
				Line:      logLine,
			})
		}

		// Create the keyed stream
		streams[i] = distributor.KeyedStream{
			HashKey:        lokiring.TokenFor(tenantID, labelsStr),
			HashKeyNoShard: stream.Hash,
			Stream:         stream,
		}
	}

	return streams
}

// generateLogLine creates a log line of approximately the specified size
func generateLogLine(streamIdx, entryIdx, size int) string {
	base := fmt.Sprintf("stream-%d entry-%d ", streamIdx, entryIdx)
	if len(base) >= size {
		return base[:size]
	}

	// Pad with repeating characters to reach desired size
	padding := strings.Repeat("x", size-len(base))
	return base + padding
}
