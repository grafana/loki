package metastore

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/bits-and-blooms/bloom/v3"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestApplyBlooms_Passthrough_NoPredicates(t *testing.T) {
	t.Parallel()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
		{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	makeRec := func(paths []string, sections []int64) arrow.RecordBatch {
		pathB := array.NewStringBuilder(memory.DefaultAllocator)
		secB := array.NewInt64Builder(memory.DefaultAllocator)
		pathB.AppendValues(paths, nil)
		secB.AppendValues(sections, nil)
		cols := []arrow.Array{pathB.NewArray(), secB.NewArray()}
		rec := array.NewRecordBatch(schema, cols, int64(len(paths)))
		return rec
	}

	rec1 := makeRec([]string{"a", "b"}, []int64{1, 2})
	rec2 := makeRec([]string{"c"}, []int64{3})

	input := &sliceRecordBatchReader{recs: []arrow.RecordBatch{rec1, rec2}}
	reader := newApplyBlooms(nil, nil, input, nil).(*applyBlooms)

	var total int64
	for {
		rec, err := reader.Read(context.Background())
		if err != nil && err != io.EOF {
			require.NoError(t, err)
		}
		if rec != nil {
			total += rec.NumRows()
		}
		if err == io.EOF {
			break
		}
	}

	require.Equal(t, int64(3), total)
	require.Equal(t, uint64(3), reader.totalReadRows())
}

func TestApplyBlooms_IgnoresNonEqualPredicates(t *testing.T) {
	t.Parallel()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
		{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	pathB := array.NewStringBuilder(memory.DefaultAllocator)
	secB := array.NewInt64Builder(memory.DefaultAllocator)
	pathB.AppendValues([]string{"a"}, nil)
	secB.AppendValues([]int64{1}, nil)
	cols := []arrow.Array{pathB.NewArray(), secB.NewArray()}
	rec := array.NewRecordBatch(schema, cols, 1)

	input := &sliceRecordBatchReader{recs: []arrow.RecordBatch{rec}}
	predicates := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchRegexp, "traceID", "abcd"),
	}
	reader := newApplyBlooms(nil, predicates, input, nil).(*applyBlooms)

	out, err := reader.Read(context.Background())
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, int64(1), out.NumRows())
	require.Equal(t, uint64(1), reader.totalReadRows())
}

func TestApplyBlooms_FiltersByBloomOnSectionKey(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0))

	traceBloom := bloom.NewWithEstimates(10, 0.01)
	traceBloom.AddString("abcd")
	traceBloomBytes, err := traceBloom.MarshalBinary()
	require.NoError(t, err)
	require.NoError(t, builder.AppendColumnIndex(tenantID, "test-path", 0, "traceID", 0, traceBloomBytes))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	inSchema := arrow.NewSchema([]arrow.Field{
		{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
		{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
		{Name: "x.int64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)

	pathB := array.NewStringBuilder(memory.DefaultAllocator)
	secB := array.NewInt64Builder(memory.DefaultAllocator)
	xB := array.NewInt64Builder(memory.DefaultAllocator)
	pathB.AppendValues([]string{"test-path", "other-path"}, nil)
	secB.AppendValues([]int64{0, 0}, nil)
	xB.AppendValues([]int64{10, 20}, nil)
	inCols := []arrow.Array{pathB.NewArray(), secB.NewArray(), xB.NewArray()}

	inRec := array.NewRecordBatch(inSchema, inCols, 2)

	input := &sliceRecordBatchReader{recs: []arrow.RecordBatch{inRec}}
	predicates := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
	}
	reader := newApplyBlooms(obj, predicates, input, nil).(*applyBlooms)

	out, err := reader.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, int64(1), out.NumRows())

	outPath := out.Column(0).(*array.String)
	outSection := out.Column(1).(*array.Int64)
	require.Equal(t, "test-path", outPath.Value(0))
	require.Equal(t, int64(0), outSection.Value(0))
}

func TestApplyBlooms_PredicateMissReturnsEOF(t *testing.T) {
	t.Parallel()

	ctx := user.InjectOrgID(context.Background(), tenantID)

	builder, err := indexobj.NewBuilder(logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       128,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil)
	require.NoError(t, err)

	_, err = builder.AppendStream(tenantID, streams.Stream{
		ID:               1,
		Labels:           labels.New(labels.Label{Name: "app", Value: "foo"}),
		MinTimestamp:     now.Add(-3 * time.Hour),
		MaxTimestamp:     now.Add(-2 * time.Hour),
		UncompressedSize: 5,
	})
	require.NoError(t, err)
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-3*time.Hour), 5))
	require.NoError(t, builder.ObserveLogLine(tenantID, "test-path", 0, 1, 1, now.Add(-2*time.Hour), 0))

	traceBloom := bloom.NewWithEstimates(10, 0.01)
	traceBloom.AddString("abcd")
	traceBloomBytes, err := traceBloom.MarshalBinary()
	require.NoError(t, err)
	require.NoError(t, builder.AppendColumnIndex(tenantID, "test-path", 0, "traceID", 0, traceBloomBytes))

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })

	inSchema := arrow.NewSchema([]arrow.Field{
		{Name: "path.path.utf8", Type: arrow.BinaryTypes.String},
		{Name: "section.int64", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	pathB := array.NewStringBuilder(memory.DefaultAllocator)
	secB := array.NewInt64Builder(memory.DefaultAllocator)
	pathB.AppendValues([]string{"test-path"}, nil)
	secB.AppendValues([]int64{0}, nil)
	inCols := []arrow.Array{pathB.NewArray(), secB.NewArray()}
	inRec := array.NewRecordBatch(inSchema, inCols, 1)

	input := &sliceRecordBatchReader{recs: []arrow.RecordBatch{inRec}}
	predicates := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "traceID", "doesnotexist"),
	}
	reader := newApplyBlooms(obj, predicates, input, nil).(*applyBlooms)

	out, err := reader.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, out)
}

func TestApplyBlooms_InvalidInputSchemaReturnsError(t *testing.T) {
	t.Parallel()

	inSchema := arrow.NewSchema(nil, nil)
	inRec := array.NewRecordBatch(inSchema, nil, 1)

	input := &sliceRecordBatchReader{recs: []arrow.RecordBatch{inRec}}
	predicates := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "traceID", "abcd"),
	}
	reader := newApplyBlooms(nil, predicates, input, nil).(*applyBlooms)

	out, err := reader.Read(context.Background())
	require.Error(t, err)
	require.ErrorContains(t, err, "missing mandatory fields")
	require.Nil(t, out)
}
