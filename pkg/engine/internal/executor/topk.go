package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical/physicalpb"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

type topkOptions struct {
	// Input pipeliens to compute top K from.
	Inputs []Pipeline

	// SortBy is the list of columns to sort by, in order of precedence.
	SortBy     []*physicalpb.ColumnExpression
	Ascending  bool // Sorts lines in ascending order if true.
	NullsFirst bool // When true, considers NULLs < non-NULLs when sorting.
	K          int  // Number of top rows to compute.

	// MaxUnused determines the maximum number of unused rows to retain. An
	// unused row is any row from a retained record that does not contribute to
	// the current top K.
	//
	// After the number of unused rows exceeds this value, retained records are
	// compacted into a new record only containing the current used rows.
	MaxUnused int
}

// topkPipeline performs a topk (SORT + LIMIT) operation across several input
// pipelines.
type topkPipeline struct {
	inputs []Pipeline
	batch  *topkBatch

	computed bool
}

var _ Pipeline = (*topkPipeline)(nil)

// newTopkPipeline creates a new topkPipeline with the given options.
func newTopkPipeline(opts topkOptions) (*topkPipeline, error) {
	fields, err := exprsToFields(opts.SortBy)
	if err != nil {
		return nil, err
	}

	return &topkPipeline{
		inputs: opts.Inputs,
		batch: &topkBatch{
			Fields:     fields,
			Ascending:  opts.Ascending,
			NullsFirst: opts.NullsFirst,
			K:          opts.K,
			MaxUnused:  opts.MaxUnused,
		},
	}, nil
}

func exprsToFields(exprs []*physicalpb.ColumnExpression) ([]arrow.Field, error) {
	fields := make([]arrow.Field, 0, len(exprs))
	for _, expr := range exprs {
		dt, err := guessLokiType(expr)
		if err != nil {
			return nil, err
		}

		ident := semconv.NewIdentifier(expr.Name, expr.Type, dt)
		fields = append(fields, semconv.FieldFromIdent(ident, true))
	}
	return fields, nil
}

func guessLokiType(expr *physicalpb.ColumnExpression) (types.DataType, error) {
	switch expr.Type {
	case physicalpb.COLUMN_TYPE_BUILTIN:
		switch expr.Name {
		case types.ColumnNameBuiltinTimestamp:
			return types.Loki.Timestamp, nil
		case types.ColumnNameBuiltinMessage:
			return types.Loki.String, nil
		default:
			panic(fmt.Sprintf("unsupported builtin column type %s", expr))
		}
	case physicalpb.COLUMN_TYPE_GENERATED:
		return types.Loki.Float, nil
	case physicalpb.COLUMN_TYPE_AMBIGUOUS:
		// TODO(rfratto): It's not clear how topk should sort when there's an
		// ambiguous column reference, since ambiguous column references can
		// refer to multiple columns.
		return nil, fmt.Errorf("topkPipeline does not support ambiguous column types")
	default:
		return types.Loki.String, nil
	}
}

// Read computes the topk as the next record. Read blocks until all input
// pipelines have been fully read and the top K rows have been computed.
func (p *topkPipeline) Read(ctx context.Context) (arrow.Record, error) {
	if !p.computed {
		rec, err := p.compute(ctx)
		p.computed = true
		return rec, err
	}
	return nil, EOF
}

func (p *topkPipeline) compute(ctx context.Context) (arrow.Record, error) {
NextInput:
	for _, in := range p.Inputs() {
		for {
			rec, err := in.Read(ctx)
			if err != nil && errors.Is(err, EOF) {
				continue NextInput
			} else if err != nil {
				return nil, err
			}

			p.batch.Put(memory.DefaultAllocator, rec)

			// Release the record; p.batch.Put will add an extra retain if necessary.
			rec.Release()
		}
	}

	compacted := p.batch.Compact(memory.DefaultAllocator)
	if compacted == nil {
		return nil, EOF
	}
	return compacted, nil
}

// Close closes the resources of the pipeline.
func (p *topkPipeline) Close() {
	p.batch.Reset()
	for _, in := range p.inputs {
		in.Close()
	}
}

func (p *topkPipeline) Inputs() []Pipeline { return p.inputs }

func (p *topkPipeline) Transport() Transport { return Local }
