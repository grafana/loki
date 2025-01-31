package distributor

import (
	"context"
	"flag"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type poolClientFactory interface {
	GetClientFor(addr string) (client.PoolClient, error)
}

const (
	// The factor used to weight the moving average. Must be in the range [0, 1.0].
	// A larger factor weights recent samples more heavily while a smaller
	// factor weights historic samples more heavily.
	smoothingFactor = .4
)

type RateStoreConfig struct {
	MaxParallelism           int           `yaml:"max_request_parallelism"`
	StreamRateUpdateInterval time.Duration `yaml:"stream_rate_update_interval"`
	IngesterReqTimeout       time.Duration `yaml:"ingester_request_timeout"`
	Debug                    bool          `yaml:"debug"`
}

func (cfg *RateStoreConfig) RegisterFlagsWithPrefix(prefix string, fs *flag.FlagSet) {
	fs.IntVar(&cfg.MaxParallelism, prefix+".max-request-parallelism", 200, "The max number of concurrent requests to make to ingester stream apis")
	fs.DurationVar(&cfg.StreamRateUpdateInterval, prefix+".stream-rate-update-interval", time.Second, "The interval on which distributors will update current stream rates from ingesters")
	fs.DurationVar(&cfg.IngesterReqTimeout, prefix+".ingester-request-timeout", 500*time.Millisecond, "Timeout for communication between distributors and any given ingester when updating rates")
	fs.BoolVar(&cfg.Debug, prefix+".debug", false, "If enabled, detailed logs and spans will be emitted.")
}

type ingesterClient struct {
	addr   string
	client logproto.StreamDataClient
}

type expiringRate struct {
	createdAt time.Time
	rate      int64
	shards    int64
	pushes    float64
}

type rateStore struct {
	services.Service

	ring            ring.ReadRing
	clientPool      poolClientFactory
	rates           map[string]map[uint64]expiringRate // tenant id -> fingerprint -> rate
	rateLock        sync.RWMutex
	rateKeepAlive   time.Duration
	ingesterTimeout time.Duration
	maxParallelism  int
	limits          Limits

	metrics *ratestoreMetrics

	debug bool
}

func NewRateStore(cfg RateStoreConfig, r ring.ReadRing, cf poolClientFactory, l Limits, registerer prometheus.Registerer) *rateStore { //nolint
	s := &rateStore{
		ring:            r,
		clientPool:      cf,
		maxParallelism:  cfg.MaxParallelism,
		ingesterTimeout: cfg.IngesterReqTimeout,
		rateKeepAlive:   10 * time.Minute,
		limits:          l,
		metrics:         newRateStoreMetrics(registerer),
		rates:           make(map[string]map[uint64]expiringRate),
		debug:           cfg.Debug,
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
	clients, err := s.getClients(ctx)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error getting ingester clients", "err", err)
		s.metrics.rateRefreshFailures.WithLabelValues("ring").Inc()
		return nil // Don't fail the service because we have an error getting the clients once
	}

	streamRates := s.getRates(ctx, clients)
	updated := s.aggregateByShard(ctx, streamRates)
	updateStats := s.updateRates(ctx, updated)

	s.metrics.maxStreamRate.Set(float64(updateStats.maxRate))
	s.metrics.maxStreamShardCount.Set(float64(updateStats.maxShards))
	s.metrics.streamCount.Set(float64(updateStats.totalStreams))
	s.metrics.expiredCount.Add(float64(updateStats.expiredCount))

	return nil
}

type rateStats struct {
	maxShards    int64
	maxRate      int64
	totalStreams int64
	expiredCount int64
}

func (s *rateStore) updateRates(ctx context.Context, updated map[string]map[uint64]expiringRate) rateStats {
	streamCnt := 0
	if s.debug {
		if sp := opentracing.SpanFromContext(ctx); sp != nil {
			sp.LogKV("event", "started updating rates")
			defer sp.LogKV("event", "finished updating rates", "streams", streamCnt)
		}
	}
	s.rateLock.Lock()
	defer s.rateLock.Unlock()

	for tenantID, tenant := range updated {
		if _, ok := s.rates[tenantID]; !ok {
			s.rates[tenantID] = map[uint64]expiringRate{}
		}

		for stream, rate := range tenant {
			if oldRate, ok := s.rates[tenantID][stream]; ok {
				rate.rate = weightedMovingAverage(rate.rate, oldRate.rate)
				rate.pushes = weightedMovingAverageF(rate.pushes, oldRate.pushes)
			}
			s.rates[tenantID][stream] = rate
			streamCnt++
		}
	}

	return s.cleanupExpired(updated)
}

func weightedMovingAverage(n, l int64) int64 {
	return int64(weightedMovingAverageF(float64(n), float64(l)))
}

func weightedMovingAverageF(next, last float64) float64 {
	// https://en.wikipedia.org/wiki/Moving_average#Exponential_moving_average
	return (smoothingFactor * next) + ((1 - smoothingFactor) * last)
}

func (s *rateStore) cleanupExpired(updated map[string]map[uint64]expiringRate) rateStats {
	var rs rateStats

	for tID, tenant := range s.rates {
		rs.totalStreams += int64(len(tenant))
		for stream, rate := range tenant {
			if time.Since(rate.createdAt) > s.rateKeepAlive {
				rs.expiredCount++
				delete(s.rates[tID], stream)
				if len(s.rates[tID]) == 0 {
					delete(s.rates, tID)
				}
				continue
			}

			if !s.wasUpdated(tID, stream, updated) {
				rate.rate = weightedMovingAverage(0, rate.rate)
				rate.pushes = weightedMovingAverageF(0, rate.pushes)
				s.rates[tID][stream] = rate
			}

			rs.maxRate = max(rs.maxRate, rate.rate)
			rs.maxShards = max(rs.maxShards, rate.shards)

			s.metrics.streamShardCount.Observe(float64(rate.shards))
			s.metrics.streamRate.Observe(float64(rate.rate))
		}
	}

	return rs
}

func (s *rateStore) wasUpdated(tenantID string, streamID uint64, lastUpdated map[string]map[uint64]expiringRate) bool {
	if _, ok := lastUpdated[tenantID]; !ok {
		return false
	}

	if _, ok := lastUpdated[tenantID][streamID]; !ok {
		return false
	}

	return true
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

func (s *rateStore) aggregateByShard(ctx context.Context, streamRates map[string]map[uint64]*logproto.StreamRate) map[string]map[uint64]expiringRate {
	if s.debug {
		if sp := opentracing.SpanFromContext(ctx); sp != nil {
			sp.LogKV("event", "started to aggregate by shard")
			defer sp.LogKV("event", "finished to aggregate by shard")
		}
	}
	rates := map[string]map[uint64]expiringRate{}
	now := time.Now()

	for tID, tenant := range streamRates {
		if _, ok := rates[tID]; !ok {
			rates[tID] = map[uint64]expiringRate{}
		}

		for _, streamRate := range tenant {
			rate := rates[tID][streamRate.StreamHashNoShard]
			rate.rate += streamRate.Rate
			rate.pushes = math.Max(float64(streamRate.Pushes), rate.pushes)
			rate.shards++
			rate.createdAt = now

			rates[tID][streamRate.StreamHashNoShard] = rate
		}
	}

	return rates
}

func (s *rateStore) getRates(ctx context.Context, clients []ingesterClient) map[string]map[uint64]*logproto.StreamRate {
	if s.debug {
		if sp := opentracing.SpanFromContext(ctx); sp != nil {
			sp.LogKV("event", "started to get rates from ingesters")
			defer sp.LogKV("event", "finished to get rates from ingesters")
		}
	}

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
		func() {
			if s.debug {
				startTime := time.Now()
				defer func() {
					level.Debug(util_log.Logger).Log("msg", "get rates from ingester", "duration", time.Since(startTime), "ingester", c.addr)
				}()
			}
			ctx, cancel := context.WithTimeout(ctx, s.ingesterTimeout)

			resp, err := c.client.GetStreamRates(ctx, &logproto.StreamRatesRequest{})
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "unable to get stream rates from ingester", "ingester", c.addr, "err", err)
				s.metrics.rateRefreshFailures.WithLabelValues(c.addr).Inc()
			}

			responses <- resp
			cancel()
		}()
	}
}

