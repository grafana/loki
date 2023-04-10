package distributor

import (
	"context"
	"flag"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/util"
	"github.com/opentracing/opentracing-go"

	"github.com/weaveworks/common/instrument"

	"github.com/grafana/dskit/services"

	"github.com/go-kit/log/level"

	util_log "github.com/grafana/loki/pkg/util/log"

	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logproto"
)

type poolClientFactory interface {
	GetClientFor(addr string) (client.PoolClient, error)
}

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
	if s.debug {
		if sp := opentracing.SpanFromContext(ctx); sp != nil {
			sp.LogKV("event", "started to update rates")
			defer sp.LogKV("event", "finished to update rates")
		}
	}
	s.rateLock.Lock()
	defer s.rateLock.Unlock()

	for tenantID, tenant := range updated {
		if _, ok := s.rates[tenantID]; !ok {
			s.rates[tenantID] = map[uint64]expiringRate{}
		}

		for stream, rate := range tenant {
			s.rates[tenantID][stream] = rate
		}
	}

	return s.cleanupExpired()
}

func (s *rateStore) cleanupExpired() rateStats {
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

			rs.maxRate = max(rs.maxRate, rate.rate)
			rs.maxShards = max(rs.maxShards, rate.shards)

			s.metrics.streamShardCount.Observe(float64(rate.shards))
			s.metrics.streamRate.Observe(float64(rate.rate))
		}
	}

	return rs
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

	for tID, tenant := range streamRates {
		for _, streamRate := range tenant {
			if _, ok := rates[tID]; !ok {
				rates[tID] = map[uint64]expiringRate{}
			}

			rate := rates[tID][streamRate.StreamHashNoShard]
			rate.rate += streamRate.Rate
			rate.shards++
			rate.createdAt = time.Now()

			rates[tID][streamRate.StreamHashNoShard] = rate
		}
	}

	return rates
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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

func (s *rateStore) RateFor(tenant string, streamHash uint64) int64 {
	s.rateLock.RLock()
	defer s.rateLock.RUnlock()

	if t, ok := s.rates[tenant]; ok {
		return t[streamHash].rate
	}
	return 0
}
