package index

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

var fakeSchema = []string{"label:service_name"}

func newTestIndexBuilder(t *testing.T) *indexobj.Builder {
	t.Helper()
	builder, err := indexobj.NewBuilder("tenant-1", testCalculatorConfig, nil, indexobj.NewBuilderMetrics(nil), nil)
	require.NoError(t, err)
	return builder
}

// makeTestStreamLabels creates a stream labels map with 3 streams.
// stream 1: service_name=svcA
// stream 2: service_name=svcB
// stream 3: (no service_name)
func makeTestStreamLabels() map[int64]labels.Labels {
	return map[int64]labels.Labels{
		1: labels.FromStrings("service_name", "svcA", "env", "prod"),
		2: labels.FromStrings("service_name", "svcB", "env", "dev"),
		3: labels.FromStrings("env", "staging"),
		4: labels.FromStrings("env", "dev"),
	}
}

// makeTestShardBuckets creates the shard buckets for each of the provided labels
func makeTestShardBuckets(testLabels map[int64]labels.Labels) map[int64]uint32 {
	buckets := make(map[int64]uint32, len(testLabels))
	for id, lbls := range testLabels {
		buckets[id] = streams.DefaultShardLayout().BucketFromLabels(lbls)
	}
	return buckets
}

func makeTestCalcContext(builder *indexobj.Builder) *logsCalculationContext {
	testLabels := makeTestStreamLabels()
	return &logsCalculationContext{
		objectPath:         "test/path/obj1",
		sectionIdx:         0,
		streamLabels:       testLabels,
		streamShardBuckets: makeTestShardBuckets(testLabels),
		shardLayout:        streams.DefaultShardLayout(),
		builder:            builder,
	}
}

func TestStatsCalculation_StoresFullyQualifiedSchema(t *testing.T) {
	builder := newTestIndexBuilder(t)
	testLabels := labels.FromStrings("service_name", "svcA", "cluster", "c1")
	ctx := &logsCalculationContext{
		objectPath: "test/path/obj1",
		sectionIdx: 0,
		streamLabels: map[int64]labels.Labels{
			1: testLabels,
		},
		streamShardBuckets: map[int64]uint32{
			1: streams.DefaultShardLayout().BucketFromLabels(testLabels),
		},
		builder: builder,
	}
	calc := &statsCalculation{schema: []string{"label:service_name", "label:cluster"}}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(100, 0).UTC(), Line: []byte("x")},
	}))
	require.NoError(t, calc.Flush(context.Background(), ctx, builder))

	actual := flushAndReadAllStats(t, builder)
	require.Len(t, actual, 1)

	require.Equal(t, "label:service_name,label:cluster", actual[0].SortSchema,
		"stats sort_schema must store the fully-qualified schema")
	require.Equal(t, map[string]string{"service_name": "svcA", "cluster": "c1"}, actual[0].Labels,
		"label columns must stay keyed by the bare Prometheus name")
	require.Equal(t, streams.DefaultShardLayout().BucketFromLabels(ctx.streamLabels[1]), actual[0].ShardBucket)
}

func TestStatsCalculation_GroupsByShardAndSchema(t *testing.T) {
	builder := newTestIndexBuilder(t)
	first := labels.FromStrings("service_name", "api", "instance", "0")
	firstShard := streams.DefaultShardLayout().BucketFromLabels(first)
	var second labels.Labels
	for i := 1; i < 256; i++ {
		candidate := labels.FromStrings("service_name", "api", "instance", strconv.Itoa(i))
		if streams.DefaultShardLayout().BucketFromLabels(candidate) != firstShard {
			second = candidate
			break
		}
	}
	require.NotEmpty(t, second)
	ctx := &logsCalculationContext{
		objectPath: "test/path/obj1",
		sectionIdx: 0,
		streamLabels: map[int64]labels.Labels{
			1: first,
			2: second,
		},
		streamShardBuckets: map[int64]uint32{
			1: streams.DefaultShardLayout().BucketFromLabels(first),
			2: streams.DefaultShardLayout().BucketFromLabels(second),
		},
		builder: builder,
	}
	calc := &statsCalculation{schema: fakeSchema}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))
	records := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(100, 0).UTC(), Line: []byte("a")},
		{StreamID: 2, Timestamp: time.Unix(150, 0).UTC(), Line: []byte("c")},
	}

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, records))
	require.NoError(t, calc.Flush(context.Background(), ctx, builder))

	actual := flushAndReadAllStats(t, builder)
	require.Len(t, actual, 2)
	gotShards := make(map[uint32]struct{}, len(actual))
	for _, row := range actual {
		require.Equal(t, "api", row.Labels["service_name"])
		gotShards[row.ShardBucket] = struct{}{}
	}
	require.Contains(t, gotShards, firstShard)
	require.Contains(t, gotShards, streams.DefaultShardLayout().BucketFromLabels(second))
}

