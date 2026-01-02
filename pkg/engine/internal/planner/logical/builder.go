package logical

import (
	"time"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// Builder provides an ergonomic interface for constructing a [Plan].
type Builder struct {
	val Value
}

// NewBuilder creates a new Builder from a Value, where the starting Value is
// usually a [MakeTable].
func NewBuilder(val Value) *Builder {
	return &Builder{val: val}
}

// Select applies a [Select] operation to the Builder.
func (b *Builder) Select(predicate Value) *Builder {
	return &Builder{
		val: &Select{
			Table:     b.val,
			Predicate: predicate,
		},
	}
}

// Limit applies a [Limit] operation to the Builder.
func (b *Builder) Limit(skip uint32, fetch uint32) *Builder {
	return &Builder{
		val: &Limit{
			Table: b.val,
			Skip:  skip,
			Fetch: fetch,
		},
	}
}

// Parse applies a [Parse] operation to the Builder.
func (b *Builder) Parse(op types.VariadicOp, strict bool, keepEmpty bool) *Builder {
	val := &FunctionOp{
		Op: op,
		Values: []Value{
			// source column
			&ColumnRef{
				Ref: semconv.ColumnIdentMessage.ColumnRef(),
			},
			// nil for requested keys (to be filled in by projection pushdown optimizer)
			NewLiteral([]string{}),
			NewLiteral(strict),
			NewLiteral(keepEmpty),
		},
	}
	return b.ProjectExpand(val)
}

// Cast applies an [Projection] operation, with an [UnaryOp] cast operation, to the Builder.
func (b *Builder) Cast(identifier string, op types.UnaryOp) *Builder {
	val := &UnaryOp{
		Op: op,
		Value: &ColumnRef{
			Ref: types.ColumnRef{
				Column: identifier,
				Type:   types.ColumnTypeAmbiguous,
			},
		},
	}
	return b.ProjectExpand(val)
}

// Sort applies a [Sort] operation to the Builder.
func (b *Builder) Sort(column ColumnRef, ascending, nullsFirst bool) *Builder {
	return &Builder{
		val: &Sort{
			Table: b.val,

			Column:     column,
			Ascending:  ascending,
			NullsFirst: nullsFirst,
		},
	}
}

// TopK applies a [TopK] operation to the Builder.
func (b *Builder) TopK(sortBy *ColumnRef, K int, ascending, nullsFirst bool) *Builder {
	return &Builder{
		val: &TopK{
			Table: b.val,

			SortBy:     sortBy,
			Ascending:  ascending,
			NullsFirst: nullsFirst,
			K:          K,
		},
	}
}

// BinOpRight adds a binary arithmetic operation with a given right value
func (b *Builder) BinOpRight(op types.BinaryOp, right Value) *Builder {
	return &Builder{
		val: &BinOp{
			Left:  b.val,
			Right: right,
			Op:    op,
		},
	}
}

// BinOpLeft adds a binary arithmetic operation with a given left value
func (b *Builder) BinOpLeft(op types.BinaryOp, left Value) *Builder {
	return &Builder{
		val: &BinOp{
			Left:  left,
			Right: b.val,
			Op:    op,
		},
	}
}

// RangeAggregation applies a [RangeAggregation] operation to the Builder.
func (b *Builder) RangeAggregation(
	grouping Grouping,
	operation types.RangeAggregationType,
	startTS, endTS time.Time,
	step time.Duration,
	rangeInterval time.Duration,
) *Builder {
	return &Builder{
		val: &RangeAggregation{
			Table: b.val,

			Operation:     operation,
			Grouping:      grouping,
			Start:         startTS,
			End:           endTS,
			Step:          step,
			RangeInterval: rangeInterval,
		},
	}
}

// VectorAggregation applies a [VectorAggregation] operation to the Builder.
func (b *Builder) VectorAggregation(
	grouping Grouping,
	operation types.VectorAggregationType,
) *Builder {
	return &Builder{
		val: &VectorAggregation{
			Table:     b.val,
			Grouping:  grouping,
			Operation: operation,
		},
	}
}

// Compat applies a [LogQLCompat] operation to the Builder, which is a marker to ensure v1 engine compatible results.
func (b *Builder) Compat(logqlCompatibility bool) *Builder {
	if logqlCompatibility {
		return &Builder{
			val: &LogQLCompat{
				Value: b.val,
			},
		}
	}
	return b
}

func (b *Builder) Project(all, expand, drop bool, expr ...Value) *Builder {
	return &Builder{
		val: &Projection{
			Relation:    b.val,
			All:         all,
			Expand:      expand,
			Drop:        drop,
			Expressions: expr,
		},
	}
}

func (b *Builder) ProjectAll(expand, drop bool, expr ...Value) *Builder {
	return b.Project(true, expand, drop, expr...)
}

func (b *Builder) ProjectDrop(expr ...Value) *Builder {
	return b.ProjectAll(false, true, expr...)
}

func (b *Builder) ProjectExpand(expr ...Value) *Builder {
	return b.ProjectAll(true, false, expr...)
}

// Value returns the underlying [Value]. This is useful when you need to access
// the value directly, such as when passing it to a function that operates on
// values rather than a Builder.
func (b *Builder) Value() Value { return b.val }

// ToPlan converts the Builder to a Plan.
func (b *Builder) ToPlan() (*Plan, error) {
	return convertToPlan(b.val)
}
