package compactionv2

import (
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// ResultRecordSchema is the Arrow schema of the record a compaction job emits to
// report its replacement index: exactly one row.
var ResultRecordSchema = arrow.NewSchema([]arrow.Field{
	{Name: "path", Type: arrow.BinaryTypes.String, Nullable: false},
}, nil)

// ResultArtifact identifies the replacement index produced by a completed task.
// It is usable only when returned with a nil error. Its index and newly referenced
// log objects have been uploaded, but it has not been published to the ToC.
// The zero value is invalid.
type ResultArtifact struct {
	Path string
}

// Validate checks that the artifact identifies a replacement index.
func (a ResultArtifact) Validate() error {
	if a.Path == "" {
		return fmt.Errorf("result artifact: empty path")
	}
	return nil
}

// ToRecordBatch encodes the artifact as a single-row record batch under
// ResultRecordSchema. The caller owns the returned record batch.
func (a ResultArtifact) ToRecordBatch(mem memory.Allocator) (arrow.RecordBatch, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	b := array.NewRecordBuilder(mem, ResultRecordSchema)
	defer b.Release()
	path := b.Field(0).(*array.StringBuilder)
	path.Append(a.Path)
	return b.NewRecordBatch(), nil
}

// FromRecordBatch decodes exactly one artifact with a non-null, nonempty path.
// The artifact does not borrow memory from rec. A failed decode clears the receiver.
func (a *ResultArtifact) FromRecordBatch(rec arrow.RecordBatch) error {
	if a == nil {
		return fmt.Errorf("result record: nil artifact destination")
	}
	*a = ResultArtifact{}
	if rec == nil {
		return fmt.Errorf("result record: missing record")
	}
	if !rec.Schema().Equal(ResultRecordSchema) {
		return fmt.Errorf("result record: schema does not match ResultRecordSchema")
	}
	if rec.NumRows() != 1 {
		return fmt.Errorf("result record: got %d rows, want 1", rec.NumRows())
	}
	path := rec.Column(0).(*array.String)
	if path.IsNull(0) {
		return fmt.Errorf("result record: null path")
	}
	decoded := ResultArtifact{Path: strings.Clone(path.Value(0))}
	if err := decoded.Validate(); err != nil {
		return err
	}
	*a = decoded
	return nil
}
