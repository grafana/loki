package arrowcodec

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtobufCodec_ArrowRecordSerialization(t *testing.T) {
	codec := DefaultArrowCodec

	tests := map[string]struct {
		createRecord func() arrow.RecordBatch
	}{
		"simple int64 record": {
			createRecord: createTestArrowRecord,
		},
		"empty record": {
			createRecord: func() arrow.RecordBatch {
				schema := arrow.NewSchema([]arrow.Field{
					{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
				}, nil)

				builder := array.NewInt64Builder(memory.DefaultAllocator)
				data := builder.NewArray()

				return array.NewRecordBatch(schema, []arrow.Array{data}, 0)
			},
		},
		"multiple columns": {
			createRecord: func() arrow.RecordBatch {
				schema := arrow.NewSchema([]arrow.Field{
					{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
					{Name: "value", Type: arrow.PrimitiveTypes.Float64, Nullable: false},
				}, nil)

				idBuilder := array.NewInt64Builder(memory.DefaultAllocator)
				idBuilder.Append(1)
				idBuilder.Append(2)

				valBuilder := array.NewFloat64Builder(memory.DefaultAllocator)
				valBuilder.Append(1.5)
				valBuilder.Append(2.5)

				idData := idBuilder.NewArray()
				valData := valBuilder.NewArray()

				return array.NewRecordBatch(schema, []arrow.Array{idData, valData}, 2)
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			original := tt.createRecord()

			data, err := codec.SerializeArrowRecord(original)
			require.NoError(t, err)
			require.NotEmpty(t, data)

			deserialized, err := codec.DeserializeArrowRecord(data)
			require.NoError(t, err)
			require.NotNil(t, deserialized)

			assert.True(t, original.Schema().Equal(deserialized.Schema()))
			assert.Equal(t, original.NumRows(), deserialized.NumRows())
			assert.Equal(t, original.NumCols(), deserialized.NumCols())
		})
	}
}

func createTestArrowRecord() arrow.RecordBatch {
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64, Nullable: false},
	}, nil)

	builder := array.NewInt64Builder(memory.DefaultAllocator)
	builder.Append(1)
	builder.Append(2)
	builder.Append(3)

	data := builder.NewArray()

	return array.NewRecordBatch(schema, []arrow.Array{data}, 3)
}
