package distributor

import (
	"context"
	"errors"
	"io"
	"maps"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/rateservice/proto"
)

type rateStoreV2 struct {
	realms    map[string]map[string]uint64
	realmsMtx sync.RWMutex
}

func newRateStoreV2() *rateStoreV2 {
	return &rateStoreV2{
		realms: make(map[string]map[string]uint64),
	}
}

// GetRate returns the rate for the realm and name. It returns false if the
// realm, or the name, does not exist.
func (r *rateStoreV2) GetRate(realm, name string) (uint64, bool) {
	r.realmsMtx.RLock()
	defer r.realmsMtx.RUnlock()
	realmRates, ok := r.realms[realm]
	if !ok {
		return 0, false
	}
	v, ok := realmRates[name]
	return v, ok
}

// GetRates returns all rates for the realm. It returns false if the realm
// does not exist.
func (r *rateStoreV2) GetRates(realm string) (map[string]uint64, bool) {
	r.realmsMtx.RLock()
	defer r.realmsMtx.RUnlock()
	realmRates, ok := r.realms[realm]
	if !ok {
		return nil, false
	}
	return maps.Clone(realmRates), true
}

// UpdateRates replaces the rates for the realm with rates.
func (r *rateStoreV2) UpdateRates(realm string, rates map[string]uint64) {
	r.realmsMtx.Lock()
	defer r.realmsMtx.Unlock()
	r.realms[realm] = rates
}

// A rateBatcherV2 batches rates to be sent to the rate service.
type rateBatcherV2 struct {
	*services.BasicService
	client proto.RateServiceClient

	// realms contains the batched updates for each realm.
	realms    map[string]realmBatchV2
	realmsMtx sync.Mutex

	logger log.Logger

	flushes        prometheus.Counter
	requests       prometheus.Counter
	requestsFailed prometheus.Counter
}

// A realmBatchV2 contains the batches for all rates in a realm.
type realmBatchV2 map[string]rateBatchV2

// A rateBatch contains the per-second batches for a rate.
type rateBatchV2 map[uint64]uint64

func newRateBatcherV2(
	client proto.RateServiceClient,
	interval time.Duration,
	logger log.Logger,
	reg prometheus.Registerer,
) *rateBatcherV2 {
	s := &rateBatcherV2{
		client: client,
		realms: make(map[string]realmBatchV2),
		logger: logger,
		flushes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_rate_service_batcher_flushes_total",
			Help: "The total number of flushes.",
		}),
		requests: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_rate_service_batcher_requests_total",
			Help: "The total number of requests to the rate service.",
		}),
		requestsFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_rate_service_batcher_requests_failed_total",
			Help: "The total number of failed requests to the rate service.",
		}),
	}
	s.BasicService = services.NewTimerService(interval, s.starting, s.oneIter, s.stopping)
	return s
}

// Add adds the realm and name to the next batch of updates.
func (r *rateBatcherV2) Add(realm, name string, val uint64, nowSecs uint64) {
	r.realmsMtx.Lock()
	defer r.realmsMtx.Unlock()
	realmBatch, ok := r.realms[realm]
	if !ok {
		realmBatch = make(realmBatchV2)
		r.realms[realm] = realmBatch
	}
	nameBatch, ok := realmBatch[name]
	if !ok {
		nameBatch = make(rateBatchV2)
		realmBatch[name] = nameBatch
	}
	nameBatch[nowSecs] += val
}

// Flush flushes the next batch of rate updates to the rates service.
func (r *rateBatcherV2) Flush(ctx context.Context) error {
	r.flushes.Inc()
	// Get the next batch that needs to be flushed.
	r.realmsMtx.Lock()
	if len(r.realms) == 0 {
		r.realmsMtx.Unlock()
		return nil
	}
	realms := r.realms
	r.realms = make(map[string]realmBatchV2)
	r.realmsMtx.Unlock()
	// Build a request for each realm, and send it off to the rate service.
	for realm, rates := range realms {
		params := make([]*proto.UpdateRealmParam, 0, len(rates))
		for name, secs := range rates {
			param := &proto.UpdateRealmParam{Name: name}
			for ts, value := range secs {
				param.Values = append(param.Values, &proto.UpdateRateParam{
					Ts:    ts,
					Value: value,
				})
			}
			params = append(params, param)
		}
		r.requests.Inc()
		_, err := r.client.UpdateRealm(ctx, &proto.UpdateRealmRequest{
			Realm:  realm,
			Params: params,
		})
		if err != nil {
			r.requestsFailed.Inc()
			level.Warn(r.logger).Log("msg", "failed to update rates", "err", err)
		}
	}
	return nil
}

// starting implements [services.StartingFn].
func (r *rateBatcherV2) starting(ctx context.Context) error {
	return nil
}

// oneIter implements [services.OneIteration].
func (r *rateBatcherV2) oneIter(ctx context.Context) error {
	return r.Flush(ctx)
}

// stopping implements [services.StoppingFn].
func (r *rateBatcherV2) stopping(failureCase error) error {
	return nil
}

// A rateFetcherV2 fetches rates from the rate service.
type rateFetcherV2 struct {
	services.BasicService
	client        proto.RateServiceClient
	store         *rateStoreV2
	logger        log.Logger
	fetches       prometheus.Counter
	fetchesFailed prometheus.Counter
}

// newRateServiceFetcher returns a new rate service fetcher.
func newRateFetcherV2(
	client proto.RateServiceClient,
	interval time.Duration,
	store *rateStoreV2,
	logger log.Logger,
	reg prometheus.Registerer,
) *rateFetcherV2 {
	f := &rateFetcherV2{
		client: client,
		store:  store,
		logger: logger,
		fetches: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_rate_service_fetcher_fetches_total",
			Help: "The total number of fetches from the rate service.",
		}),
		fetchesFailed: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_rate_service_fetcher_fetches_failed_total",
			Help: "The total number of failed fetches from the rate service.",
		}),
	}
	f.BasicService = *services.NewTimerService(interval, f.starting, f.oneIter, f.stopping)
	return f
}

// Fetch the rates for all realms.
func (r *rateFetcherV2) Fetch(ctx context.Context) error {
	level.Info(r.logger).Log("msg", "fetching rates")
	r.fetches.Inc()
	stream, err := r.client.AllRealms(ctx, &proto.AllRealmsRequest{})
	if err != nil {
		r.fetchesFailed.Inc()
		return err
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				level.Error(r.logger).Log("msg", "unexpected error in stream.Recv()", "err", err)
			}
			break
		}
		level.Info(r.logger).Log("msg", "fetched rates for realm", "realm", resp.Realm)
		rates := make(map[string]uint64, len(resp.Results))
		for _, res := range resp.Results {
			level.Info(r.logger).Log("msg", "rate for name", "name", res.Name, "value", res.Value)
			rates[res.Name] = res.Value
		}
		r.store.UpdateRates(resp.Realm, rates)
	}
	return nil
}

// starting implements [services.StartingFn].
func (r *rateFetcherV2) starting(ctx context.Context) error {
	return nil
}

// oneIter implements [services.OneIteration].
func (r *rateFetcherV2) oneIter(ctx context.Context) error {
	return r.Fetch(ctx)
}

// stopping implements [services.StoppingFn].
func (r *rateFetcherV2) stopping(failureCase error) error {
	return nil
}
