package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
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
func (b *Builder) Limit(skip uint64, fetch uint64) *Builder {
	return &Builder{
		val: &Limit{
			Table: b.val,
			Skip:  skip,
			Fetch: fetch,
		},
	}
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

// Schema returns the schema of the data that will be produced by this Builder.
func (b *Builder) Schema() *schema.Schema {
	return b.val.Schema()
}

// Value returns the underlying [Value]. This is useful when you need to access
// the value directly, such as when passing it to a function that operates on
// values rather than a Builder.
func (b *Builder) Value() Value { return b.val }

// ToPlan converts the Builder to a Plan.
func (b *Builder) ToPlan() (*Plan, error) {
	return convertToPlan(b.val)
}
