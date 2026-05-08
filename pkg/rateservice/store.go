package rateservice

import (
	"iter"
	"slices"
	"sync"
	"time"

	"github.com/grafana/loki/v3/pkg/rateservice/proto"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	realmsDesc      = prometheus.NewDesc("loki_rate_service_realms", "The current number of realms.", nil, nil)
	realmsRatesDesc = prometheus.NewDesc("loki_rate_service_rates", "The current number of rates per realm.", []string{"realm"}, nil)
)

// A rateStore stores rate buckets for various realms.
type rateStore struct {
	windowSecs, bucketSizeSecs, numBuckets uint64

	realms    map[string]map[string][]rateBucket
	realmsMtx sync.Mutex
}

type rateBucket struct {
	ts, value uint64
}

// newRateStore returns a new rateStore for the window and bucket size.
func newRateStore(windowSecs, bucketSizeSecs uint64) *rateStore {
	return &rateStore{
		windowSecs:     windowSecs,
		bucketSizeSecs: bucketSizeSecs,
		numBuckets:     windowSecs / bucketSizeSecs,
		realms:         make(map[string]map[string][]rateBucket),
	}
}

// AllRealms returns all rates for all realms.
func (s *rateStore) AllRealms() []*proto.AllRealmsResponse {
	s.realmsMtx.Lock()
	defer s.realmsMtx.Unlock()
	results := make([]*proto.AllRealmsResponse, 0, len(s.realms))
	for realm := range s.realms {
		tmp, _ := s.getRealm(realm)
		results = append(results, &proto.AllRealmsResponse{
			Realm:   realm,
			Results: tmp,
		})
	}
	return results
}

// GetRealm returns all rates for the realm.
func (s *rateStore) GetRealm(realm string) ([]*proto.RateResult, bool) {
	s.realmsMtx.Lock()
	defer s.realmsMtx.Unlock()
	return s.getRealm(realm)
}

// UpdateRealm updates the rates for the realm.
func (s *rateStore) UpdateRealm(realm string, params []*proto.UpdateRealmParam) {
	s.realmsMtx.Lock()
	defer s.realmsMtx.Unlock()
	realmRates, ok := s.realms[realm]
	if !ok {
		realmRates = make(map[string][]rateBucket)
	}
	for _, param := range params {
		buckets, ok := realmRates[param.Name]
		if !ok {
			buckets = make([]rateBucket, s.numBuckets)
		}
		s.updateBucket(buckets, param.Values)
		realmRates[param.Name] = buckets
	}
	s.realms[realm] = realmRates
}

// Describe implements [prometheus.Collector].
func (s *rateStore) Describe(descs chan<- *prometheus.Desc) {
	descs <- realmsDesc
	descs <- realmsRatesDesc
}

// Collect implements [prometheus.Collector].
func (s *rateStore) Collect(metrics chan<- prometheus.Metric) {
	var (
		numRealms     int
		ratesPerRealm = make(map[string]int, 128)
	)
	// Count the number of realms, and the number of rates per realm.
	s.realmsMtx.Lock()
	for realm, rates := range s.realms {
		numRealms++
		ratesPerRealm[realm] = len(rates)
	}
	s.realmsMtx.Unlock()
	// Collect the metrics.
	metrics <- prometheus.MustNewConstMetric(
		realmsDesc,
		prometheus.GaugeValue,
		float64(numRealms),
	)
	for realm, num := range ratesPerRealm {
		metrics <- prometheus.MustNewConstMetric(
			realmsRatesDesc,
			prometheus.GaugeValue,
			float64(num),
			realm,
		)
	}
}

func (s *rateStore) getRealm(realm string) ([]*proto.RateResult, bool) {
	realmRates, ok := s.realms[realm]
	if !ok {
		return nil, false
	}
	nowSecs := unixSecs()
	results := make([]*proto.RateResult, 0, len(realmRates))
	for name, buckets := range realmRates {
		results = append(results, &proto.RateResult{
			Name: name,
			Value: reduceBuckets(
				bucketsInWindow(slices.Values(buckets),
					s.windowSecs,
					nowSecs,
				),
			),
		})
	}
	return results, true
}

func (s *rateStore) updateBucket(buckets []rateBucket, params []*proto.UpdateRateParam) {
	for _, val := range params {
		// rate buckets are implemented as a circular list. To update a rate
		// bucket we must first calculate the bucket index.
		bucketNum := val.Ts / s.bucketSizeSecs
		bucketIdx := int(bucketNum % uint64(s.numBuckets))
		bucket := &buckets[bucketIdx]
		// Once we have found the bucket, we then need to check if it is an old
		// bucket outside the window. If it is, we must reset it before we
		// can re-use it.
		startTs := (val.Ts / s.bucketSizeSecs) * s.bucketSizeSecs
		if bucket.ts < startTs {
			bucket.ts = startTs
			bucket.value = 0
		}
		bucket.value += val.Value
	}
}

// reduceBuckets reduces the all buckets into an instant value.
func reduceBuckets(buckets iter.Seq[rateBucket]) uint64 {
	var result, num uint64
	for bucket := range buckets {
		result += bucket.value
		num++
	}
	if num == 0 {
		return 0
	}
	return result / num
}

// bucketsInWindow returns a [seq.Iter] that iterates over buckets within the
// window.
func bucketsInWindow(buckets iter.Seq[rateBucket], windowSecs, nowSecs uint64) iter.Seq[rateBucket] {
	return func(yield func(rateBucket) bool) {
		startTs := nowSecs - windowSecs
		for bucket := range buckets {
			if bucket.ts > startTs && bucket.ts <= nowSecs {
				if !yield(bucket) {
					return
				}
			}
		}
	}
}

// unixSecs returns the current time, in seconds, since the Unix epoch.
func unixSecs() uint64 {
	return uint64(time.Now().Unix())
}
