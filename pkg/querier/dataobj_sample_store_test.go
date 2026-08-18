package querier

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/plan"
)

const dataObjTestTenant = "fake"

// sampleRow is one sample from SelectSamples together with its stream identity, for order-independent
// set assertions. Timestamp, Value, and Hash come from the embedded logproto.Sample.
type sampleRow struct {
	logproto.Sample
	labels     string
	streamHash uint64
}

// collectSamples drains it into sampleRows and asserts no error. The data-object reader emits samples
// in no guaranteed order, so callers compare the result as a set (require.ElementsMatch).
func collectSamples(t *testing.T, it iter.SampleIterator) []sampleRow {
	t.Helper()
	var got []sampleRow
	for it.Next() {
		got = append(got, sampleRow{Sample: it.At(), labels: it.Labels(), streamHash: it.StreamHash()})
	}
	require.NoError(t, it.Err())
	return got
}

// TestDataObjSampleStore_SelectSamples covers the data-object sample store's SelectSamples: request
// routing, the samples it returns from a data object, and structured-metadata predicate push-down.
func TestDataObjSampleStore_SelectSamples(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)

	t.Run("timestamp-first request is forwarded to the chunk store", func(t *testing.T) {
		chunk := &recordingSampleStore{}
		bucket := objstore.NewInMemBucket()
		ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, nil) // empty: a real metastore with no objects to resolve
		store := NewDataObjSampleStore(chunk, bucket, ms, nil, false, log.NewNopLogger(), nil)

		params := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
			Start: time.Unix(0, 0),
			End:   time.Unix(100, 0),
			Order: logproto.SAMPLE_ORDER_BY_TIMESTAMP,
		}}

		_, err := store.SelectSamples(ctx, params)
		require.NoError(t, err)

		// A timestamp-first request returns the chunk store's iterator directly; the data-object path
		// (which would build its own iterator) is never taken.
		require.Len(t, chunk.calls, 1, "timestamp-first request must be forwarded to the chunk store")
	})

	t.Run("stream-first count_over_time returns one sample per line from data objects", func(t *testing.T) {
		foo := labels.FromStrings("app", "foo", "cluster", "test")
		bar := labels.FromStrings("app", "bar", "cluster", "test")

		testStreams := []logproto.Stream{
			{Labels: foo.String(), Entries: []push.Entry{
				{Timestamp: time.Unix(10, 0), Line: "foo-c"},
				{Timestamp: time.Unix(6, 0), Line: "foo-b"},
				{Timestamp: time.Unix(2, 0), Line: "foo-a"},
			}},
			{Labels: bar.String(), Entries: []push.Entry{
				{Timestamp: time.Unix(20, 0), Line: "bar-b"},
				{Timestamp: time.Unix(15, 0), Line: "bar-a"},
			}},
		}

		bucket := objstore.NewInMemBucket()
		ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, [][]logproto.Stream{testStreams})
		store := NewDataObjSampleStore(nil, bucket, ms, nil, false, log.NewNopLogger(), nil)

		expr, err := syntax.ParseSampleExpr(`count_over_time({cluster="test"}[1h])`)
		require.NoError(t, err)
		params := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
			Start:    time.Unix(0, 0),
			End:      time.Unix(100, 0),
			Selector: expr.String(),
			Plan:     &plan.QueryPlan{AST: expr},
			Order:    logproto.SAMPLE_ORDER_BY_STREAM,
		}}

		it, err := store.SelectSamples(ctx, params)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, it.Close()) })

		got := collectSamples(t, it)

		// One sample per matching line: value 1 (count_over_time), Hash 0 (data objects are not
		// deduplicated at query time), and — with no grouping — the stream's own labels. The reader is
		// unordered (no cross-stream order, no timestamp order within a stream), so match as a set.
		u := func(sec int64) int64 { return time.Unix(sec, 0).UnixNano() }
		fooFP, barFP := labels.StableHash(foo), labels.StableHash(bar)
		require.ElementsMatch(t, []sampleRow{
			{Sample: logproto.Sample{Timestamp: u(2), Value: 1}, labels: foo.String(), streamHash: fooFP},
			{Sample: logproto.Sample{Timestamp: u(6), Value: 1}, labels: foo.String(), streamHash: fooFP},
			{Sample: logproto.Sample{Timestamp: u(10), Value: 1}, labels: foo.String(), streamHash: fooFP},
			{Sample: logproto.Sample{Timestamp: u(15), Value: 1}, labels: bar.String(), streamHash: barFP},
			{Sample: logproto.Sample{Timestamp: u(20), Value: 1}, labels: bar.String(), streamHash: barFP},
		}, got)
	})

	t.Run("records processed bytes in query stats", func(t *testing.T) {
		foo := labels.FromStrings("app", "foo", "cluster", "test")
		bar := labels.FromStrings("app", "bar", "cluster", "test")

		// The lines carry structured metadata so a bare count_over_time projects the metadata column as a
		// secondary (non-predicate) column, exercising the post-predicate byte accounting too.
		md := push.LabelsAdapter{{Name: "trace_id", Value: "t1"}}
		testStreams := []logproto.Stream{
			{Labels: foo.String(), Entries: []push.Entry{
				{Timestamp: time.Unix(2, 0), Line: "foo-a", StructuredMetadata: md},
				{Timestamp: time.Unix(6, 0), Line: "foo-b", StructuredMetadata: md},
			}},
			{Labels: bar.String(), Entries: []push.Entry{
				{Timestamp: time.Unix(20, 0), Line: "bar-a", StructuredMetadata: md},
			}},
		}

		bucket := objstore.NewInMemBucket()
		ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, [][]logproto.Stream{testStreams})
		store := NewDataObjSampleStore(nil, bucket, ms, nil, false, log.NewNopLogger(), nil)

		expr, err := syntax.ParseSampleExpr(`count_over_time({cluster="test"}[1h])`)
		require.NoError(t, err)
		params := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
			Start:    time.Unix(0, 0),
			End:      time.Unix(100, 0),
			Selector: expr.String(),
			Plan:     &plan.QueryPlan{AST: expr},
			Order:    logproto.SAMPLE_ORDER_BY_STREAM,
		}}

		statsData, statsCtx := stats.NewContext(ctx)
		it, err := store.SelectSamples(statsCtx, params)
		require.NoError(t, err)

		_ = collectSamples(t, it)
		// Close bridges the read bytes into statsCtx, so it must run before Result reads them.
		require.NoError(t, it.Close())

		res := statsData.Result(0, 0, 0)
		require.Positive(t, res.Querier.Store.Dataobj.PrePredicateDecompressedBytes,
			"data-object reads must report pre-predicate (stream_id + timestamp) bytes (dropped before the fix)")
		require.Positive(t, res.Querier.Store.Dataobj.PostPredicateDecompressedBytes,
			"data-object reads must report post-predicate (secondary metadata column) bytes")
		require.Equal(t,
			res.Querier.Store.Dataobj.PrePredicateDecompressedBytes+res.Querier.Store.Dataobj.PostPredicateDecompressedBytes,
			res.Summary.TotalBytesProcessed,
			"the data-object bytes must flow into summary.totalBytesProcessed")

		// A repeated Close must not add the bytes again.
		require.NoError(t, it.Close())
		require.Equal(t, res.Querier.Store.Dataobj.PrePredicateDecompressedBytes,
			statsData.Result(0, 0, 0).Querier.Store.Dataobj.PrePredicateDecompressedBytes,
			"a repeated Close must not double-count processed bytes")

		// Ensure metrics are tracked.
		m := store.(*dataObjSampleStore).metrics
		require.Positive(t, testutil.ToFloat64(m.fetchedCompressedBytes.WithLabelValues(dataObjComponentMetastore)),
			"the metastore must report fetched bytes")
		require.Positive(t, testutil.ToFloat64(m.fetchedCompressedBytes.WithLabelValues(dataObjComponentStreamsReader)),
			"opening the data object must report fetched bytes on the streams reader")
		require.Positive(t, testutil.ToFloat64(m.processedUncompressedBytes.WithLabelValues(dataObjComponentLogsReader)),
			"the logs reader must report processed bytes")
		require.Equal(t,
			res.Querier.Store.Dataobj.PrePredicateDecompressedBytes+res.Querier.Store.Dataobj.PostPredicateDecompressedBytes,
			int64(testutil.ToFloat64(m.processedUncompressedBytes.WithLabelValues(dataObjComponentLogsReader))),
			"the logs-reader processed metric must match the query-stat processed bytes")

		// No bytes may land in "other": every read region must map to a known component. This fails
		// loudly if a region name drifts out of componentForRootRegion's cases.
		require.Zero(t, testutil.ToFloat64(m.fetchedCompressedBytes.WithLabelValues(dataObjComponentOther)),
			"no fetched bytes may fall into the other bucket")
		require.Zero(t, testutil.ToFloat64(m.processedUncompressedBytes.WithLabelValues(dataObjComponentOther)),
			"no processed bytes may fall into the other bucket")
	})

	t.Run("structured-metadata equality filter is pushed down", func(t *testing.T) {
		foo := labels.FromStrings("app", "foo", "cluster", "test")
		bar := labels.FromStrings("app", "bar", "cluster", "test")
		md := func(traceID string) push.LabelsAdapter {
			return push.LabelsAdapter{{Name: "trace_id", Value: traceID}}
		}

		testStreams := []logproto.Stream{
			{Labels: foo.String(), Entries: []push.Entry{
				{Timestamp: time.Unix(1, 0), Line: "a", StructuredMetadata: md("t1")},
				{Timestamp: time.Unix(2, 0), Line: "b", StructuredMetadata: md("target")},
				{Timestamp: time.Unix(3, 0), Line: "c", StructuredMetadata: md("t3")},
			}},
			{Labels: bar.String(), Entries: []push.Entry{
				{Timestamp: time.Unix(4, 0), Line: "d", StructuredMetadata: md("target")},
				{Timestamp: time.Unix(5, 0), Line: "e", StructuredMetadata: md("t5")},
			}},
		}

		bucket := objstore.NewInMemBucket()
		ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, [][]logproto.Stream{testStreams})
		store := NewDataObjSampleStore(nil, bucket, ms, nil, false, log.NewNopLogger(), nil)

		expr, err := syntax.ParseSampleExpr(`count_over_time({cluster="test"} | trace_id="target"[1h])`)
		require.NoError(t, err)
		params := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
			Start:    time.Unix(0, 0),
			End:      time.Unix(100, 0),
			Selector: expr.String(),
			Plan:     &plan.QueryPlan{AST: expr},
			Order:    logproto.SAMPLE_ORDER_BY_STREAM,
		}}

		statsData, sctx := stats.NewContext(ctx)
		it, err := store.SelectSamples(sctx, params)
		require.NoError(t, err)

		got := collectSamples(t, it)
		require.NoError(t, it.Close()) // Close bridges the read bytes into statsData; must precede Result.

		// Only the two lines with trace_id="target" survive — one per stream. count_over_time emits value 1
		// per line, Hash 0, and with no grouping the surviving line's structured metadata surfaces in the
		// output labels.
		fooTarget := labels.FromStrings("app", "foo", "cluster", "test", "trace_id", "target")
		barTarget := labels.FromStrings("app", "bar", "cluster", "test", "trace_id", "target")
		require.ElementsMatch(t, []sampleRow{
			{Sample: logproto.Sample{Timestamp: time.Unix(2, 0).UnixNano(), Value: 1}, labels: fooTarget.String(), streamHash: labels.StableHash(foo)},
			{Sample: logproto.Sample{Timestamp: time.Unix(4, 0).UnixNano(), Value: 1}, labels: barTarget.String(), streamHash: labels.StableHash(bar)},
		}, got)

		// Pushing the trace_id filter down makes the metadata column a predicate (primary) column, so its
		// bytes count as pre-predicate and there is no secondary column to read. This pins the
		// primary/secondary mapping: the bare-count subtest reports post-predicate bytes, this must not.
		res := statsData.Result(0, 0, 0)
		require.Positive(t, res.Querier.Store.Dataobj.PrePredicateDecompressedBytes)
		require.Zero(t, res.Querier.Store.Dataobj.PostPredicateDecompressedBytes,
			"a pushed-down metadata filter reads trace_id as a primary column, leaving nothing secondary")
	})
}

