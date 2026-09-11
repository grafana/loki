package compactionv2

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// ResultRecordSchema is the Arrow schema of the record a compaction job emits to
// report the index artifacts it produced: one row per produced index.
var ResultRecordSchema = arrow.NewSchema([]arrow.Field{
	{Name: "path", Type: arrow.BinaryTypes.String, Nullable: false},
}, nil)

// ResultArtifact is one produced index artifact reported by a compaction job.
type ResultArtifact struct {
	Path string
}

// BuildResultRecord builds a single record batch of artifacts under
// ResultRecordSchema.
func BuildResultRecord(mem memory.Allocator, artifacts []ResultArtifact) arrow.RecordBatch {
	b := array.NewRecordBuilder(mem, ResultRecordSchema)
	path := b.Field(0).(*array.StringBuilder)
	for _, a := range artifacts {
		path.Append(a.Path)
	}
	return b.NewRecordBatch()
}

// ReadResultRecord reads every row of a result batch into ResultArtifacts. It
// returns an error if the batch's schema does not match ResultRecordSchema or if
// any path value is null.
func ReadResultRecord(rec arrow.RecordBatch) ([]ResultArtifact, error) {
	if !rec.Schema().Equal(ResultRecordSchema) {
		return nil, fmt.Errorf("result record: schema does not match ResultRecordSchema")
	}
	path := rec.Column(0).(*array.String)
	n := int(rec.NumRows())
	if n == 0 {
		return nil, nil
	}
	out := make([]ResultArtifact, n)
	for i := range n {
		if path.IsNull(i) {
			return nil, fmt.Errorf("result record: null path at row %d", i)
		}
		out[i] = ResultArtifact{Path: path.Value(i)}
	}
	return out, nil
}
