package compactionv2

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestResultRecordRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		in   ResultArtifact
	}{
		{
			name: "keeps the path of an artifact without log counts",
			in:   ResultArtifact{Path: "indexes/tenants/acme/ab/cdef"},
		},
		{
			name: "keeps the log counts of a LogMerge artifact",
			in: ResultArtifact{Path: "indexes/tenants/acme/ab/cdef", LogCounts: &LogMergeCounts{
				KeptRecords: 7,
				Sections: []SectionRecords{
					{ObjectPath: "objects/a", SectionIndex: 0, Records: 5},
					{ObjectPath: "objects/b", SectionIndex: 3, Records: 4},
				},
			}},
		},
		{
			name: "keeps log counts with no sections",
			in:   ResultArtifact{Path: "indexes/tenants/acme/ab/cdef", LogCounts: &LogMergeCounts{Sections: []SectionRecords{}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			rec, err := tc.in.ToRecordBatch(mem)
			require.NoError(t, err)
			require.EqualValues(t, 1, rec.NumRows())

			var out ResultArtifact
			err = out.FromRecordBatch(rec)
			rec.Release()
			require.NoError(t, err)
			require.Equal(t, tc.in, out)
		})
	}
}

func TestResultArtifactToRecordBatchInvalid(t *testing.T) {
	rec, err := (ResultArtifact{}).ToRecordBatch(memory.DefaultAllocator)
	require.ErrorContains(t, err, "empty path")
	require.Nil(t, rec)
}

func TestResultArtifactFromRecordBatchNilReceiver(t *testing.T) {
	var artifact *ResultArtifact
	require.ErrorContains(t, artifact.FromRecordBatch(nil), "nil artifact destination")
}

func TestResultArtifactFromRecordBatchInvalid(t *testing.T) {
	for _, tc := range []struct {
		name    string
		schema  *arrow.Schema
		paths   []string
		null    bool
		wantErr string
	}{
		{name: "returns an error when the record is missing", wantErr: "missing record"},
		{name: "returns an error for a record with no rows", schema: ResultRecordSchema, wantErr: "got 0 rows, want 1"},
		{name: "returns an error for a record with two rows", schema: ResultRecordSchema, paths: []string{"a", "b"}, wantErr: "got 2 rows, want 1"},
		{name: "returns an error for an empty path", schema: ResultRecordSchema, paths: []string{""}, wantErr: "empty path"},
		{name: "returns an error for a null path", schema: ResultRecordSchema, null: true, wantErr: "null path"},
		{name: "returns an error for an unknown schema", schema: arrow.NewSchema([]arrow.Field{{Name: "path", Type: arrow.PrimitiveTypes.Int64}}, nil), wantErr: "schema does not match"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			var rec arrow.RecordBatch
			if tc.schema != nil {
				b := array.NewRecordBuilder(mem, tc.schema)
				defer b.Release()
				if tc.schema == ResultRecordSchema {
					path := b.Field(0).(*array.StringBuilder)
					path.AppendValues(tc.paths, nil)
					if tc.null {
						path.AppendNull()
					}
				}
				rec = b.NewRecordBatch()
				defer rec.Release()
			}
			out := ResultArtifact{Path: "previous-result"}
			err := out.FromRecordBatch(rec)
			require.ErrorContains(t, err, tc.wantErr)
			require.Equal(t, ResultArtifact{}, out)
		})
	}

	// logMergeRecord builds a LogMergeResultRecordSchema record with one row
	// per element of rows. Each row holds the given section list lengths, and
	// nullKept makes the kept_records value null.
	type logMergeRow struct {
		paths, indexes, records int
		nullKept                bool
	}
	logMergeRecord := func(mem memory.Allocator, rows ...logMergeRow) arrow.RecordBatch {
		b := array.NewRecordBuilder(mem, LogMergeResultRecordSchema)
		defer b.Release()
		for _, row := range rows {
			b.Field(0).(*array.StringBuilder).Append("index")
			if row.nullKept {
				b.Field(1).(*array.Int64Builder).AppendNull()
			} else {
				b.Field(1).(*array.Int64Builder).Append(1)
			}
			paths, indexes, records := b.Field(2).(*array.ListBuilder), b.Field(3).(*array.ListBuilder), b.Field(4).(*array.ListBuilder)
			paths.Append(true)
			indexes.Append(true)
			records.Append(true)
			for range row.paths {
				paths.ValueBuilder().(*array.StringBuilder).Append("objects/a")
			}
			for range row.indexes {
				indexes.ValueBuilder().(*array.Int64Builder).Append(0)
			}
			for range row.records {
				records.ValueBuilder().(*array.Int64Builder).Append(1)
			}
		}
		return b.NewRecordBatch()
	}
	for _, tc := range []struct {
		name    string
		rows    []logMergeRow
		wantErr string
	}{
		{name: "returns an error when a log count is null", rows: []logMergeRow{{nullKept: true}}, wantErr: "null log count"},
		{name: "returns an error when the section lists have different lengths", rows: []logMergeRow{{paths: 2, indexes: 2, records: 1}}, wantErr: "different lengths"},
		{name: "returns an error for two rows under the LogMerge schema", rows: []logMergeRow{{}, {}}, wantErr: "got 2 rows, want 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			rec := logMergeRecord(mem, tc.rows...)
			defer rec.Release()

			out := ResultArtifact{Path: "previous-result"}
			err := out.FromRecordBatch(rec)
			require.ErrorContains(t, err, tc.wantErr)
			require.Equal(t, ResultArtifact{}, out)
		})
	}
}
