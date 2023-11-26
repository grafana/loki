package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NOTE: Goal here is to generate keys for range queries (with and without time to snap)
// and keys for instant query (with our proposal)
// experiement to check how keys are getting reused for different queries that share similar start time, step, internval etc.

type mockLimits struct {
	split time.Duration
}

type rangeQuery struct {
	q     string
	start time.Time
	end   time.Time
	step  int64 // in milliseconds
}

type instantQuery struct {
	q  string
	ts time.Time
}

var now = time.Now()
var userID = "fake"

var (
	instantQueries = []instantQuery{
		{
			q:  `sum(bytes_rate({foo="bar"}[1h]))`,
			ts: now,
		},
	}

	rangeQueries = []rangeQuery{
		{
			q:     `sum(bytes_rate({foo="bar"}[5m]))`,
			start: now.Add(-1 * time.Hour),
			end:   now.Add(1 * time.Hour),
			step:  (30 * time.Minute).Milliseconds(),
		},
	}
)

var limits = map[string]mockLimits{
	"1h":  {split: 1 * time.Hour},
	"15m": {split: 15 * time.Minute},
	"5m":  {split: 5 * time.Minute},
}

func main() {
	listener, err := net.Listen("tcp", "127.0.0.1:1112")
	if err != nil {
		panic("failed to listen on local addrs")
	}

	svr := &http.Server{
		Handler: promhttp.Handler(),
	}

	go func() {
		analyseInstant(instantQueries)

		time.Sleep(60 * time.Second) // wait for the final prometheus scrape.

		defer listener.Close() // shutdown http server
	}()

	fmt.Println("serving http on 127.0.0.1:1112")
	log.Fatal(svr.Serve(listener))

	// analyseRange(rangeQueries)

	// sub1 := split_by_range(instantQueries[0], limits["5m"], true)
	// sub2 := split_by_range(instantQueries[0], limits["5m"], false)
	// for _, s := range sub1 {
	// 	fmt.Println(s.q, s.ts)
	// }
	// fmt.Println("================")
	// for _, s := range sub2 {
	// 	fmt.Println(s.q, s.ts)
	// }

}

func analyseInstant(queries []instantQuery) {
	for lk, limit := range limits {
		cache := newCache("instant_query", lk, prometheus.DefaultRegisterer)
		for _, q := range queries {
			keys := make([]string, 0)
			subs := split_by_range(q, limit, true)
			for _, s := range subs {
				key := genInstantQueryKey(&limit, userID, &s)
				keys = append(keys, key)
			}
			_, err := cache.Get(keys)
			if err != nil {
				panic("Failed to fetch the keys")
			}
		}

	}
}

func analyseRange(queries []rangeQuery) {
	for _, limit := range limits {
		for _, q := range queries {
			split_by_interval(q, limit)
		}
	}
}

// used by range query
func split_by_interval(r rangeQuery, limit mockLimits) []rangeQuery {
	interval := limit.split

	res := make([]rangeQuery, 0)

	util.ForInterval(interval, r.start, r.end, false, func(start, end time.Time) {
		res = append(res, rangeQuery{
			q:     r.q,
			start: start,
			end:   end,
			step:  r.step,
		})
	})

	return res
}

// used by instant query
// TODO: missing 1 sub queries while walking through AST of ConcatSampleExpr.
func split_by_range(r instantQuery, limit mockLimits, offset bool) []instantQuery {
	interval := limit.split
	mapperStats := logql.NewMapperStats()
	mapperMetrics := logql.NewShardMapperMetrics(nil)
	mapper, err := logql.NewRangeMapper(interval, mapperMetrics, mapperStats)
	if err != nil {
		panic("failed to create range mapper")
	}
	noop, parsed, err := mapper.Parse(syntax.MustParseExpr(r.q))
	if err != nil {
		panic(fmt.Errorf("failed to parse the query in range mapper: %w", err))
	}
	if noop {
		// add a metric to it.
		fmt.Println("didn't split noop", "query", r.q, "split", interval)
	}

	subQueries := make([]instantQuery, 0)

	parsed.Walk(func(e syntax.Expr) {
		switch concat := e.(type) {
		case *logql.ConcatSampleExpr:
			// This traverse only nested `ConcatSampleExpr`.
			for concat != nil {
				subQueries = append(subQueries, instantQuery{
					q:  concat.DownstreamSampleExpr.SampleExpr.String(),
					ts: r.ts, // It uses same exec timestamp from original query. Option A from the proposal
				})
				concat = concat.Next()
			}
		}
	})

	if !offset {
		// 1. remove offset
		// 2. adjust exec `ts`

		// NOTE: Assumption: Original start is always greater than offset.
		for i, sub := range subQueries {
			parsed, err := syntax.ParseSampleExpr(sub.q)
			if err != nil {
				panic("unable to parse the subquery")
			}
			parsed.Walk(func(e syntax.Expr) {
				switch rng := e.(type) {
				case *syntax.RangeAggregationExpr:
					off := rng.Left.Offset
					rng.Left.Offset = 0       // remove offset
					sub.ts = sub.ts.Add(-off) // adjust exec `ts` of the sub query
				}
			})
			sub.q = parsed.String()

			subQueries[i] = sub
		}
	}

	return subQueries
}