const testSectionSize = 1 << 20 // large: all of an object's streams land in one logs section

// TestDataObjSampleIterator_RecordsAreStreamClustered documents why the iterator's single last-stream
// cache is enough: the logs section is sorted by stream, so records arrive in stream-clustered runs. It
// builds several objects (each a concurrently-scanned section) of many-line streams and checks that,
// over the drained StreamHash sequence, only a few distinct streams appear within any read batch and
// consecutive stream changes stay far below the record count — so the last-stream entry serves almost
// every line even though the concurrent sections interleave their batches.
func TestDataObjSampleIterator_RecordsAreStreamClustered(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)

	const (
		numObjects     = 6
		streamsPerObj  = 4
		linesPerStream = 800
	)
	var groups [][]logproto.Stream
	idx := 0
	for o := 0; o < numObjects; o++ {
		var group []logproto.Stream
		for s := 0; s < streamsPerObj; s++ {
			lbls := labels.FromStrings("cluster", "test", "pod", fmt.Sprintf("pod-%05d", idx))
			entries := make([]push.Entry, linesPerStream)
			for j := 0; j < linesPerStream; j++ {
				entries[j] = push.Entry{Timestamp: time.Unix(int64(j+1), 0), Line: "x"}
			}
			group = append(group, logproto.Stream{Labels: lbls.String(), Entries: entries})
			idx++
		}
		groups = append(groups, group)
	}

	bucket := objstore.NewInMemBucket()
	ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, groups)
	store := NewDataObjSampleStore(nil, bucket, ms, nil, false, log.NewNopLogger(), nil)

	expr, err := syntax.ParseSampleExpr(`count_over_time({cluster="test"}[24h])`)
	require.NoError(t, err)
	params := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
		Start:    time.Unix(0, 0),
		End:      time.Unix(100000, 0),
		Selector: expr.String(),
		Plan:     &plan.QueryPlan{AST: expr},
		Order:    logproto.SAMPLE_ORDER_BY_STREAM,
	}}

	it, err := store.SelectSamples(ctx, params)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	var seq []uint64
	for it.Next() {
		seq = append(seq, it.StreamHash())
	}
	require.NoError(t, it.Err())
	require.Len(t, seq, numObjects*streamsPerObj*linesPerStream)

	distinct := map[uint64]struct{}{}
	for _, h := range seq {
		distinct[h] = struct{}{}
	}
	changes := 1 // the first record is a "change" from nothing
	for i := 1; i < len(seq); i++ {
		if seq[i] != seq[i-1] {
			changes++
		}
	}
	maxWin, win := 0, map[uint64]int{}
	for i, h := range seq {
		win[h]++
		if i >= 1024 {
			old := seq[i-1024]
			if win[old]--; win[old] == 0 {
				delete(win, old)
			}
		}
		if len(win) > maxWin {
			maxWin = len(win)
		}
	}

	t.Logf("records=%d distinct=%d streamChanges=%d maxDistinctPer1024=%d", len(seq), len(distinct), changes, maxWin)

	// Records are stream-clustered, so the single last-stream cache (which evicts on every stream change)
	// still hits on the vast majority of lines: stream changes stay a tiny fraction of the record count.
	require.Less(t, changes, len(seq)/20, "records are not stream-clustered; the single-entry cache would thrash")
}

