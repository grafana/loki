package distributor

import (
	"context"
	"flag"
	"strconv"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/util"

	"github.com/weaveworks/common/instrument"

	"github.com/grafana/dskit/services"

	"github.com/go-kit/log/level"

	util_log "github.com/grafana/loki/pkg/util/log"

	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logproto"
)

const keySeparator = ":"

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
	fs.DurationVar(&cfg.IngesterReqTimeout, prefix+".ingester-request-timeout", 500*time.Millisecond, "Timeout for communication between distributors and any given ingester when updating rates")
}

type ingesterClient struct {
	addr   string
	client logproto.StreamDataClient
}

type rateStore struct {
	services.Service

	ring            ring.ReadRing
	clientPool      poolClientFactory
	rates           map[string]int64
	rateLock        sync.RWMutex
	ingesterTimeout time.Duration
	maxParallelism  int
	limits          Limits

	metrics *ratestoreMetrics
}

func NewRateStore(cfg RateStoreConfig, r ring.ReadRing, cf poolClientFactory, l Limits, registerer prometheus.Registerer) *rateStore { //nolint
	s := &rateStore{
		ring:            r,
		clientPool:      cf,
		maxParallelism:  cfg.MaxParallelism,
		ingesterTimeout: cfg.IngesterReqTimeout,
		limits:          l,
		metrics:         newRateStoreMetrics(registerer),
	}

	rateCollectionInterval := util.DurationWithJitter(cfg.StreamRateUpdateInterval, 0.2)
	s.Service = services.
		NewTimerService(rateCollectionInterval, s.instrumentedUpdateAllRates, s.instrumentedUpdateAllRates, nil).
		WithName("rate store")

	return s
}

func (s *rateStore) instrumentedUpdateAllRates(ctx context.Context) error {
	if !s.anyShardingEnabled() {
		return nil
	}

	return instrument.CollectedRequest(ctx, "GetAllStreamRates", s.metrics.refreshDuration, instrument.ErrorCode, s.updateAllRates)
}

func (s *rateStore) updateAllRates(ctx context.Context) error {
	clients, err := s.getClients()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting ingester clients", "err", err)
		s.metrics.rateRefreshFailures.WithLabelValues("ring").Inc()
		return nil // Don't fail the service because we have an error getting the clients once
	}

	streamRates := s.getRates(ctx, clients)
	rates := s.aggregateByShard(streamRates)

	s.metrics.streamCount.Set(float64(len(rates)))

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

func (s *rateStore) aggregateByShard(streamRates map[string]*logproto.StreamRate) map[string]int64 {
	var maxRate int64
	shardCount := make(map[string]int)
	rates := make(map[string]int64)

	for _, sr := range streamRates {
		key := key(sr.Tenant, sr.StreamHashNoShard)
		shardCount[key]++

		if _, ok := rates[key]; ok {
			rates[key] += sr.Rate
			maxRate = max(rates[key], maxRate)

			continue
		}

		rates[key] = sr.Rate
		maxRate = max(rates[key], maxRate)
	}

	var maxShards int64
	for _, v := range shardCount {
		maxShards = max(maxShards, int64(v))
	}

	s.metrics.maxStreamRate.Set(float64(maxRate))
	s.metrics.maxStreamShardCount.Set(float64(maxShards))
	return rates
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (s *rateStore) getRates(ctx context.Context, clients []ingesterClient) map[string]*logproto.StreamRate {
	parallelClients := make(chan ingesterClient, len(clients))
	responses := make(chan *logproto.StreamRatesResponse, len(clients))

	for i := 0; i < s.maxParallelism; i++ {
		go s.getRatesFromIngesters(ctx, parallelClients, responses)
	}

	for _, c := range clients {
		parallelClients <- c
	}
	close(parallelClients)

	return s.ratesPerStream(responses, len(clients))
}

func (s *rateStore) getRatesFromIngesters(ctx context.Context, clients chan ingesterClient, responses chan *logproto.StreamRatesResponse) {
	for c := range clients {
		ctx, cancel := context.WithTimeout(ctx, s.ingesterTimeout)

		resp, err := c.client.GetStreamRates(ctx, &logproto.StreamRatesRequest{})
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "unable to get stream rates", "err", err)
			s.metrics.rateRefreshFailures.WithLabelValues(c.addr).Inc()
		}

		responses <- resp
		cancel()
	}
}

func (s *rateStore) ratesPerStream(responses chan *logproto.StreamRatesResponse, totalResponses int) map[string]*logproto.StreamRate {
	var maxRate int64
	streamRates := make(map[string]*logproto.StreamRate)
	for i := 0; i < totalResponses; i++ {
		resp := <-responses
		if resp == nil {
			continue
		}

		for j := 0; j < len(resp.StreamRates); j++ {
			rate := resp.StreamRates[j]
			key := key(rate.Tenant, rate.StreamHash)

			maxRate = max(maxRate, rate.Rate)

			if r, ok := streamRates[key]; ok {
				if r.Rate < rate.Rate {
					streamRates[key] = rate
				}
				continue
			}

			streamRates[key] = rate
		}
	}

	s.metrics.maxUniqueStreamRate.Set(float64(maxRate))
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

func (s *rateStore) RateFor(tenant string, streamHash uint64) int64 {
	s.rateLock.RLock()
	defer s.rateLock.RUnlock()

	return s.rates[key(tenant, streamHash)]
}

func key(tenant string, hash uint64) string {
	return tenant + keySeparator + strconv.FormatUint(hash, 10)
}