// step align given range query.
// Taken from `step_align.go`
func step_align(r rangeQuery) rangeQuery {
	start := (r.start.UnixMilli() / r.step) * r.step
	end := (r.end.UnixMilli() / r.step) * r.step
	r.start = time.UnixMilli(start)
	r.end = time.UnixMilli(end)
	return r
}

// snap time aligns the start and end time of the sub query in Range query (can be done for instant query as well?)
func snap_time(q rangeQuery) rangeQuery {
	return q
}

type mockStore struct {
	constData float64
}

func (m *mockStore) Get() float64 {
	return m.constData
}

type cache struct {
	name                            string
	storedKeys, requestedKeys, hits prometheus.Counter

	// protects `data`
	mu   sync.Mutex
	data map[string]any

	store *mockStore
}

type cacheReq struct {
	key string
	val any
}

func newCache(name string, split string, reg prometheus.Registerer) *cache {
	namespace := "experiment"

	return &cache{
		name: name,
		data: make(map[string]any),
		storedKeys: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        "cache_stored_keys_total",
			Help:        "Total count of keys stored by the cache",
			ConstLabels: prometheus.Labels{"name": name, "split": split},
		}),
		requestedKeys: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        "cache_requested_keys_total",
			Help:        "Total count of keys requested from the cache",
			ConstLabels: prometheus.Labels{"name": name, "split": split},
		}),
		hits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   namespace,
			Name:        "cache_hits_keys_total",
			Help:        "Total count of keys actually fetched from the cache",
			ConstLabels: prometheus.Labels{"name": name, "split": split},
		}),
		store: &mockStore{constData: 10},
	}

}

// func (c *cache) Put(reqs []cacheReq) error {
// 	c.storedKeys.Add(float64(len((reqs))))
// 	for _, r := range reqs {
// 		c.data[r.key] = r.val
// 	}

// 	return nil
// }

func (c *cache) Get(keys []string) ([]cacheReq, error) {
	var (
		hits = 0
		res  = make([]cacheReq, 0)
	)

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, k := range keys {
		v, ok := c.data[k]
		if ok {
			hits++
			res = append(res, cacheReq{key: k, val: v})
		} else {
			v := c.store.Get() // get from store
			c.data[k] = v      // store it in cache. NO batching for now.
			c.storedKeys.Inc()
		}
	}
	c.hits.Add(float64((hits)))
	c.requestedKeys.Add(float64(len(keys)))
	return res, nil
}

// Copied from `queryrange/limits.go` cacheKeyLimits.Generatecachekey
func genRangeQueryKey(l *mockLimits, userID string, r *rangeQuery) string {
	split := l.split

	var currentInterval int64
	if denominator := int64(split / time.Millisecond); denominator > 0 {
		currentInterval = r.start.UnixMilli() / denominator
	}

	// if l.transformer != nil {
	// 	userID = l.transformer(ctx, userID)
	// }

	// include both the currentInterval and the split duration in key to ensure
	// a cache key can't be reused when an interval changes
	return fmt.Sprintf("%s:%s:%d:%d:%d", userID, r.q, r.step, currentInterval, split)
}

func genInstantQueryKey(l *mockLimits, userID string, r *instantQuery) string {
	split := l.split

	var currentInterval int64
	if denominator := int64(split / time.Millisecond); denominator > 0 {
		currentInterval = r.ts.UnixMilli() / denominator
	}

	return fmt.Sprintf("%s:%s:%d:%d", userID, r.q, currentInterval, split)
}
