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

	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

var (
	incrementingIntPipeline = newRecordGenerator(
		arrow.NewSchema([]arrow.Field{
			{Name: "id", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
		}, nil),

		func(offset, maxRows, batchSize int64, schema *arrow.Schema) arrow.Record {
			builder := array.NewInt64Builder(memory.DefaultAllocator)
			defer builder.Release()

			rows := int64(0)
			for ; rows < batchSize && offset+rows < maxRows; rows++ {
				builder.Append(offset + rows)
			}

			data := builder.NewArray()
			defer data.Release()

			columns := []arrow.Array{data}
			return array.NewRecord(schema, columns, rows)
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
			{Name: "id", Type: datatype.Arrow.Integer, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Integer)},
			{Name: "timestamp", Type: datatype.Arrow.Timestamp, Metadata: datatype.ColumnMetadata(types.ColumnTypeBuiltin, datatype.Loki.Timestamp)},
		}, nil),

		func(offset, maxRows, batchSize int64, schema *arrow.Schema) arrow.Record {
			idColBuilder := array.NewInt64Builder(memory.DefaultAllocator)
			defer idColBuilder.Release()

			tsColBuilder := array.NewTimestampBuilder(memory.DefaultAllocator, arrow.FixedWidthTypes.Timestamp_ns.(*arrow.TimestampType))
			defer tsColBuilder.Release()

			rows := int64(0)
			for ; rows < batchSize && offset+rows < maxRows; rows++ {
				idColBuilder.Append(offset + rows)
				tsColBuilder.Append(arrow.Timestamp(start.Add(order * (time.Duration(offset)*time.Second + time.Duration(rows)*time.Millisecond)).UnixNano()))
			}

			idData := idColBuilder.NewArray()
			defer idData.Release()

			tsData := tsColBuilder.NewArray()
			defer tsData.Release()

			columns := []arrow.Array{idData, tsData}
			return array.NewRecord(schema, columns, rows)
		},
	)
}

type batchFunc func(offset, maxRows, batchSize int64, schema *arrow.Schema) arrow.Record

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
		Local,
		func(_ context.Context, _ []Pipeline) state {
			if pos >= rows {
				return Exhausted
			}
			batch := p.batch(pos, rows, batchSize, p.schema)
			pos += batch.NumRows()
			return successState(batch)
		},
		nil,
	)
}

// collect reads all data from the pipeline until it is exhausted or returns an error.
func collect(t *testing.T, pipeline Pipeline) (batches int64, rows int64) {
	ctx := t.Context()
	for {
		err := pipeline.Read(ctx)
		if errors.Is(err, EOF) {
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

// ArrowtestPipeline creates a [Pipeline] that emits test data from a sequence
// of [arrowtest.Rows].
type ArrowtestPipeline struct {
	alloc  memory.Allocator
	schema *arrow.Schema
	rows   []arrowtest.Rows

	cur   int
	state state
}

var _ Pipeline = (*ArrowtestPipeline)(nil)

// NewArrowtestPipeline creates a new ArrowtestPipeline which will emit each
// [arrowtest.Rows] as a record.
//
// If schema is defined, all rows will be emitted using that schema. If schema
// is nil, the schema is derived from each element in rows as it is emitted.
func NewArrowtestPipeline(alloc memory.Allocator, schema *arrow.Schema, rows ...arrowtest.Rows) *ArrowtestPipeline {
	if alloc == nil {
		alloc = memory.DefaultAllocator
	}

	return &ArrowtestPipeline{
		alloc:  alloc,
		schema: schema,
		rows:   rows,
	}
}

// Read implements [Pipeline], converting the next [arrowtest.Rows] into a
// [arrow.Record] and storing it in the pipeline's state. The state can then be
// accessed via [ArrowtestPipeline.Value].
func (p *ArrowtestPipeline) Read(_ context.Context) error {
	if p.cur >= len(p.rows) {
		p.state = Exhausted
		return EOF
	}

	rows := p.rows[p.cur]
	schema := p.schema

	if schema == nil {
		schema = rows.Schema()
	}

	p.cur++
	p.state.batch, p.state.err = rows.Record(p.alloc, schema), nil
	return p.state.err
}

// Value implements [Pipeline], returning the current record and error
// determined by the latest call to [ArrowtestPipeline.Read].
func (p *ArrowtestPipeline) Value() (arrow.Record, error) { return p.state.Value() }

// Close implements [Pipeline], immediately exhausting the pipeline.
func (p *ArrowtestPipeline) Close() { p.cur = math.MaxInt64 }

// Inputs implements [Pipeline], returning nil as this pipeline has no inputs.
func (p *ArrowtestPipeline) Inputs() []Pipeline { return nil }

// Transport implements [Pipeline], returning [Local].
func (p *ArrowtestPipeline) Transport() Transport { return Local }
