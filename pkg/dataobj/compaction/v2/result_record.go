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

// LogMergeResultRecordSchema is [ResultRecordSchema] with the [LogMergeCounts]
// of a LogMerge job, in exactly one row. The section columns are parallel
// lists with one element per section.
var LogMergeResultRecordSchema = arrow.NewSchema([]arrow.Field{
	{Name: "path", Type: arrow.BinaryTypes.String, Nullable: false},
	{Name: "kept_records", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	{Name: "section_object_paths", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: false},
	{Name: "section_indexes", Type: arrow.ListOf(arrow.PrimitiveTypes.Int64), Nullable: false},
	{Name: "section_records", Type: arrow.ListOf(arrow.PrimitiveTypes.Int64), Nullable: false},
}, nil)

// ResultArtifact identifies the replacement index produced by a completed task.
// It is usable only when returned with a nil error. Its index and newly referenced
// log objects have been uploaded, but it has not been published to the ToC.
// The zero value is invalid.
type ResultArtifact struct {
	Path string

	// LogCounts holds the record counts of a LogMerge job. It is nil for
	// other jobs and for LogMerge jobs of workers that do not report counts.
	LogCounts *LogMergeCounts
}

// LogMergeCounts are the record counts of one LogMerge job. They let the
// coordinator check that the jobs of a split merge together keep every input
// record exactly once.
type LogMergeCounts struct {
	// KeptRecords is the number of records that the job wrote.
	KeptRecords int64

	// Sections holds the record count of every input section of the job.
	Sections []SectionRecords
}

// SectionRecords is the number of records in one logs section, from the
// metadata of the section.
type SectionRecords struct {
	ObjectPath   string
	SectionIndex int64
	Records      int64
}

// Validate checks that the artifact identifies a replacement index.
func (a ResultArtifact) Validate() error {
	if a.Path == "" {
		return fmt.Errorf("result artifact: empty path")
	}
	return nil
}

// ToRecordBatch encodes the artifact as a single-row record batch. It uses
// [LogMergeResultRecordSchema] if the artifact has log counts, and
// [ResultRecordSchema] otherwise. The caller owns the returned record batch.
func (a ResultArtifact) ToRecordBatch(mem memory.Allocator) (arrow.RecordBatch, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if a.LogCounts == nil {
		b := array.NewRecordBuilder(mem, ResultRecordSchema)
		defer b.Release()
		b.Field(0).(*array.StringBuilder).Append(a.Path)
		return b.NewRecordBatch(), nil
	}

	b := array.NewRecordBuilder(mem, LogMergeResultRecordSchema)
	defer b.Release()
	b.Field(0).(*array.StringBuilder).Append(a.Path)
	b.Field(1).(*array.Int64Builder).Append(a.LogCounts.KeptRecords)

	paths := b.Field(2).(*array.ListBuilder)
	indexes := b.Field(3).(*array.ListBuilder)
	records := b.Field(4).(*array.ListBuilder)
	paths.Append(true)
	indexes.Append(true)
	records.Append(true)
	for _, section := range a.LogCounts.Sections {
		paths.ValueBuilder().(*array.StringBuilder).Append(section.ObjectPath)
		indexes.ValueBuilder().(*array.Int64Builder).Append(section.SectionIndex)
		records.ValueBuilder().(*array.Int64Builder).Append(section.Records)
	}
	return b.NewRecordBatch(), nil
}

// FromRecordBatch decodes exactly one artifact with a non-null, nonempty path.
// It accepts [ResultRecordSchema] and [LogMergeResultRecordSchema]. The
// artifact does not borrow memory from rec. A failed decode clears the
// receiver.
func (a *ResultArtifact) FromRecordBatch(rec arrow.RecordBatch) error {
	if a == nil {
		return fmt.Errorf("result record: nil artifact destination")
	}
	*a = ResultArtifact{}
	if rec == nil {
		return fmt.Errorf("result record: missing record")
	}
	withCounts := rec.Schema().Equal(LogMergeResultRecordSchema)
	if !withCounts && !rec.Schema().Equal(ResultRecordSchema) {
		return fmt.Errorf("result record: schema does not match a result record schema")
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
	if withCounts {
		counts, err := decodeLogMergeCounts(rec)
		if err != nil {
			return err
		}
		decoded.LogCounts = counts
	}
	*a = decoded
	return nil
}

func decodeLogMergeCounts(rec arrow.RecordBatch) (*LogMergeCounts, error) {
	kept := rec.Column(1).(*array.Int64)
	paths := rec.Column(2).(*array.List)
	indexes := rec.Column(3).(*array.List)
	records := rec.Column(4).(*array.List)
	if kept.IsNull(0) || paths.IsNull(0) || indexes.IsNull(0) || records.IsNull(0) {
		return nil, fmt.Errorf("result record: null log count")
	}

	pathStart, pathEnd := paths.ValueOffsets(0)
	indexStart, indexEnd := indexes.ValueOffsets(0)
	recordStart, recordEnd := records.ValueOffsets(0)
	n := pathEnd - pathStart
	if indexEnd-indexStart != n || recordEnd-recordStart != n {
		return nil, fmt.Errorf("result record: section count lists have different lengths")
	}

	pathValues := paths.ListValues().(*array.String)
	indexValues := indexes.ListValues().(*array.Int64)
	recordValues := records.ListValues().(*array.Int64)
	counts := &LogMergeCounts{KeptRecords: kept.Value(0), Sections: make([]SectionRecords, n)}
	for i := range n {
		counts.Sections[i] = SectionRecords{
			ObjectPath:   strings.Clone(pathValues.Value(int(pathStart + i))),
			SectionIndex: indexValues.Value(int(indexStart + i)),
			Records:      recordValues.Value(int(recordStart + i)),
		}
	}
	return counts, nil
}
