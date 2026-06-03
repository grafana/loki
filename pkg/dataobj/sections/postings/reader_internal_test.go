package postings

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// TestAccumulateStreamMeta_DuplicateStreamID is the regression pin.
//
// The streams section MUST surface each stream_id at most once for the
// postings+streams join to be correct. Before the fix, a duplicate row
// silently overwrote earlier metadata — rowCount / uncompressedSize would
// silently truncate to the LAST row's value, masking upstream builder /
// multi-page bugs.
//
// After the fix, accumulateStreamMeta returns an explicit
// "duplicate stream_id N in streams section" error.
//
// Internal test (package postings) because accumulateStreamMeta is
// unexported by design.
func TestAccumulateStreamMeta_DuplicateStreamID(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "stream_id", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "min_ts", Type: arrow.FixedWidthTypes.Timestamp_ns, Nullable: true},
		{Name: "max_ts", Type: arrow.FixedWidthTypes.Timestamp_ns, Nullable: true},
		{Name: "rows", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
		{Name: "uncompressed_size", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
	}, nil)

	rb := array.NewRecordBuilder(memory.DefaultAllocator, schema)

	// Two rows with the SAME stream_id (=42). Without the fix this silently
	// overwrites the first row; with the fix the second row triggers an
	// error.
	rb.Field(0).(*array.Int64Builder).AppendValues([]int64{42, 42}, nil)
	rb.Field(1).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{100, 200}, nil)
	rb.Field(2).(*array.TimestampBuilder).AppendValues([]arrow.Timestamp{150, 250}, nil)
	rb.Field(3).(*array.Int64Builder).AppendValues([]int64{10, 20}, nil)
	rb.Field(4).(*array.Int64Builder).AppendValues([]int64{1000, 2000}, nil)

	batch := rb.NewRecordBatch()

	out := make(map[int64]streamRowMetadata)
	err := accumulateStreamMeta(batch, out)
	require.Error(t, err, "duplicate stream_id MUST surface as an explicit error, not silently overwrite")
	require.Contains(t, err.Error(), "duplicate stream_id 42",
		"error message must identify the offending stream_id for upstream debugging")
}
