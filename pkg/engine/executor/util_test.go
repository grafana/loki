package executor

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

var (
	incrementingIntPipeline = newRecordGenerator(
		arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		}, nil),

		func(offset, sz int64, schema *arrow.Schema) arrow.Record {
			builder := array.NewInt64Builder(memory.DefaultAllocator)
			defer builder.Release()

			for i := int64(0); i < sz; i++ {
				builder.Append(offset + i)
			}

			data := builder.NewArray()
			defer data.Release()

			columns := []arrow.Array{data}
			return array.NewRecord(schema, columns, sz)
		},
	)
)

func ascendingTimestampPipeline(start time.Time) *recordGenerator {
	return timestampPipeline(start, ascending)
}

func descendingTimestampPipeline(start time.Time) *recordGenerator {
	return timestampPipeline(start, descending)
}

const (
	ascending  = time.Duration(1)
	descending = time.Duration(-1)
)

func timestampPipeline(start time.Time, order time.Duration) *recordGenerator {
	return newRecordGenerator(
		arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: arrow.PrimitiveTypes.Int64},
			{Name: "timestamp", Type: arrow.PrimitiveTypes.Uint64},
		}, nil),

		func(offset, sz int64, schema *arrow.Schema) arrow.Record {
			idColBuilder := array.NewInt64Builder(memory.DefaultAllocator)
			defer idColBuilder.Release()

			tsColBuilder := array.NewUint64Builder(memory.DefaultAllocator)
			defer tsColBuilder.Release()

			for i := int64(0); i < sz; i++ {
				idColBuilder.Append(offset + i)
				tsColBuilder.Append(uint64(start.Add(order * (time.Duration(offset)*time.Second + time.Duration(i)*time.Millisecond)).UnixNano()))
			}

			idData := idColBuilder.NewArray()
			defer idData.Release()

			tsData := tsColBuilder.NewArray()
			defer tsData.Release()

			columns := []arrow.Array{idData, tsData}
			return array.NewRecord(schema, columns, sz)
		},
	)
}

type recordGenerator struct {
	schema *arrow.Schema
	batch  func(offset, sz int64, schema *arrow.Schema) arrow.Record
}

func newRecordGenerator(schema *arrow.Schema, batch func(offset, sz int64, schema *arrow.Schema) arrow.Record) *recordGenerator {
	return &recordGenerator{
		schema: schema,
		batch:  batch,
	}
}

func (p *recordGenerator) Pipeline(batchSize int64, rows int64) Pipeline {
	var pos int64
	return newGenericPipeline(
		Local,
		func(_ []Pipeline) state {
			if pos >= rows {
				return Exhausted
			}
			batch := p.batch(pos, batchSize, p.schema)
			pos += batch.NumRows()
			return successState(batch)
		},
		nil,
	)
}

// collect reads all data from the pipeline until it is exhausted or returns an error.
func collect(t *testing.T, pipeline Pipeline) (batches int64, rows int64) {
	for {
		err := pipeline.Read()
		if err == EOF {
			break
		}
		if err != nil {
			t.Fatalf("did not expect error, got %s", err.Error())
		}
		batch, _ := pipeline.Value()
		t.Log("batch", batch, "err", err)
		batches++
		rows += batch.NumRows()
	}
	return batches, rows
}
