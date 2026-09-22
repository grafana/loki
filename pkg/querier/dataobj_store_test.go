package querier

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"

	"github.com/grafana/loki/pkg/push"
)

func TestNewDataObjStore(t *testing.T) {
	builder := objtest.NewBuilder(t)
	builder.Append(testCtx(t), logproto.Stream{Labels: `{app="a"}`, Entries: []push.Entry{entry(1, "one")}})
	builder.Close()

	t.Run("it fails without a chunk store, which every delegated method needs", func(t *testing.T) {
		_, err := NewDataObjStore(nil, builder.Location().Bucket, builder.Metastore(), nil)
		require.ErrorContains(t, err, "chunk store")
	})

	t.Run("it fails without a bucket", func(t *testing.T) {
		_, err := NewDataObjStore(&chunkStoreSpy{}, nil, builder.Metastore(), nil)
		require.ErrorContains(t, err, "bucket")
	})

	t.Run("it fails without a metastore", func(t *testing.T) {
		_, err := NewDataObjStore(&chunkStoreSpy{}, builder.Location().Bucket, nil, nil)
		require.ErrorContains(t, err, "metastore")
	})
}

func TestDataObjStore_SelectSamples(t *testing.T) {
	appStream := logproto.Stream{
		Labels:  `{app="a", env="prod"}`,
		Entries: []push.Entry{entry(1, "one"), entry(2, "two"), entry(3, "three")},
	}
	otherStream := logproto.Stream{
		Labels:  `{app="b", env="prod"}`,
		Entries: []push.Entry{entry(1, "beta")},
	}
	metadataStream := logproto.Stream{
		Labels: `{app="c"}`,
		Entries: []push.Entry{
			entry(1, "err", "level", "error"),
			entry(2, "warn", "level", "warn"),
			entry(3, "blank", "level", ""),
		},
	}

	t.Run("a timestamp-first request is served by the chunk store", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})

		it, err := store.selectSamplesIter(testCtx(t), `sum by (app) (count_over_time({app="a"}[1m]))`, at(0), at(10),
			func(req *logproto.SampleQueryRequest) { req.Order = logproto.SAMPLE_ORDER_BY_TIMESTAMP })
		require.NoError(t, err)
		require.NoError(t, it.Close())

		require.Equal(t, 1, store.chunkStore.selectSamplesCalls, "a timestamp-first request must reach the chunk store")
	})

	t.Run("a literal expression reads no section and yields no sample", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		got := store.selectSamples(t, testCtx(t), `vector(1)`, at(0), at(10))
		require.Empty(t, got)
		require.Zero(t, store.chunkStore.selectSamplesCalls, "a stream-first request must not reach the chunk store")
	})

	t.Run("a selector with no stream matcher fails rather than returning an empty result", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		_, err := store.selectSamplesIter(testCtx(t), `sum by (app) (count_over_time({__name__=~".+"}[1m]))`, at(0), at(10))
		require.ErrorContains(t, err, "at least one stream matcher")
	})

	t.Run("sum(count_over_time) counts every line of the matching stream", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream, otherStream})
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({app="a"}[1m]))`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 3, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
		}, got)
	})

	t.Run("the window includes a line at the start bound and excludes one at the end bound", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({app="a"}[1m]))`, at(1), at(3))
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
		}, got)
	})

	t.Run("sum(bytes_over_time) measures the line length", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		got := store.selectSamples(t, testCtx(t), `sum by (app) (bytes_over_time({app="a"}[1m]))`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 1, Value: 3, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 2, Value: 3, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 3, Value: 5, StreamHash: streamHashOf(appStream.Labels)},
		}, got)
	})

	t.Run("a line filter drops the lines it does not match", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({app="a"} |= "t" [1m]))`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 3, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
		}, got)
	})

	t.Run("a bare range aggregation surfaces structured metadata in the output labels", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{metadataStream})
		got := store.selectSamples(t, testCtx(t), `count_over_time({app="c"}[1m])`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{app="c", level=""}`, TimestampSec: 3, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
			{Labels: `{app="c", level="error"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
			{Labels: `{app="c", level="warn"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
		}, got)
	})

	t.Run("a grouping on a metadata key reads that key and groups by it", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{metadataStream})
		got := store.selectSamples(t, testCtx(t), `sum by (level) (count_over_time({app="c"}[1m]))`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{level=""}`, TimestampSec: 3, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
			{Labels: `{level="error"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
			{Labels: `{level="warn"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
		}, got)
	})

	t.Run("a metadata equality keeps only the matching rows", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{metadataStream})
		got := store.selectSamples(t, testCtx(t), `sum by (level) (count_over_time({app="c"} | level="error" [1m]))`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{level="error"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
		}, got)
	})

	t.Run("a metadata negation keeps the rows whose value is empty", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{metadataStream})
		got := store.selectSamples(t, testCtx(t), `sum by (level) (count_over_time({app="c"} | level!="error" [1m]))`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{level=""}`, TimestampSec: 3, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
			{Labels: `{level="warn"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(metadataStream.Labels)},
		}, got)
	})

	t.Run("a delete request with a line filter excludes the deleted lines", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({app="a"}[1m]))`, at(0), at(10),
			func(req *logproto.SampleQueryRequest) {
				req.Deletes = []*logproto.Delete{{
					Selector: `{app="a"} |= "two"`,
					Start:    at(0).UnixNano(),
					End:      at(10).UnixNano(),
				}}
			})
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 3, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
		}, got)
	})

	t.Run("an access-control filter drops the streams it denies", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream, otherStream}, withStreamFilterer(denyAppFilterer{app: "a"}))
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({env="prod"}[1m]))`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{app="b"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(otherStream.Labels)},
		}, got)
	})

	t.Run("an access-control filter that denies every stream yields no sample and no error", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream, otherStream}, withStreamFilterer(denyEverythingFilterer{}))
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({env="prod"}[1m]))`, at(0), at(10))
		require.Empty(t, got)
	})

	t.Run("several logs sections of one object are all read", func(t *testing.T) {
		var manyStreams []logproto.Stream
		for i := 0; i < 8; i++ {
			manyStreams = append(manyStreams, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="many", idx="%d"}`, i),
				Entries: []push.Entry{entry(1, "line")},
			})
		}
		// A tiny section forces the builder to split these streams across sections.
		store := newTestDataObjStore(t, manyStreams, withSectionSize(flagext.Bytes(1)))
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({app="many"}[1m]))`, at(0), at(10))
		require.Len(t, got, len(manyStreams))
	})

	t.Run("several objects are all read", func(t *testing.T) {
		var manyStreams []logproto.Stream
		for i := 0; i < 4; i++ {
			manyStreams = append(manyStreams, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="split", idx="%d"}`, i),
				Entries: []push.Entry{entry(1, "line")},
			})
		}
		store := newTestDataObjStore(t, manyStreams, withObjectPerStream())
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({app="split"}[1m]))`, at(0), at(10))
		require.Len(t, got, len(manyStreams))
	})

	t.Run("another tenant's sections in the same object are not read", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream},
			withOtherTenantStream("other-tenant", logproto.Stream{
				Labels:  `{app="a", env="prod"}`,
				Entries: []push.Entry{entry(1, "not mine"), entry(2, "not mine either")},
			}))
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({app="a"}[1m]))`, at(0), at(10))
		require.Equal(t, []sampleRow{
			{Labels: `{app="a"}`, TimestampSec: 1, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 2, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
			{Labels: `{app="a"}`, TimestampSec: 3, Value: 1, StreamHash: streamHashOf(appStream.Labels)},
		}, got)
	})

	t.Run("query statistics report the rows and bytes the read decompressed", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		statsCtx, ctx := stats.NewContext(testCtx(t))

		got := store.selectSamples(t, ctx, `sum by (app) (count_over_time({app="a"}[1m]))`, at(0), at(10))
		require.Len(t, got, 3)

		result := statsCtx.Result(time.Second, 0, len(got))
		require.Positive(t, result.Querier.Store.Dataobj.PrePredicateDecompressedBytes, "pre-predicate bytes")
		require.Positive(t, result.Querier.Store.Dataobj.PrePredicateDecompressedRows, "pre-predicate rows")
	})

	t.Run("closing after reading only some samples reports no error", func(t *testing.T) {
		var manyStreams []logproto.Stream
		for i := 0; i < 40; i++ {
			manyStreams = append(manyStreams, logproto.Stream{
				Labels:  fmt.Sprintf(`{app="early", idx="%d"}`, i),
				Entries: []push.Entry{entry(1, "line"), entry(2, "line")},
			})
		}
		// One object per stream, so the planner is still resolving when the read stops.
		store := newTestDataObjStore(t, manyStreams, withObjectPerStream())

		it, err := store.selectSamplesIter(testCtx(t), `sum by (app) (count_over_time({app="early"}[1m]))`, at(0), at(10))
		require.NoError(t, err)
		require.True(t, it.Next())
		require.NoError(t, it.Close(), "stopping early is not a query failure")
	})

	t.Run("a request carrying more than one shard fails rather than reading one of them", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		_, err := store.selectSamplesIter(testCtx(t), `sum by (app) (count_over_time({app="a"}[1m]))`, at(0), at(10),
			func(req *logproto.SampleQueryRequest) {
				req.Shards = []string{
					powerOfTwoShard(0, 2).String(),
					powerOfTwoShard(1, 2).String(),
				}
			})
		require.ErrorContains(t, err, "one shard per request")
	})

	t.Run("a query matching no stream yields no sample and no error", func(t *testing.T) {
		store := newTestDataObjStore(t, []logproto.Stream{appStream})
		got := store.selectSamples(t, testCtx(t), `sum by (app) (count_over_time({app="nothing"}[1m]))`, at(0), at(10))
		require.Empty(t, got)
	})
}

// denyAppFilterer denies every stream whose app label has the given value.
type denyAppFilterer struct{ app string }

func (f denyAppFilterer) ForRequest(context.Context) chunk.Filterer { return f }

func (f denyAppFilterer) ShouldFilter(streamLabels labels.Labels) bool {
	return streamLabels.Get("app") == f.app
}

func (f denyAppFilterer) RequiredLabelNames() []string { return []string{"app"} }

// denyEverythingFilterer denies every stream.
type denyEverythingFilterer struct{}

func (f denyEverythingFilterer) ForRequest(context.Context) chunk.Filterer { return f }

func (f denyEverythingFilterer) ShouldFilter(labels.Labels) bool { return true }

func (f denyEverythingFilterer) RequiredLabelNames() []string { return nil }

// chunkStoreSpy counts the SelectSamples calls that reach the chunk store.
type chunkStoreSpy struct {
	Store

	selectSamplesCalls int
}

func (s *chunkStoreSpy) SelectSamples(context.Context, logql.SelectSampleParams) (iter.SampleIterator, error) {
	s.selectSamplesCalls++
	return iter.NoopSampleIterator, nil
}

// sampleRow is one emitted sample with its identity, for order-independent comparison. The read
// path emits samples in no order, so a test compares sets, never sequences.
type sampleRow struct {
	Labels       string
	TimestampSec int64
	Value        float64
	StreamHash   uint64
}

// testDataObjStore builds a bucket of data objects from streams and returns a store over them.
type testDataObjStore struct {
	store Store

	// chunkStore counts what the store delegated, so a test can tell a served query from a
	// passed-through one.
	chunkStore *chunkStoreSpy
}

type testStoreOptions struct {
	sectionSize       flagext.Bytes
	flushEveryStream  bool
	filterer          chunk.RequestChunkFilterer
	otherTenant       string
	otherTenantStream *logproto.Stream
}

type testStoreOption func(*testStoreOptions)

// withSectionSize caps a logs section so the streams span several sections of one object.
func withSectionSize(size flagext.Bytes) testStoreOption {
	return func(o *testStoreOptions) { o.sectionSize = size }
}

// withObjectPerStream flushes after each stream, so each lands in its own data object.
func withObjectPerStream() testStoreOption {
	return func(o *testStoreOptions) { o.flushEveryStream = true }
}

func withStreamFilterer(filterer chunk.RequestChunkFilterer) testStoreOption {
	return func(o *testStoreOptions) { o.filterer = filterer }
}

// withOtherTenantStream writes a stream for a second tenant into the same objects, so a read
// that miscounted the logs-relative section index would return that tenant's rows.
func withOtherTenantStream(tenant string, stream logproto.Stream) testStoreOption {
	return func(o *testStoreOptions) {
		o.otherTenant = tenant
		o.otherTenantStream = &stream
	}
}

func newTestDataObjStore(t *testing.T, streams []logproto.Stream, opts ...testStoreOption) *testDataObjStore {
	t.Helper()

	var options testStoreOptions
	for _, opt := range opts {
		opt(&options)
	}

	var builderOpts []objtest.Option
	if options.sectionSize > 0 {
		builderOpts = append(builderOpts, objtest.WithTargetSectionSize(options.sectionSize))
	}

	builder := objtest.NewBuilder(t, builderOpts...)
	ctx := user.InjectOrgID(t.Context(), objtest.Tenant)

	if options.otherTenantStream != nil {
		builder.AppendFor(ctx, options.otherTenant, *options.otherTenantStream)
	}
	for _, stream := range streams {
		builder.Append(ctx, stream)
		if options.flushEveryStream {
			builder.Flush(ctx)
		}
	}
	builder.Close()

	var storeOpts []DataObjStoreOption
	if options.filterer != nil {
		storeOpts = append(storeOpts, WithDataObjStreamFilterer(options.filterer))
	}

	location := builder.Location()
	chunkStore := &chunkStoreSpy{}
	store, err := NewDataObjStore(chunkStore, location.Bucket, builder.Metastore(), nil, storeOpts...)
	require.NoError(t, err)

	return &testDataObjStore{store: store, chunkStore: chunkStore}
}

// selectSamples runs a stream-first metric query and returns its samples as a set.
func (s *testDataObjStore) selectSamples(t *testing.T, ctx context.Context, query string, start, end time.Time, mutators ...func(*logproto.SampleQueryRequest)) []sampleRow {
	t.Helper()
	it, err := s.selectSamplesIter(ctx, query, start, end, mutators...)
	require.NoError(t, err)
	return collectSamples(t, it)
}

func (s *testDataObjStore) selectSamplesIter(ctx context.Context, query string, start, end time.Time, mutators ...func(*logproto.SampleQueryRequest)) (iter.SampleIterator, error) {
	expr, err := syntax.ParseSampleExpr(query)
	if err != nil {
		return nil, err
	}
	req := &logproto.SampleQueryRequest{
		Selector: query,
		Start:    start,
		End:      end,
		Order:    logproto.SAMPLE_ORDER_BY_STREAM,
		Plan:     &plan.QueryPlan{AST: expr},
	}
	for _, mutate := range mutators {
		mutate(req)
	}
	return s.store.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: req})
}

func collectSamples(t *testing.T, it iter.SampleIterator) []sampleRow {
	t.Helper()
	var got []sampleRow
	for it.Next() {
		sample := it.At()
		got = append(got, sampleRow{
			Labels:       it.Labels(),
			TimestampSec: sample.Timestamp / int64(time.Second),
			Value:        sample.Value,
			StreamHash:   it.StreamHash(),
		})
	}
	require.NoError(t, it.Err())
	require.NoError(t, it.Close())
	sortSamples(got)
	return got
}

func sortSamples(rows []sampleRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Labels != rows[j].Labels {
			return rows[i].Labels < rows[j].Labels
		}
		if rows[i].TimestampSec != rows[j].TimestampSec {
			return rows[i].TimestampSec < rows[j].TimestampSec
		}
		if rows[i].Value != rows[j].Value {
			return rows[i].Value < rows[j].Value
		}
		// StreamHash is part of the compared tuple, so it has to be part of the order too.
		// Without it two streams that agree on labels, timestamp and value would sort at random.
		return rows[i].StreamHash < rows[j].StreamHash
	})
}

func testCtx(t *testing.T) context.Context {
	return user.InjectOrgID(t.Context(), objtest.Tenant)
}

// epoch anchors every test timestamp, so a query range reads as plain seconds and the metastore's
// time filter has something stable to match.
var epoch = time.Unix(0, 0).UTC()

// at returns the given number of seconds after [epoch].
func at(second int) time.Time { return epoch.Add(time.Duration(second) * time.Second) }

// entry returns one log line at [at](second), with the given structured metadata as alternating
// name and value arguments.
func entry(second int, line string, metadata ...string) push.Entry {
	e := push.Entry{Timestamp: at(second), Line: line}
	for i := 0; i+1 < len(metadata); i += 2 {
		e.StructuredMetadata = append(e.StructuredMetadata, push.LabelAdapter{Name: metadata[i], Value: metadata[i+1]})
	}
	return e
}

// streamHashOf returns the stream hash of a LogQL label set, which is what an emitted sample
// carries to identify its stream.
func streamHashOf(streamLabels string) uint64 {
	parsed, err := syntax.ParseLabels(streamLabels)
	if err != nil {
		panic(err)
	}
	return labels.StableHash(parsed)
}

// powerOfTwoShard builds the shard annotation the query frontend sends.
func powerOfTwoShard(shard, of uint32) *logql.Shard {
	return logql.NewPowerOfTwoShard(index.ShardAnnotation{Shard: shard, Of: of}).Ptr()
}
