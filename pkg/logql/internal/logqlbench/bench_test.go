package logqlbench

import (
	"context"
	"fmt"
	"io"
	"math"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/storage"
	objectclient "github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type benchQuery struct {
	expr        string
	description string
	instant     bool
	// streamFirstSupported is false if the query stays timestamp-first regardless of the
	// per-tenant limit, e.g. a count_over_time with no outer sum.
	streamFirstSupported bool
}

func (q benchQuery) name() string {
	kind := "range"
	if q.instant {
		kind = "instant"
	}
	return fmt.Sprintf("%s (%s, %s)", q.expr, q.description, kind)
}

// selectorAll matches every stream of the fixture (2000); selectorMedium matches 250;
// selectorLow matches ~40.
var (
	selectorAll    = fmt.Sprintf("{%s=%q}", labelAllName, labelAllValue)
	selectorMedium = fmt.Sprintf("{%s=%q}", labelMediumInputName, padValue("svc-0"))
	selectorLow    = fmt.Sprintf("{%s=%q}", labelSubsetName, labelSubsetValue)
)

// storeParallelism bounds concurrent object-store reads for both backends (see
// dataObjMaxConcurrency and MaxParallelGetChunk in store_chunk_test.go), so neither backend's
// latency sensitivity is an artifact of a mismatched concurrency setting.
const storeParallelism = 100

// dataObjMaxConcurrency is how many logs sections the data-object store scans at once. A fixture
// section is smaller than twice the rangeio minimum range size, so rangeio does not split its
// reads. Each section keeps at most MaxParallelism requests in flight.
const dataObjMaxConcurrency = storeParallelism

// dataObjRangeConfig defaults rangeio.Config and sets MaxParallelism so that
// dataObjMaxConcurrency sections, each reading MaxParallelism ranges at a time, stay within
// storeParallelism requests.
var dataObjRangeConfig = newDataObjRangeConfig()

func newDataObjRangeConfig() rangeio.Config {
	var cfg rangeio.Config
	flagext.DefaultValues(&cfg)
	cfg.MaxParallelism = max(1, storeParallelism/dataObjMaxConcurrency)
	return cfg
}

var queries = []benchQuery{
	{
		expr:                 fmt.Sprintf("sum(count_over_time(%s[5m]))", selectorAll),
		description:          "high input, low output",
		instant:              false,
		streamFirstSupported: true,
	},
	{
		expr:                 fmt.Sprintf("sum(count_over_time(%s[30m]))", selectorAll),
		description:          "high input, low output",
		instant:              false,
		streamFirstSupported: true,
	},
	{
		expr:                 fmt.Sprintf("sum by(%s) (count_over_time(%s[5m]))", labelMediumCardName, selectorAll),
		description:          "high input, medium output",
		instant:              false,
		streamFirstSupported: true,
	},
	{
		expr:                 fmt.Sprintf("sum by(%s) (count_over_time(%s[30m]))", labelMediumCardName, selectorAll),
		description:          "high input, medium output",
		instant:              false,
		streamFirstSupported: true,
	},
	{
		expr:                 fmt.Sprintf("sum(count_over_time(%s[5m]))", selectorLow),
		description:          "low input, low output",
		instant:              false,
		streamFirstSupported: true,
	},
	{
		expr:                 fmt.Sprintf("sum(count_over_time(%s[30m]))", selectorLow),
		description:          "low input, low output",
		instant:              false,
		streamFirstSupported: true,
	},
	{
		expr:                 fmt.Sprintf("sum(count_over_time(%s[24h]))", selectorAll),
		description:          "high input, low output",
		instant:              true,
		streamFirstSupported: true,
	},
	{
		expr:                 fmt.Sprintf("sum by(%s) (count_over_time(%s[24h]))", labelMediumCardName, selectorAll),
		description:          "high input, medium output",
		instant:              true,
		streamFirstSupported: true,
	},
	{
		expr:                 fmt.Sprintf("count_over_time(%s[24h])", selectorMedium),
		description:          "medium input, medium output",
		instant:              true,
		streamFirstSupported: false,
	},
}

func TestQueriesStreamFirstSupport(t *testing.T) {
	for _, query := range queries {
		name := query.name() + " does not support stream-first execution"
		if query.streamFirstSupported {
			name = query.name() + " supports stream-first execution"
		}
		t.Run(name, func(t *testing.T) {
			_, got := logql.StreamFirstRangeAggregation(query.expr)
			require.Equal(t, query.streamFirstSupported, got, "query %q", query.expr)
		})
	}
}

// benchScenario is one way to run a query: a store and a sample order.
type benchScenario struct {
	name       string
	order      logproto.SampleOrder
	getQuerier func() (logql.Querier, error)
}

