package compactionv2

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

func TestEncodeResultArtifacts(t *testing.T) {
	keys, err := postings.NewKeyRange(
		postings.Key{Kind: postings.KindLabel, ColumnName: "app", LabelValue: "api"},
		postings.Key{Kind: postings.KindLabel, ColumnName: "env", LabelValue: "prod"},
	)
	require.NoError(t, err)

	t.Run("round-trips every artifact with and without posting keys", func(t *testing.T) {
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer mem.AssertSize(t, 0)
		in := []ResultArtifact{
			{Path: "indexes/a", PostingKeys: keys},
			{Path: "files/b"},
		}
		rec, err := EncodeResultArtifacts(mem, in)
		require.NoError(t, err)
		require.EqualValues(t, 2, rec.NumRows())

		out, err := DecodeResultArtifacts(rec)
		rec.Release()
		require.NoError(t, err)
		require.Equal(t, in, out)
	})

	for _, tc := range []struct {
		name      string
		artifacts []ResultArtifact
		wantErr   string
	}{
		{name: "returns an error when there are no artifacts", wantErr: "no artifacts"},
		{name: "returns an error when an artifact has no path", artifacts: []ResultArtifact{{}}, wantErr: "empty path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := EncodeResultArtifacts(memory.DefaultAllocator, tc.artifacts)
			require.ErrorContains(t, err, tc.wantErr)
			require.Nil(t, rec)
		})
	}
}

func TestDecodeResultArtifacts(t *testing.T) {
	// row sets the columns of one record row. A nil value is a null value.
	type row struct {
		path           *string
		minKey, maxKey []byte
	}
	str := func(s string) *string { return &s }
	keys, err := postings.NewKeyRange(
		postings.Key{Kind: postings.KindLabel, ColumnName: "app"},
		postings.Key{Kind: postings.KindLabel, ColumnName: "env"},
	)
	require.NoError(t, err)
	lowKey, highKey := keys.Encode()

	t.Run("decodes the record of a worker that predates the posting key columns as artifacts with unknown posting key bounds", func(t *testing.T) {
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer mem.AssertSize(t, 0)
		b := array.NewRecordBuilder(mem, legacyResultRecordSchema)
		defer b.Release()
		b.Field(0).(*array.StringBuilder).Append("indexes/a")
		rec := b.NewRecordBatch()
		defer rec.Release()

		out, err := DecodeResultArtifacts(rec)
		require.NoError(t, err)
		require.Equal(t, []ResultArtifact{{Path: "indexes/a"}}, out)
	})

	for _, tc := range []struct {
		name    string
		schema  *arrow.Schema
		rows    []row
		wantErr string
	}{
		{name: "returns an error when the record is missing", wantErr: "missing record"},
		{name: "returns an error when the record has another schema", schema: arrow.NewSchema([]arrow.Field{{Name: "path", Type: arrow.PrimitiveTypes.Int64}}, nil), rows: []row{{}}, wantErr: "schema does not match"},
		{name: "returns an error when the record has no rows", schema: ResultRecordSchema, wantErr: "no rows"},
		{name: "returns an error when a path is null", schema: ResultRecordSchema, rows: []row{{}}, wantErr: "null path"},
		{name: "returns an error when a path is empty", schema: ResultRecordSchema, rows: []row{{path: str("")}}, wantErr: "empty path"},
		{name: "returns an error when a row sets only one posting key bound", schema: ResultRecordSchema, rows: []row{{path: str("a"), minKey: lowKey}}, wantErr: "only one posting key bound"},
		{name: "returns an error when the posting key bounds are out of order", schema: ResultRecordSchema, rows: []row{{path: str("a"), minKey: highKey, maxKey: lowKey}}, wantErr: "sorts after"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
			defer mem.AssertSize(t, 0)
			var rec arrow.RecordBatch
			if tc.schema != nil {
				b := array.NewRecordBuilder(mem, tc.schema)
				defer b.Release()
				if tc.schema == ResultRecordSchema {
					for _, r := range tc.rows {
						appendString(b.Field(0).(*array.StringBuilder), r.path)
						appendBinary(b.Field(1).(*array.BinaryBuilder), r.minKey)
						appendBinary(b.Field(2).(*array.BinaryBuilder), r.maxKey)
					}
				} else {
					for range tc.rows {
						b.Field(0).(*array.Int64Builder).Append(1)
					}
				}
				rec = b.NewRecordBatch()
				defer rec.Release()
			}
			out, err := DecodeResultArtifacts(rec)
			require.ErrorContains(t, err, tc.wantErr)
			require.Nil(t, out)
		})
	}
}

func appendString(b *array.StringBuilder, value *string) {
	if value == nil {
		b.AppendNull()
		return
	}
	b.Append(*value)
}

func appendBinary(b *array.BinaryBuilder, value []byte) {
	if value == nil {
		b.AppendNull()
		return
	}
	b.Append(value)
}