func TestStatsCalculation_MissingShardBucket(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	ctx.streamShardBuckets = map[int64]uint32{}
	calc := &statsCalculation{schema: fakeSchema}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))
	err := calc.ProcessBatch(context.Background(), ctx, []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("x")},
	})
	require.ErrorContains(t, err, "shard bucket not found")
}

func TestStatsCalculation_RejectsUnsupportedSortKey(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{schema: []string{"metadata:trace_id"}}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	err := calc.Prepare(context.Background(), ctx, nil, logs.Stats{})
	require.Error(t, err)
	require.ErrorContains(t, err, "metadata:trace_id")
}

func TestStatsCalculation_BasicAggregation(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{schema: fakeSchema}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

	ts1 := time.Unix(100, 0).UTC()
	ts2 := time.Unix(200, 0).UTC()
	ts3 := time.Unix(150, 0).UTC()

	line1 := []byte("hello world")
	line2 := []byte("another line")
	line3 := []byte("from svcB")
	line4 := []byte("x")

	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: line1},                    // svcA, 11 bytes
		{StreamID: 1, Timestamp: ts2, Line: line2},                    // svcA, 12 bytes
		{StreamID: 2, Timestamp: ts3, Line: line3},                    // svcB, 9 bytes
		{StreamID: 3, Timestamp: time.Unix(50, 0).UTC(), Line: line4}, // no service_name, 1 byte
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx, builder))

	actual := flushAndReadAllStats(t, builder)
	// We expect 3 aggregates: "", "svcA", and "svcB".
	require.Len(t, actual, 3)
	byService := make(map[string]stats.Stat, len(actual))
	for _, row := range actual {
		byService[row.Labels["service_name"]] = row
	}

	require.Equal(t, stats.Stat{
		ObjectPath:       "test/path/obj1",
		SectionIndex:     0,
		SortSchema:       "label:service_name",
		ShardBucket:      streams.DefaultShardLayout().BucketFromLabels(ctx.streamLabels[3]),
		Labels:           map[string]string{"service_name": ""},
		MinTimestamp:     time.Unix(50, 0).UnixNano(),
		MaxTimestamp:     time.Unix(50, 0).UnixNano(),
		RowCount:         1,
		UncompressedSize: int64(len(line4)),
	}, byService[""])

	require.Equal(t, stats.Stat{
		ObjectPath:       "test/path/obj1",
		SectionIndex:     0,
		SortSchema:       "label:service_name",
		ShardBucket:      streams.DefaultShardLayout().BucketFromLabels(ctx.streamLabels[1]),
		Labels:           map[string]string{"service_name": "svcA"},
		MinTimestamp:     ts1.UnixNano(),
		MaxTimestamp:     ts2.UnixNano(),
		RowCount:         2,
		UncompressedSize: int64(len(line1) + len(line2)),
	}, byService["svcA"])

	require.Equal(t, stats.Stat{
		ObjectPath:       "test/path/obj1",
		SectionIndex:     0,
		SortSchema:       "label:service_name",
		ShardBucket:      streams.DefaultShardLayout().BucketFromLabels(ctx.streamLabels[2]),
		Labels:           map[string]string{"service_name": "svcB"},
		MinTimestamp:     ts3.UnixNano(),
		MaxTimestamp:     ts3.UnixNano(),
		RowCount:         1,
		UncompressedSize: int64(len(line3)),
	}, byService["svcB"])
}

func TestStatsCalculation_MetadataFields(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{schema: fakeSchema}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

	ts := time.Unix(500, 0).UTC()
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts, Line: []byte("msg")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx, builder))

	actual := flushAndReadAllStats(t, builder)
	require.Len(t, actual, 1)

	// Verify metadata fields are set correctly.
	require.Equal(t, stats.Stat{
		ObjectPath:       "test/path/obj1",
		SectionIndex:     0,
		SortSchema:       "label:service_name",
		ShardBucket:      streams.DefaultShardLayout().BucketFromLabels(ctx.streamLabels[1]),
		Labels:           map[string]string{"service_name": "svcA"},
		MinTimestamp:     ts.UnixNano(),
		MaxTimestamp:     ts.UnixNano(),
		RowCount:         1,
		UncompressedSize: int64(len([]byte("msg"))),
	}, actual[0])
}

