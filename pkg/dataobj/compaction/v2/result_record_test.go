package compactionv2

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestResultRecordRoundTrip(t *testing.T) {
	mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer mem.AssertSize(t, 0)
	in := ResultArtifact{Path: "indexes/tenants/acme/ab/cdef"}
	rec, err := in.ToRecordBatch(mem)
	require.NoError(t, err)
	require.EqualValues(t, 1, rec.NumRows())

	var out ResultArtifact
	err = out.FromRecordBatch(rec)
	rec.Release()
	require.NoError(t, err)
	require.Equal(t, in, out)
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
		{name: "missing", wantErr: "missing record"},
		{name: "empty", schema: ResultRecordSchema, wantErr: "got 0 rows, want 1"},
		{name: "multiple", schema: ResultRecordSchema, paths: []string{"a", "b"}, wantErr: "got 2 rows, want 1"},
		{name: "empty path", schema: ResultRecordSchema, paths: []string{""}, wantErr: "empty path"},
		{name: "null path", schema: ResultRecordSchema, null: true, wantErr: "null path"},
		{name: "wrong schema", schema: arrow.NewSchema([]arrow.Field{{Name: "path", Type: arrow.PrimitiveTypes.Int64}}, nil), wantErr: "schema does not match"},
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
}
