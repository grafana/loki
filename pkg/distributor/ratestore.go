package distributor

import (
	"context"
	"flag"
	"sync"
	"time"

	"github.com/weaveworks/common/instrument"

	"github.com/grafana/dskit/services"

	"github.com/go-kit/log/level"

	util_log "github.com/grafana/loki/pkg/util/log"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/ring/client"

	"github.com/grafana/loki/pkg/logproto"
)

type poolClientFactory interface {
	GetClientFor(addr string) (client.PoolClient, error)
}

type RateStoreConfig struct {
	MaxParallelism           int           `yaml:"max_request_parallelism"`
	StreamRateUpdateInterval time.Duration `yaml:"stream_rate_update_interval"`
	IngesterReqTimeout       time.Duration `yaml:"ingester_request_timeout"`
}

func (cfg *RateStoreConfig) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.IntVar(&cfg.MaxParallelism, prefix+".max-request-parallelism", 200, "The max number of concurrent requests to make to ingester stream apis")
	fs.DurationVar(&cfg.StreamRateUpdateInterval, prefix+".stream-rate-update-interval", time.Second, "The interval on which distributors will update current stream rates from ingesters")
	fs.DurationVar(&cfg.IngesterReqTimeout, prefix+".ingester-request-timeout", time.Second, "Timeout for communication between distributors and ingesters when updating rates")
}

type ingesterClient struct {
	addr   string
	client logproto.StreamDataClient
}

type rateStore struct {
	services.Service

	ring                   ring.ReadRing
	clientPool             poolClientFactory
	rates                  map[uint64]int64
	rateLock               sync.RWMutex
	rateCollectionInterval time.Duration
	ingesterTimeout        time.Duration
	maxParallelism         int
	rateRefreshFailures    *prometheus.CounterVec
	refreshDuration        *instrument.HistogramCollector
	limits                 Limits
}

func NewRateStore(cfg RateStoreConfig, r ring.ReadRing, cf poolClientFactory, l Limits, registerer prometheus.Registerer) *rateStore { //nolint
	s := &rateStore{
		ring:                   r,
		clientPool:             cf,
		rateCollectionInterval: cfg.StreamRateUpdateInterval,
		maxParallelism:         cfg.MaxParallelism,
		ingesterTimeout:        cfg.IngesterReqTimeout,
		limits:                 l,
		rateRefreshFailures: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "rate_store_refresh_failures_total",
			Help:      "The total number of failed attempts to refresh the distributor's view of stream rates",
		}, []string{"source"}),
		refreshDuration: instrument.NewHistogramCollector(
			promauto.With(registerer).NewHistogramVec(
				prometheus.HistogramOpts{
					Namespace: "loki",
					Name:      "rate_store_refresh_duration_seconds",
					Help:      "Time spent refreshing the rate store",
					Buckets:   prometheus.DefBuckets,
				}, instrument.HistogramCollectorBuckets,
			),
		),
	}

	s.Service = services.
		NewTimerService(s.rateCollectionInterval, s.instrumentedUpdateAllRates, s.instrumentedUpdateAllRates, nil).
		WithName("rate store")

	return s
}

func (s *rateStore) instrumentedUpdateAllRates(ctx context.Context) error {
	if !s.anyShardingEnabled() {
		return nil
	}

	return instrument.CollectedRequest(ctx, "GetAllStreamRates", s.refreshDuration, instrument.ErrorCode, s.updateAllRates)
}

func (s *rateStore) updateAllRates(ctx context.Context) error {
	clients, err := s.getClients()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting ingester clients", "err", err)
		s.rateRefreshFailures.WithLabelValues("ring").Inc()
		return nil // Don't fail the service because we have an error getting the clients once
	}

	streamRates := s.getRates(ctx, clients)
	rates := s.aggregateByShard(streamRates)

	s.rateLock.Lock()
	defer s.rateLock.Unlock()
	s.rates = rates

	return nil
}

func (s *rateStore) anyShardingEnabled() bool {
	limits := s.limits.AllByUserID()
	if limits == nil {
		// There aren't any tenant limits, check the default
		return s.limits.ShardStreams("fake").Enabled
	}

	for user := range limits {
		if s.limits.ShardStreams(user).Enabled {
			return true
		}
	}

	return false
}

func (s *rateStore) aggregateByShard(streamRates map[uint64]*logproto.StreamRate) map[uint64]int64 {
	rates := make(map[uint64]int64)
	for _, sr := range streamRates {
		if _, ok := rates[sr.StreamHashNoShard]; ok {
			rates[sr.StreamHashNoShard] += sr.Rate
			continue
		}

		rates[sr.StreamHashNoShard] = sr.Rate
	}
	return rates
}

func (s *rateStore) getRates(ctx context.Context, clients []ingesterClient) map[uint64]*logproto.StreamRate {
	parallelClients := make(chan ingesterClient, len(clients))
	responses := make(chan *logproto.StreamRatesResponse, len(clients))

	for i := 0; i < s.maxParallelism; i++ {
		go s.getRatesFromIngesters(ctx, parallelClients, responses)
	}

	for _, c := range clients {
		parallelClients <- c
	}
	close(parallelClients)

	return ratesPerStream(responses, len(clients))
}

func (s *rateStore) getRatesFromIngesters(ctx context.Context, clients chan ingesterClient, responses chan *logproto.StreamRatesResponse) {
	for c := range clients {
		ctx, cancel := context.WithTimeout(ctx, s.ingesterTimeout)

		resp, err := c.client.GetStreamRates(ctx, &logproto.StreamRatesRequest{})
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "unable to get stream rates", "err", err)
			s.rateRefreshFailures.WithLabelValues(c.addr).Inc()
		}

		responses <- resp
		cancel()
	}
}

func ratesPerStream(responses chan *logproto.StreamRatesResponse, totalResponses int) map[uint64]*logproto.StreamRate {
	streamRates := make(map[uint64]*logproto.StreamRate)
	for i := 0; i < totalResponses; i++ {
		resp := <-responses
		if resp == nil {
			continue
		}

		for j := 0; j < len(resp.StreamRates); j++ {
			rate := resp.StreamRates[j]

			if r, ok := streamRates[rate.StreamHash]; ok {
				if r.Rate < rate.Rate {
					streamRates[rate.StreamHash] = rate
				}
				continue
			}

			streamRates[rate.StreamHash] = rate
		}
	}

	return streamRates
}

func (s *rateStore) getClients() ([]ingesterClient, error) {
	ingesters, err := s.ring.GetAllHealthy(ring.Read)
	if err != nil {
		return nil, err
	}

	clients := make([]ingesterClient, 0, len(ingesters.Instances))
	for _, i := range ingesters.Instances {
		client, err := s.clientPool.GetClientFor(i.Addr)
		if err != nil {
			return nil, err
		}

		clients = append(clients, ingesterClient{i.Addr, client.(logproto.StreamDataClient)})
	}

	return clients, nil
}

func (s *rateStore) RateFor(streamHash uint64) int64 {
	s.rateLock.RLock()
	defer s.rateLock.RUnlock()

	return s.rates[streamHash]
}
