package executor

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	incrementingIntPipeline = newRecordGenerator(
		arrow.NewSchema([]arrow.Field{
			semconv.FieldFromFQN("int64.builtin.id", false),
		}, nil),

		func(offset, maxRows, batchSize int64, schema *arrow.Schema) arrow.RecordBatch {
			builder := array.NewInt64Builder(memory.DefaultAllocator)

			rows := int64(0)
			for ; rows < batchSize && offset+rows < maxRows; rows++ {
				builder.Append(offset + rows)
			}

			data := builder.NewArray()

			columns := []arrow.Array{data}
			return array.NewRecordBatch(schema, columns, rows)
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
			semconv.FieldFromFQN("int64.builtin.id", false),
			semconv.FieldFromFQN("timestamp_ns.builtin.timestamp", false),
		}, nil),

		func(offset, maxRows, batchSize int64, schema *arrow.Schema) arrow.RecordBatch {
			idColBuilder := array.NewInt64Builder(memory.DefaultAllocator)
			tsColBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))

			rows := int64(0)
			for ; rows < batchSize && offset+rows < maxRows; rows++ {
				idColBuilder.Append(offset + rows)
				tsColBuilder.Append(arrow.Timestamp(start.Add(order * (time.Duration(offset)*time.Second + time.Duration(rows)*time.Millisecond)).UnixNano()))
			}

			idData := idColBuilder.NewArray()
			tsData := tsColBuilder.NewArray()

			columns := []arrow.Array{idData, tsData}
			return array.NewRecordBatch(schema, columns, rows)
		},
	)
}

type batchFunc func(offset, maxRows, batchSize int64, schema *arrow.Schema) arrow.RecordBatch

type recordGenerator struct {
	schema *arrow.Schema
	batch  batchFunc
}

func newRecordGenerator(schema *arrow.Schema, batch batchFunc) *recordGenerator {
	return &recordGenerator{
		schema: schema,
		batch:  batch,
	}
}

func (p *recordGenerator) Pipeline(batchSize int64, rows int64) Pipeline {
	var pos int64
	return newGenericPipeline(
		func(_ context.Context, _ []Pipeline) (arrow.RecordBatch, error) {
			if pos >= rows {
				return nil, EOF
			}
			batch := p.batch(pos, rows, batchSize, p.schema)
			pos += batch.NumRows()
			return batch, nil
		},
		nil,
	)
}

// collect reads all data from the pipeline until it is exhausted or returns an error.
func collect(t *testing.T, pipeline Pipeline) (batches int64, rows int64) {
	ctx := t.Context()
	for {
		batch, err := pipeline.Read(ctx)
		if errors.Is(err, EOF) {
			break
		}
		if err != nil {
			t.Fatalf("did not expect error, got %s", err.Error())
		}
		t.Log("batch", batch, "err", err)
		batches++
		rows += batch.NumRows()
	}
	return batches, rows
}

// ArrowtestPipeline creates a [Pipeline] that emits test data from a sequence
// of [arrowtest.Rows].
type ArrowtestPipeline struct {
	schema *arrow.Schema
	rows   []arrowtest.Rows

	cur int
}

var _ Pipeline = (*ArrowtestPipeline)(nil)

// NewArrowtestPipeline creates a new ArrowtestPipeline which will emit each
// [arrowtest.Rows] as a record.
//
// If schema is defined, all rows will be emitted using that schema. If schema
// is nil, the schema is derived from each element in rows as it is emitted.
func NewArrowtestPipeline(schema *arrow.Schema, rows ...arrowtest.Rows) *ArrowtestPipeline {
	return &ArrowtestPipeline{
		schema: schema,
		rows:   rows,
	}
}

// Read implements [Pipeline], converting the next [arrowtest.Rows] into a
// [arrow.RecordBatch] and storing it in the pipeline's state. The state can then be
// accessed via [ArrowtestPipeline.Value].
func (p *ArrowtestPipeline) Read(_ context.Context) (arrow.RecordBatch, error) {
	if p.cur >= len(p.rows) {
		return nil, EOF
	}

	rows := p.rows[p.cur]
	schema := p.schema

	if schema == nil {
		schema = rows.Schema()
	}

	p.cur++
	return rows.Record(memory.DefaultAllocator, schema), nil
}

// Close implements [Pipeline], immediately exhausting the pipeline.
func (p *ArrowtestPipeline) Close() { p.cur = math.MaxInt64 }