func (s *rateStore) ratesPerStream(responses chan *logproto.StreamRatesResponse, totalResponses int) map[string]map[uint64]*logproto.StreamRate {
	var maxRate int64
	streamRates := map[string]map[uint64]*logproto.StreamRate{}
	for i := 0; i < totalResponses; i++ {
		resp := <-responses
		if resp == nil {
			continue
		}

		for _, rate := range resp.StreamRates {
			maxRate = max(maxRate, rate.Rate)

			if _, ok := streamRates[rate.Tenant]; !ok {
				streamRates[rate.Tenant] = map[uint64]*logproto.StreamRate{}
			}

			if r, ok := streamRates[rate.Tenant][rate.StreamHash]; ok {
				if r.Rate < rate.Rate {
					streamRates[rate.Tenant][rate.StreamHash] = rate
				}
				continue
			}

			streamRates[rate.Tenant][rate.StreamHash] = rate
		}
	}

	s.metrics.maxUniqueStreamRate.Set(float64(maxRate))
	return streamRates
}

func (s *rateStore) getClients(ctx context.Context) ([]ingesterClient, error) {
	if s.debug {
		if sp := opentracing.SpanFromContext(ctx); sp != nil {
			sp.LogKV("event", "ratestore started getting clients")
			defer sp.LogKV("event", "ratestore finished getting clients")
		}
	}

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

func (s *rateStore) RateFor(tenant string, streamHash uint64) (int64, float64) {
	s.rateLock.RLock()
	defer s.rateLock.RUnlock()

	if t, ok := s.rates[tenant]; ok {
		rate := t[streamHash]
		return rate.rate, rate.pushes
	}

	return 0, 0
}