func TestStatsCalculation_IncludesStructuredMetadataBytes(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{schema: fakeSchema}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

	// The uncompressed_logs_size byte contract is line bytes plus structured
	// metadata value bytes, matching streams.Stream.UncompressedSize captured
	// during initial indexing. A one-byte line with ten bytes of metadata must
	// report eleven bytes so compaction output agrees with initial ToC values.
	ts := time.Unix(500, 0).UTC()
	batch := []logs.Record{
		{
			StreamID:  1,
			Timestamp: ts,
			Line:      []byte("x"),
			Metadata:  labels.FromStrings("trace_id", "0123456789"),
		},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx, builder))

	actual := flushAndReadAllStats(t, builder)
	require.Len(t, actual, 1)
	require.Equal(t, int64(len("x")+len("0123456789")), actual[0].UncompressedSize)
}

func TestStatsCalculation_MissingServiceName(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{schema: fakeSchema}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

	batch := []logs.Record{
		{StreamID: 3, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("no svc")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch))
	require.NoError(t, calc.Flush(context.Background(), ctx, builder))

	actual := flushAndReadAllStats(t, builder)
	require.Len(t, actual, 1)
	require.Equal(t, map[string]string{"service_name": ""}, actual[0].Labels)
}

func TestStatsCalculation_MultipleBatches(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{schema: fakeSchema}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))

	// First batch.
	batch1 := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(10, 0).UTC(), Line: []byte("first")},
		{StreamID: 2, Timestamp: time.Unix(20, 0).UTC(), Line: []byte("second")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch1))

	// Second batch — extends svcA's time range and adds more bytes.
	batch2 := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(30, 0).UTC(), Line: []byte("third batch")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, batch2))
	require.NoError(t, calc.Flush(context.Background(), ctx, builder))

	actual := flushAndReadAllStats(t, builder)
	// svcA and svcB
	require.Len(t, actual, 2)
	var svcA stats.Stat
	for _, row := range actual {
		if row.Labels["service_name"] == "svcA" {
			svcA = row
			break
		}
	}
	require.NotEmpty(t, svcA.Labels)
	require.Equal(t, int64(2), svcA.RowCount)
	require.Equal(t, time.Unix(10, 0).UnixNano(), svcA.MinTimestamp)
	require.Equal(t, time.Unix(30, 0).UnixNano(), svcA.MaxTimestamp)
	require.Equal(t, int64(len("first")+len("third batch")), svcA.UncompressedSize)
}

func TestStatsCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	ctx := makeTestCalcContext(builder)
	calc := &statsCalculation{schema: fakeSchema}
	require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), calc.schema)))

	require.NoError(t, calc.Prepare(context.Background(), ctx, nil, logs.Stats{}))
	require.NoError(t, calc.ProcessBatch(context.Background(), ctx, nil))
	require.NoError(t, calc.Flush(context.Background(), ctx, builder))

	// No data → builder is empty, Flush returns ErrBuilderEmpty.
	_, _, err := builder.Flush()
	require.ErrorIs(t, err, indexobj.ErrBuilderEmpty, "expected builder to be empty after empty batch")
}

func flushAndReadAllStats(t *testing.T, builder *indexobj.Builder) []stats.Stat {
	t.Helper()
	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })
	return readAllStats(t, obj)
}

// readAllStats reads the rows of all the stats sections of obj, in section order.
func readAllStats(t *testing.T, obj *dataobj.Object) []stats.Stat {
	t.Helper()
	var all []stats.Stat
	for _, s := range obj.Sections() {
		if !stats.CheckSection(s) {
			continue
		}
		sec, err := stats.Open(t.Context(), s)
		require.NoError(t, err)

		rowReader := stats.NewRowReader(t.Context(), sec, nil)
		for rowReader.Next() {
			all = append(all, rowReader.At())
		}
		require.NoError(t, rowReader.Err())
		require.NoError(t, rowReader.Close())
	}
	return all
}
