package compactionv2

import (
	"testing"
	"time"

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

	t.Run("round-trips every artifact with and without a time range and posting keys", func(t *testing.T) {
		mem := memory.NewCheckedAllocator(memory.DefaultAllocator)
		defer mem.AssertSize(t, 0)
		in := []ResultArtifact{
			{Path: "indexes/a", MinTime: time.Unix(0, 10).UTC(), MaxTime: time.Unix(0, 20).UTC(), PostingKeys: keys},
			{Path: "indexes/b", MinTime: time.Unix(0, 30).UTC(), MaxTime: time.Unix(0, 30).UTC()},
			{Path: "files/c"},
		}
		rec, err := EncodeResultArtifacts(mem, in)
		require.NoError(t, err)
		require.EqualValues(t, 3, rec.NumRows())

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
		{name: "returns an error when a time range sets only one bound", artifacts: []ResultArtifact{{Path: "a", MinTime: time.Unix(0, 10)}}, wantErr: "only one bound"},
		{name: "returns an error when a time range ends before it starts", artifacts: []ResultArtifact{{Path: "a", MinTime: time.Unix(0, 20), MaxTime: time.Unix(0, 10)}}, wantErr: "before its start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec, err := EncodeResultArtifacts(memory.DefaultAllocator, tc.artifacts)
			require.ErrorContains(t, err, tc.wantErr)
			require.Nil(t, rec)
		})
	}
}

func TestDecodeResultArtifacts(t *testing.T) {
	// row sets the columns of one record row. A nil pointer is a null value.
	type row struct {
		path             *string
		minTime, maxTime *int64
		minKey, maxKey   []byte
	}
	str := func(s string) *string { return &s }
	num := func(n int64) *int64 { return &n }
	keys, err := postings.NewKeyRange(
		postings.Key{Kind: postings.KindLabel, ColumnName: "app"},
		postings.Key{Kind: postings.KindLabel, ColumnName: "env"},
	)
	require.NoError(t, err)
	lowKey, highKey := keys.Encode()

	for _, tc := range []struct {
		name    string
		schema  *arrow.Schema
		rows    []row
		wantErr string
	}{
		{name: "returns an error when the record is missing", wantErr: "missing record"},
		{name: "returns an error when the record has another schema", schema: arrow.NewSchema([]arrow.Field{{Name: "path", Type: arrow.PrimitiveTypes.Int64}}, nil), wantErr: "schema does not match"},
		{name: "returns an error when the record has no rows", schema: ResultRecordSchema, wantErr: "no rows"},
		{name: "returns an error when a path is null", schema: ResultRecordSchema, rows: []row{{}}, wantErr: "null path"},
		{name: "returns an error when a path is empty", schema: ResultRecordSchema, rows: []row{{path: str("")}}, wantErr: "empty path"},
		{name: "returns an error when a time range sets only one bound", schema: ResultRecordSchema, rows: []row{{path: str("a"), minTime: num(10)}}, wantErr: "only one bound"},
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
						appendTimestamp(b.Field(1).(*array.TimestampBuilder), r.minTime)
						appendTimestamp(b.Field(2).(*array.TimestampBuilder), r.maxTime)
						appendBinary(b.Field(3).(*array.BinaryBuilder), r.minKey)
						appendBinary(b.Field(4).(*array.BinaryBuilder), r.maxKey)
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

func appendTimestamp(b *array.TimestampBuilder, value *int64) {
	if value == nil {
		b.AppendNull()
		return
	}
	b.Append(arrow.Timestamp(*value))
}

func appendBinary(b *array.BinaryBuilder, value []byte) {
	if value == nil {
		b.AppendNull()
		return
	}
	b.Append(value)
}
