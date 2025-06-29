package executor

import (
	"bytes"
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"

	"github.com/grafana/loki/pkg/push"
)

var (
	labelMD    = datatype.ColumnMetadata(types.ColumnTypeLabel, datatype.Loki.String)
	metadataMD = datatype.ColumnMetadata(types.ColumnTypeMetadata, datatype.Loki.String)
)

func Test_dataobjScan(t *testing.T) {
	obj := buildDataobj(t, []logproto.Stream{
		{
			Labels: `{service="loki", env="prod"}`,
			Entries: []logproto.Entry{
				{
					Timestamp:          time.Unix(5, 0),
					Line:               "hello world",
					StructuredMetadata: []push.LabelAdapter{{Name: "guid", Value: "aaaa-bbbb-cccc-dddd"}},
				},
				{
					Timestamp:          time.Unix(10, 0),
					Line:               "goodbye world",
					StructuredMetadata: []push.LabelAdapter{{Name: "guid", Value: "eeee-ffff-aaaa-bbbb"}},
				},
			},
		},
		{
			Labels: `{service="notloki", env="prod"}`,
			Entries: []logproto.Entry{
				{
					Timestamp:          time.Unix(2, 0),
					Line:               "hello world",
					StructuredMetadata: []push.LabelAdapter{{Name: "pod", Value: "notloki-pod-1"}},
				},
				{
					Timestamp:          time.Unix(3, 0),
					Line:               "goodbye world",
					StructuredMetadata: []push.LabelAdapter{{Name: "pod", Value: "notloki-pod-1"}},
				},
			},
		},
	})

	t.Run("All columns", func(t *testing.T) {
		pipeline := newDataobjScanPipeline(t.Context(), dataobjScanOptions{
			Object:      obj,
			StreamIDs:   []int64{1, 2}, // All streams
			Sections:    []int{0},      // All sections (there is only a single one)
			Projections: nil,           // All columns
			Direction:   physical.ASC,
			Limit:       0, // No limit
		})

		expectFields := []arrow.Field{
			{Name: "env", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
			{Name: "service", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
			{Name: "guid", Type: arrow.BinaryTypes.String, Metadata: metadataMD, Nullable: true},
			{Name: "pod", Type: arrow.BinaryTypes.String, Metadata: metadataMD, Nullable: true},
			{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp, Nullable: true},
			{Name: "message", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadataBuiltinMessage, Nullable: true},
		}

		expectCSV := `prod,notloki,NULL,notloki-pod-1,1970-01-01 00:00:02,hello world
prod,notloki,NULL,notloki-pod-1,1970-01-01 00:00:03,goodbye world
prod,loki,aaaa-bbbb-cccc-dddd,NULL,1970-01-01 00:00:05,hello world
prod,loki,eeee-ffff-aaaa-bbbb,NULL,1970-01-01 00:00:10,goodbye world`

		expectRecord, err := CSVToArrow(expectFields, expectCSV)
		require.NoError(t, err)
		defer expectRecord.Release()

		AssertPipelinesEqual(t, pipeline, NewBufferedPipeline(expectRecord))
	})

	t.Run("Column subset", func(t *testing.T) {
		pipeline := newDataobjScanPipeline(t.Context(), dataobjScanOptions{
			Object:    obj,
			StreamIDs: []int64{1, 2}, // All streams
			Sections:  []int{0},      // All sections (there is only a single one)
			Projections: []physical.ColumnExpression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "timestamp", Type: types.ColumnTypeBuiltin}},
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "env", Type: types.ColumnTypeLabel}},
			},
			Direction: physical.ASC,
			Limit:     0, // No limit
		})

		expectFields := []arrow.Field{
			{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp, Nullable: true},
			{Name: "env", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
		}

		expectCSV := `1970-01-01 00:00:02,prod
1970-01-01 00:00:03,prod
1970-01-01 00:00:05,prod
1970-01-01 00:00:10,prod`

		expectRecord, err := CSVToArrow(expectFields, expectCSV)
		require.NoError(t, err)
		defer expectRecord.Release()

		AssertPipelinesEqual(t, pipeline, NewBufferedPipeline(expectRecord))
	})

	t.Run("Unknown column", func(t *testing.T) {
		// Here, we'll check for a column which only exists once in the dataobj but is
		// ambiguous from the perspective of the caller.
		pipeline := newDataobjScanPipeline(t.Context(), dataobjScanOptions{
			Object:    obj,
			StreamIDs: []int64{1, 2}, // All streams
			Sections:  []int{0},      // All sections (there is only a single one)
			Projections: []physical.ColumnExpression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "env", Type: types.ColumnTypeAmbiguous}},
			},
			Direction: physical.ASC,
			Limit:     0, // No limit
		})

		expectFields := []arrow.Field{
			{Name: "env", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
			{Name: "env", Type: arrow.BinaryTypes.String, Metadata: metadataMD, Nullable: true},
		}

		expectCSV := `prod,NULL
prod,NULL
prod,NULL
prod,NULL`

		expectRecord, err := CSVToArrow(expectFields, expectCSV)
		require.NoError(t, err)
		defer expectRecord.Release()

		AssertPipelinesEqual(t, pipeline, NewBufferedPipeline(expectRecord))
	})
}