// newTestDataObjMetastore builds one data object per stream group on the bucket — object + postings
// index + metastore table-of-contents, the way the dataobj-consumer and dataobj-index-builder do —
// registers them all in one real metastore, and returns it. sectionSize caps each logs section, so a
// small value forces several logs sections within an object; pass testSectionSize for one per object.
func newTestDataObjMetastore(ctx context.Context, t *testing.T, bucket objstore.Bucket, sectionSize int, objects [][]logproto.Stream) metastore.Metastore {
	t.Helper()

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		TargetObjectSize:        1 << 20,
		TargetSectionSize:       flagext.Bytes(sectionSize),
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}
	for _, group := range objects {
		buildAndIndexObject(ctx, t, bucket, cfg, group)
	}
	return metastore.NewObjectMetastore(bucket, metastore.Config{ReadPostingsSections: true}, log.NewNopLogger(), metastore.NewObjectMetastoreMetrics(nil))
}

// buildAndIndexObject builds one logs data object from objStreams, uploads it, indexes it, and
// registers the index in the metastore table of contents. Appends use an epoch-era time so the
// metastore's time-range filter matches the fixtures' timestamps.
func buildAndIndexObject(ctx context.Context, t *testing.T, bucket objstore.Bucket, cfg logsobj.BuilderBaseConfig, objStreams []logproto.Stream) {
	t.Helper()

	up := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger())

	logsBuilder, err := logsobj.NewBuilder(logsobj.BuilderConfig{BuilderBaseConfig: cfg}, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)
	for _, s := range objStreams {
		require.NoError(t, logsBuilder.Append(dataObjTestTenant, s, time.Unix(0, 0)))
	}
	logsObj, logsCloser, err := logsBuilder.Flush()
	require.NoError(t, err)
	logsPath, err := up.Upload(ctx, logsObj)
	require.NoError(t, err)
	require.NoError(t, logsCloser.Close())

	calc := index.NewCalculator(mustIndexBuilder(t, cfg))
	logsRO, err := dataobj.FromBucket(ctx, bucket, logsPath, 0)
	require.NoError(t, err)
	require.NoError(t, calc.Calculate(ctx, log.NewNopLogger(), logsRO, logsPath))

	idxObj, idxCloser, timeRanges, err := calc.Flush()
	require.NoError(t, err)
	idxPath, err := up.Upload(ctx, idxObj)
	require.NoError(t, err)
	require.NoError(t, idxCloser.Close())

	toc := metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger())
	require.NoError(t, toc.WriteEntry(ctx, idxPath, timeRanges))
}

