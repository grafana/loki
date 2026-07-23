package compactionv2

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestResultRecordRoundTrip(t *testing.T) {
	in := []ResultArtifact{
		{Path: "indexes/tenants/acme/ab/cdef"},
		{Path: "indexes/tenants/acme/12/3456"},
	}
	rec := BuildResultRecord(memory.DefaultAllocator, in)
	require.EqualValues(t, 2, rec.NumRows())

	out, err := ReadResultRecord(rec)
	require.NoError(t, err)
	require.Equal(t, in, out)
}

func TestReadResultRecordEmpty(t *testing.T) {
	rec := BuildResultRecord(memory.DefaultAllocator, nil)
	require.EqualValues(t, 0, rec.NumRows())

	out, err := ReadResultRecord(rec)
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestReadResultRecordWrongSchema(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "path", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)
	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	b.Field(0).(*array.Int64Builder).Append(1)
	rec := b.NewRecordBatch()

	_, err := ReadResultRecord(rec)
	require.Error(t, err)
}

func TestReadResultRecordNullPath(t *testing.T) {
	b := array.NewRecordBuilder(memory.DefaultAllocator, ResultRecordSchema)
	b.Field(0).(*array.StringBuilder).AppendNull()
	rec := b.NewRecordBatch()

	_, err := ReadResultRecord(rec)
	require.Error(t, err)
}