func Test_dataobjScan_DuplicateColumns(t *testing.T) {
	obj := buildDataobj(t, []logproto.Stream{
		// Case 1: A single row has a value for a label and metadata column with
		// the same name.
		{
			Labels: `{service="loki", env="prod", pod="pod-1"}`,
			Entries: []logproto.Entry{
				{
					Timestamp:          time.Unix(1, 0),
					Line:               "message 1",
					StructuredMetadata: []push.LabelAdapter{{Name: "pod", Value: "override"}},
				},
			},
		},

		// Case 2: A label and metadata column share a name but have values in
		// different rows.
		{
			Labels: `{service="loki", env="prod"}`,
			Entries: []logproto.Entry{{
				Timestamp:          time.Unix(2, 0),
				Line:               "message 2",
				StructuredMetadata: []push.LabelAdapter{{Name: "namespace", Value: "namespace-1"}},
			}},
		},
		{
			Labels: `{service="loki", env="prod", namespace="namespace-2"}`,
			Entries: []logproto.Entry{{
				Timestamp:          time.Unix(3, 0),
				Line:               "message 3",
				StructuredMetadata: nil,
			}},
		},
	})

	t.Run("All columns", func(t *testing.T) {
		pipeline := newDataobjScanPipeline(t.Context(), dataobjScanOptions{
			Object:      obj,
			StreamIDs:   []int64{1, 2, 3}, // All streams
			Sections:    []int{0},         // All sections (there is only a single one)
			Projections: nil,              // All columns
			Direction:   physical.ASC,
			Limit:       0, // No limit
		})

		expectFields := []arrow.Field{
			{Name: "env", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
			{Name: "namespace", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
			{Name: "pod", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
			{Name: "service", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},

			{Name: "namespace", Type: arrow.BinaryTypes.String, Metadata: metadataMD, Nullable: true},
			{Name: "pod", Type: arrow.BinaryTypes.String, Metadata: metadataMD, Nullable: true},

			{Name: "timestamp", Type: arrow.FixedWidthTypes.Timestamp_ns, Metadata: datatype.ColumnMetadataBuiltinTimestamp, Nullable: true},
			{Name: "message", Type: arrow.BinaryTypes.String, Metadata: datatype.ColumnMetadataBuiltinMessage, Nullable: true},
		}

		expectCSV := `prod,NULL,pod-1,loki,NULL,override,1970-01-01 00:00:01,message 1
prod,NULL,NULL,loki,namespace-1,NULL,1970-01-01 00:00:02,message 2
prod,namespace-2,NULL,loki,NULL,NULL,1970-01-01 00:00:03,message 3`

		expectRecord, err := CSVToArrow(expectFields, expectCSV)
		require.NoError(t, err)
		defer expectRecord.Release()

		AssertPipelinesEqual(t, pipeline, NewBufferedPipeline(expectRecord))
	})

	t.Run("Ambiguous pod", func(t *testing.T) {
		pipeline := newDataobjScanPipeline(t.Context(), dataobjScanOptions{
			Object:    obj,
			StreamIDs: []int64{1, 2, 3}, // All streams
			Sections:  []int{0},         // All sections (there is only a single one)
			Projections: []physical.ColumnExpression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "pod", Type: types.ColumnTypeAmbiguous}},
			},
			Direction: physical.ASC,
			Limit:     0, // No limit
		})

		expectFields := []arrow.Field{
			{Name: "pod", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
			{Name: "pod", Type: arrow.BinaryTypes.String, Metadata: metadataMD, Nullable: true},
		}

		expectCSV := `pod-1,override
NULL,NULL
NULL,NULL`

		expectRecord, err := CSVToArrow(expectFields, expectCSV)
		require.NoError(t, err)
		defer expectRecord.Release()

		AssertPipelinesEqual(t, pipeline, NewBufferedPipeline(expectRecord))
	})

	t.Run("Ambiguous namespace", func(t *testing.T) {
		pipeline := newDataobjScanPipeline(t.Context(), dataobjScanOptions{
			Object:    obj,
			StreamIDs: []int64{1, 2, 3}, // All streams
			Sections:  []int{0},         // All sections (there is only a single one)
			Projections: []physical.ColumnExpression{
				&physical.ColumnExpr{Ref: types.ColumnRef{Column: "namespace", Type: types.ColumnTypeAmbiguous}},
			},
			Direction: physical.ASC,
			Limit:     0, // No limit
		})

		expectFields := []arrow.Field{
			{Name: "namespace", Type: arrow.BinaryTypes.String, Metadata: labelMD, Nullable: true},
			{Name: "namespace", Type: arrow.BinaryTypes.String, Metadata: metadataMD, Nullable: true},
		}

		expectCSV := `NULL,NULL
NULL,namespace-1
namespace-2,NULL`

		expectRecord, err := CSVToArrow(expectFields, expectCSV)
		require.NoError(t, err)
		defer expectRecord.Release()

		AssertPipelinesEqual(t, pipeline, NewBufferedPipeline(expectRecord))
	})
}

func buildDataobj(t testing.TB, streams []logproto.Stream) *dataobj.Object {
	t.Helper()

	builder, err := logsobj.NewBuilder(logsobj.BuilderConfig{
		TargetPageSize:          8_000,
		TargetObjectSize:        math.MaxInt,
		TargetSectionSize:       32_000,
		BufferSize:              8_000,
		SectionStripeMergeLimit: 2,
	})
	require.NoError(t, err)

	for _, stream := range streams {
		require.NoError(t, builder.Append(stream))
	}

	var buf bytes.Buffer
	_, err = builder.Flush(&buf)
	require.NoError(t, err)

	r := bytes.NewReader(buf.Bytes())

	obj, err := dataobj.FromReaderAt(r, r.Size())
	require.NoError(t, err)
	return obj
}