func mustIndexBuilder(t *testing.T, cfg logsobj.BuilderBaseConfig) *indexobj.Builder {
	t.Helper()
	b, err := indexobj.NewBuilder(cfg, nil)
	require.NoError(t, err)
	return b
}

// buildDataObject builds a single-section logs data object from the given streams, uploads it to the
// bucket, and returns the stream IDs the builder assigned.
func buildDataObject(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string, testStreams []logproto.Stream) []int64 {
	t.Helper()

	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          2048,
			TargetObjectSize:        1 << 20,
			TargetSectionSize:       1 << 20, // large, so all streams land in one logs section
			BufferSize:              2048 * 8,
			SectionStripeMergeLimit: 2,
		},
	}
	builder, err := logsobj.NewBuilder(cfg, nil, logsobj.NewBuilderMetrics(), log.NewNopLogger(), nil)
	require.NoError(t, err)

	for _, s := range testStreams {
		require.NoError(t, builder.Append(dataObjTestTenant, s, time.Now()))
	}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	require.Equal(t, 1, obj.Sections().Count(streams.CheckSection))
	require.Equal(t, 1, obj.Sections().Count(logs.CheckSection), "test assumes a single logs section")

	ids := streamIDsOf(ctx, t, obj)

	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, reader.Close()) })
	require.NoError(t, bucket.Upload(ctx, path, reader))

	return ids
}

