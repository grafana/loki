package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// DataFrame provides an ergonomic builder-like interface for constructing a
// [Plan].
type DataFrame struct {
	val Value
}

// NewDataFrame creates a new DataFrame from a Value, where the starting Value
// is usually a [MakeTable].
func NewDataFrame(val Value) *DataFrame {
	return &DataFrame{val: val}
}

// Select applies a [Select] operation to the DataFrame.
func (df *DataFrame) Select(predicate Value) *DataFrame {
	return &DataFrame{
		val: &Select{
			Table:     df.val,
			Predicate: predicate,
		},
	}
}

// Limit applies a [Limit] operation to the DataFrame.
func (df *DataFrame) Limit(skip uint64, fetch uint64) *DataFrame {
	return &DataFrame{
		val: &Limit{
			Table: df.val,
			Skip:  skip,
			Fetch: fetch,
		},
	}
}

// Sort applies a [Sort] operation to the DataFrame.
func (df *DataFrame) Sort(column ColumnRef, ascending, nullsFirst bool) *DataFrame {
	return &DataFrame{
		val: &Sort{
			Table: df.val,

			Column:     column,
			Ascending:  ascending,
			NullsFirst: nullsFirst,
		},
	}
}

// Schema returns the schema of the data that will be produced by this
// DataFrame.
func (df *DataFrame) Schema() *schema.Schema {
	return df.val.Schema()
}

// Value returns the underlying [Value]. This is useful when you need to access
// the value directly, such as when passing it to a function that operates on
// values rather than DataFrames.
func (df *DataFrame) Value() Value { return df.val }

// ToPlan converts the DataFrame to a Plan.
func (df *DataFrame) ToPlan() (*Plan, error) {
	return convertToPlan(df.val)
}