// There is no dataobj-timestamp-first scenario: it would be identical to chunk-timestamp-first.
// Each store is a lazy getter so building this list never builds one.
func newScenarios(getChunkStore func() (*storage.LokiStore, error), getDataObjStore func() (querier.Store, error)) []benchScenario {
	chunkQuerier := func() (logql.Querier, error) { return getChunkStore() }
	dataObjQuerier := func() (logql.Querier, error) { return getDataObjStore() }

	return []benchScenario{
		{name: "chunk-timestamp-first", order: logproto.SAMPLE_ORDER_BY_TIMESTAMP, getQuerier: chunkQuerier},
		{name: "chunk-stream-first", order: logproto.SAMPLE_ORDER_BY_STREAM, getQuerier: chunkQuerier},
		{name: "dataobj-stream-first", order: logproto.SAMPLE_ORDER_BY_STREAM, getQuerier: dataObjQuerier},
	}
}

var benchLatencies = []struct {
	name     string
	duration time.Duration
}{
	{"0s", 0},
	{"50ms", 50 * time.Millisecond},
	{"250ms", 250 * time.Millisecond},
}

func BenchmarkLogQLMetricQueries(b *testing.B) {
	// Pin to one core so the comparison measures CPU wall time, not how much a scenario benefits
	// from parallelism: a scenario that happens to parallelize more could look faster for that
	// reason alone, even doing the same or more total CPU work.
	runtime.GOMAXPROCS(1)

	dir, err := ensureFixtures(b)
	require.NoError(b, err)

	instrumentation := &instrumentation{}

	getChunkStore := sync.OnceValues(func() (*storage.LokiStore, error) {
		chunkStore, err := newChunkStore(dir, tenant, func(inner objectclient.ObjectClient) objectclient.ObjectClient {
			return newInstrumentedObjectClient(inner, instrumentation)
		})
		if err != nil {
			return nil, err
		}
		b.Cleanup(func() { _ = chunkStore.Close() })
		return chunkStore.store, nil
	})

	getDataObjStore := sync.OnceValues(func() (querier.Store, error) {
		bucket, err := newDataObjBucket(dir, func(inner objstore.Bucket) objstore.Bucket {
			return newInstrumentedBucket(inner, instrumentation)
		})
		if err != nil {
			return nil, err
		}
		b.Cleanup(func() { _ = bucket.Close() })

		reg := prometheus.NewRegistry()
		dataObjMetastore := newDataObjMetastore(bucket, log.NewNopLogger(), metastore.NewObjectMetastoreMetrics(reg))
		return querier.NewDataObjStore(unreachableStore{}, bucket, dataObjMetastore, reg, querier.WithDataObjRangeConfig(dataObjRangeConfig), querier.WithDataObjMaxConcurrency(dataObjMaxConcurrency))
	})

	scenarios := newScenarios(getChunkStore, getDataObjStore)

	runQuery := func(b *testing.B, engine logql.Engine, params logql.Params) {
		ctx := user.InjectOrgID(context.Background(), tenant)
		res, err := engine.Query(params).Exec(ctx)
		require.NoError(b, err)
		require.NotNil(b, res.Data)
	}

	for _, query := range queries {
		start, end, step := fixtureStart, fixtureEnd, 5*time.Minute
		if query.instant {
			start, end, step = fixtureEnd, fixtureEnd, 0
		}
		params, err := logql.NewLiteralParams(query.expr, start, end, step, 0, logproto.FORWARD, 0, nil, nil)
		require.NoError(b, err)

		b.Run("query="+query.name(), func(b *testing.B) {
			for _, scenario := range scenarios {
				if scenario.order == logproto.SAMPLE_ORDER_BY_STREAM && !query.streamFirstSupported {
					continue
				}

				b.Run("scenario="+scenario.name, func(b *testing.B) {
					storeQuerier, err := scenario.getQuerier()
					require.NoError(b, err)

					var engineOpts logql.EngineOpts
					flagext.DefaultValues(&engineOpts)
					engine := logql.NewEngine(engineOpts, storeQuerier, benchLimits{streamFirstEnabled: scenario.order == logproto.SAMPLE_ORDER_BY_STREAM}, log.NewNopLogger())

					for _, latency := range benchLatencies {
						b.Run("latency="+latency.name, func(b *testing.B) {
							// Warmup state, with no injected latency.
							instrumentation.artificialLatencyNs.Store(0)
							runQuery(b, engine, params)

							// Run the benchmark with the injected latency.
							instrumentation.artificialLatencyNs.Store(int64(latency.duration))
							instrumentation.Reset()
							b.ReportAllocs()
							for b.Loop() {
								runQuery(b, engine, params)
							}

							require.Zero(b, instrumentation.inflight.Load(), "every read must be closed once the query returns")

							b.ReportMetric(float64(instrumentation.requests.Load())/float64(b.N), "store_reqs/op")
							b.ReportMetric(float64(instrumentation.bytes.Load())/float64(b.N), "store_bytes/op")
							b.ReportMetric(float64(instrumentation.maxInflight.Load()), "store_max_parallel")
						})
					}
				})
			}
		})
	}
}

// benchLimits enables stream-first execution for every tenant, or for none, matching
// logql.NoLimits otherwise.
type benchLimits struct {
	streamFirstEnabled bool
}