func streamIDsOf(ctx context.Context, t *testing.T, obj *dataobj.Object) []int64 {
	t.Helper()

	var ids []int64
	for _, sec := range obj.Sections().Filter(streams.CheckSection) {
		ss, err := streams.Open(ctx, sec)
		require.NoError(t, err)

		reader := streams.NewRowReader(ss)
		require.NoError(t, reader.Open(ctx))

		buf := make([]streams.Stream, 128)
		for {
			n, err := reader.Read(ctx, buf)
			if err != nil && !errors.Is(err, io.EOF) {
				require.NoError(t, err)
			}
			for i := range buf[:n] {
				ids = append(ids, buf[i].ID)
			}
			if n == 0 && errors.Is(err, io.EOF) {
				break
			}
		}
		require.NoError(t, reader.Close())
	}
	return ids
}

// TestDataObjSampleStore_ShardBucketFiltering checks the shard-bucket pruning path: for every shard of
// several shard counts, the pruned read (flag on) returns exactly what the fingerprint-filtered read
// (flag off) returns, and the shards together cover every stream exactly once.
func TestDataObjSampleStore_ShardBucketFiltering(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), dataObjTestTenant)

	// Many distinct streams so their fingerprints populate a spread of shard buckets. One line each; the
	// per-stream sample count is irrelevant to shard membership.
	const numStreams = 64
	var testStreams []logproto.Stream
	wantAll := map[string]struct{}{}
	for i := 0; i < numStreams; i++ {
		lbls := labels.FromStrings("job", "shardtest", "app", fmt.Sprintf("s%02d", i))
		testStreams = append(testStreams, logproto.Stream{Labels: lbls.String(), Entries: []push.Entry{
			{Timestamp: time.Unix(int64(10+i), 0), Line: "x"},
		}})
		wantAll[lbls.String()] = struct{}{}
	}

	bucket := objstore.NewInMemBucket()
	ms := newTestDataObjMetastore(ctx, t, bucket, testSectionSize, [][]logproto.Stream{testStreams})
	storeOff := NewDataObjSampleStore(nil, bucket, ms, nil, false, log.NewNopLogger(), nil)
	storeOn := NewDataObjSampleStore(nil, bucket, ms, nil, true, log.NewNopLogger(), nil)

	expr, err := syntax.ParseSampleExpr(`count_over_time({job="shardtest"}[1h])`)
	require.NoError(t, err)

	query := func(store Store, shards []string) []sampleRow {
		params := logql.SelectSampleParams{SampleQueryRequest: &logproto.SampleQueryRequest{
			Start:    time.Unix(0, 0),
			End:      time.Unix(1000, 0),
			Selector: expr.String(),
			Plan:     &plan.QueryPlan{AST: expr},
			Order:    logproto.SAMPLE_ORDER_BY_STREAM,
			Shards:   shards,
		}}
		it, err := store.SelectSamples(ctx, params)
		require.NoError(t, err)
		defer func() { require.NoError(t, it.Close()) }()
		return collectSamples(t, it)
	}

	labelsOf := func(rows []sampleRow) []string {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.labels)
		}
		return out
	}

	// Unsharded: both flags must return every stream once.
	require.ElementsMatch(t, labelsOf(query(storeOff, nil)), labelsOf(query(storeOn, nil)))
	require.Len(t, query(storeOn, nil), numStreams)

	// Of = 32 exercises the exact (skip-recheck) path; Of = 64 exercises the over-fetch (recheck) path;
	// smaller counts exercise multi-bucket exact ranges.
	for _, of := range []int{2, 4, 8, 32, 64} {
		seen := map[string]int{}
		for shard := 0; shard < of; shard++ {
			shards := []string{fmt.Sprintf("%d_of_%d", shard, of)}
			off := query(storeOff, shards)
			on := query(storeOn, shards)
			require.ElementsMatchf(t, labelsOf(off), labelsOf(on), "of=%d shard=%d: pruned read must match the fingerprint-filtered read", of, shard)
			for _, l := range labelsOf(on) {
				seen[l]++
			}
		}
		// The shards partition the streams: every stream appears in exactly one shard.
		require.Lenf(t, seen, numStreams, "of=%d: shards must together cover every stream", of)
		for l, c := range seen {
			require.Equalf(t, 1, c, "of=%d: stream %s appeared in %d shards, want 1", of, l, c)
		}
	}
}
