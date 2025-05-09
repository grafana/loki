package dataset

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// BoundsChecker defines the interface for checking if a value is within bounds
type BoundsChecker interface {
	// Check returns true if the value is within bounds, false otherwise
	Check(Value) bool
}

// BoundsCheckerFunc is an adapter to allow the use of ordinary functions as BoundsChecker
type BoundsCheckerFunc func(Value) bool

// Check implements BoundsChecker
func (f BoundsCheckerFunc) Check(v Value) bool {
	return f(v)
}

// ColumnBounds represents bounds for a column
type ColumnBounds struct {
	boundsChecker BoundsChecker
	column        Column
}

// Checker returns the BoundsChecker for a given column if it matches
// otherwise returns nil.
func (cb *ColumnBounds) Checker(c Column) BoundsChecker {
	if cb == nil || cb.column != c {
		return nil
	}

	return cb.boundsChecker
}

func BoundsForEqual(target Value, sortDirection datasetmd.SortDirection) BoundsChecker {
	if sortDirection == datasetmd.SORT_DIRECTION_ASCENDING {
		return BoundsCheckerFunc(func(v Value) bool {
			// Continue if v <= target.
			return CompareValues(v, target) <= 0
		})
	}

	return BoundsCheckerFunc(func(v Value) bool {
		// Continue as long as v >= target.
		return CompareValues(v, target) >= 0
	})
}

func BoundsForLessThan(target Value, sortDirection datasetmd.SortDirection) BoundsChecker {
	if sortDirection == datasetmd.SORT_DIRECTION_ASCENDING {
		return BoundsCheckerFunc(func(v Value) bool {
			// Continue if v < target.
			return CompareValues(v, target) < 0
		})
	}
	// bounds cannot be generated for descending order
	return nil
}

func BoundsForGreaterThan(target Value, sortDirection datasetmd.SortDirection) BoundsChecker {
	if sortDirection == datasetmd.SORT_DIRECTION_ASCENDING {
		// bounds cannot be generated for ascending order
		return nil
	}

	return BoundsCheckerFunc(func(v Value) bool {
		// Continue as long as v > target.
		return CompareValues(v, target) > 0
	})
}

// GenerateBoundsForColumn examines a predicate and generates a BoundFunc for the requested column.
func GenerateBoundsForColumn(p Predicate, targetColumn Column, sortDirection datasetmd.SortDirection) *ColumnBounds {
	if sortDirection == datasetmd.SORT_DIRECTION_UNSPECIFIED {
		return nil
	}

	switch p := p.(type) {
	case EqualPredicate:
		if p.Column == targetColumn {
			return &ColumnBounds{column: p.Column, boundsChecker: BoundsForEqual(p.Value, sortDirection)}
		}
	case LessThanPredicate:
		if p.Column == targetColumn {
			if f := BoundsForLessThan(p.Value, sortDirection); f != nil {
				return &ColumnBounds{column: p.Column, boundsChecker: f}
			}
		}

	case GreaterThanPredicate:
		if p.Column == targetColumn {
			if f := BoundsForGreaterThan(p.Value, sortDirection); f != nil {
				return &ColumnBounds{column: p.Column, boundsChecker: f}
			}
		}

	case InPredicate:
		if p.Column == targetColumn {
			boundaryValue := p.Values[0]
			if sortDirection == datasetmd.SORT_DIRECTION_ASCENDING {
				// Find maximum value for asc order
				for _, v := range p.Values[1:] {
					if CompareValues(v, boundaryValue) > 0 {
						boundaryValue = v
					}
				}
			} else {
				// Find min value for desc order
				for _, v := range p.Values[1:] {
					if CompareValues(v, boundaryValue) < 0 {
						boundaryValue = v
					}
				}
			}

			return &ColumnBounds{column: p.Column, boundsChecker: BoundsForEqual(boundaryValue, sortDirection)}
		}
	case AndPredicate:
		bfl := GenerateBoundsForColumn(p.Left, targetColumn, sortDirection)
		bfr := GenerateBoundsForColumn(p.Right, targetColumn, sortDirection)

		if bfl != nil && bfr != nil {
			// being conservative here as we do not have a good way to combine the two.
			return nil
		} else if bfl != nil {
			return bfl
		} else if bfr != nil {
			return bfr
		}
	case OrPredicate:
		bfl := GenerateBoundsForColumn(p.Left, targetColumn, sortDirection)
		bfr := GenerateBoundsForColumn(p.Right, targetColumn, sortDirection)

		if bfl != nil && bfr != nil {
			// being conservative here as we do not have a good way to combine the two.
			return nil
		} else if bfl != nil {
			return bfl
		} else if bfr != nil {
			return bfr
		}
	// As the initial use case is for a single column and it is going to be an InPredicate on stream_id column
	// some of the complex predicate implementations are either bring conservative or not generating bounds.
	// TODO: revisit this when we use sort order of multiple columns to generate bounds.
	case FalsePredicate, FuncPredicate, NotPredicate:
	default:
	}

	return nil
}