func (benchLimits) MaxQuerySeries(_ string) int                             { return math.MaxInt32 }
func (benchLimits) MaxQueryRange(_ context.Context, _ string) time.Duration { return 0 }
func (benchLimits) QueryTimeout(_ context.Context, _ string) time.Duration  { return time.Hour }
func (benchLimits) BlockedQueries(_ context.Context, _ string) []*validation.BlockedQuery {
	return nil
}
func (l benchLimits) StreamFirstExecutionEnabled(_ string) bool { return l.streamFirstEnabled }
func (benchLimits) DebugEngineTasks(_ string) bool              { return false }
func (benchLimits) DebugEngineStreams(_ string) bool            { return false }

// instrumentation is shared by both backends; only one runs per sub-benchmark, so one instance is safe.
type instrumentation struct {
	artificialLatencyNs atomic.Int64
	requests            atomic.Int64
	bytes               atomic.Int64

	// inflight counts reads from the start of the call until the caller closes the body or the
	// call fails. maxInflight is the peak of inflight since the last Reset.
	inflight    atomic.Int64
	maxInflight atomic.Int64
}

// indexKeyPrefix is the key prefix of index reads: the TSDB index of the chunk store and the
// metastore of the data-object store. Both backends skip these reads for counting, tracking and
// latency, so the benchmark compares log-data reads only.
const indexKeyPrefix = "index"

func tracked(key string) bool { return !strings.HasPrefix(key, indexKeyPrefix) }

// begin counts a read, tracks it as in flight and waits out the injected latency. The returned
// function ends the read. It is safe to call more than once.
func (c *instrumentation) begin() (end func()) {
	c.requests.Add(1)
	n := c.inflight.Add(1)
	for {
		peak := c.maxInflight.Load()
		if n <= peak || c.maxInflight.CompareAndSwap(peak, n) {
			break
		}
	}
	if d := c.artificialLatencyNs.Load(); d > 0 {
		time.Sleep(time.Duration(d))
	}
	var once sync.Once
	return func() { once.Do(func() { c.inflight.Add(-1) }) }
}

// Reset zeroes the counters, the in-flight count and the peak, not the injected latency.
func (c *instrumentation) Reset() {
	c.requests.Store(0)
	c.bytes.Store(0)
	c.inflight.Store(0)
	c.maxInflight.Store(0)
}

type instrumentedReadCloser struct {
	inner io.ReadCloser
	c     *instrumentation
	end   func()
}

func (r instrumentedReadCloser) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	r.c.bytes.Add(int64(n))
	return n, err
}

func (r instrumentedReadCloser) Close() error {
	r.end()
	return r.inner.Close()
}

// instrumentedObjectClient counts, tracks and delays object reads, excluding index reads.
type instrumentedObjectClient struct {
	objectclient.ObjectClient
	c *instrumentation
}

func newInstrumentedObjectClient(inner objectclient.ObjectClient, c *instrumentation) *instrumentedObjectClient {
	return &instrumentedObjectClient{ObjectClient: inner, c: c}
}

func (o *instrumentedObjectClient) GetObject(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if !tracked(key) {
		return o.ObjectClient.GetObject(ctx, key)
	}
	end := o.c.begin()
	rc, sz, err := o.ObjectClient.GetObject(ctx, key)
	if err != nil {
		end()
		return rc, sz, err
	}
	return instrumentedReadCloser{rc, o.c, end}, sz, nil
}

func (o *instrumentedObjectClient) GetObjectRange(ctx context.Context, key string, off, length int64) (io.ReadCloser, error) {
	if !tracked(key) {
		return o.ObjectClient.GetObjectRange(ctx, key, off, length)
	}
	end := o.c.begin()
	rc, err := o.ObjectClient.GetObjectRange(ctx, key, off, length)
	if err != nil {
		end()
		return rc, err
	}
	return instrumentedReadCloser{rc, o.c, end}, nil
}

// instrumentedBucket is the data-object counterpart of instrumentedObjectClient.
type instrumentedBucket struct {
	objstore.Bucket
	c *instrumentation
}

func newInstrumentedBucket(inner objstore.Bucket, c *instrumentation) *instrumentedBucket {
	return &instrumentedBucket{Bucket: inner, c: c}
}

func (b *instrumentedBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if !tracked(name) {
		return b.Bucket.Get(ctx, name)
	}
	end := b.c.begin()
	rc, err := b.Bucket.Get(ctx, name)
	if err != nil {
		end()
		return rc, err
	}
	return instrumentedReadCloser{rc, b.c, end}, nil
}

func (b *instrumentedBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if !tracked(name) {
		return b.Bucket.GetRange(ctx, name, off, length)
	}
	end := b.c.begin()
	rc, err := b.Bucket.GetRange(ctx, name, off, length)
	if err != nil {
		end()
		return rc, err
	}
	return instrumentedReadCloser{rc, b.c, end}, nil
}
