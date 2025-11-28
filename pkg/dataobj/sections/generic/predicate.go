package generic

import (
	"fmt"
	"sort"

	"github.com/apache/arrow-go/v18/arrow/scalar"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

// Predicate is an expression used to filter column values in a [Reader].
type Predicate interface{ isPredicate() }

// Supported predicates.
type (
	// An AndPredicate is a [Predicate] which asserts that a row may only be
	// included if both the Left and Right Predicate are true.
	AndPredicate struct{ Left, Right Predicate }

	// An OrPredicate is a [Predicate] which asserts that a row may only be
	// included if either the Left or Right Predicate are true.
	OrPredicate struct{ Left, Right Predicate }

	// A NotePredicate is a [Predicate] which asserts that a row may only be
	// included if the inner Predicate is false.
	NotPredicate struct{ Inner Predicate }

	// TruePredicate is a [Predicate] which always returns true.
	TruePredicate struct{}

	// FalsePredicate is a [Predicate] which always returns false.
	FalsePredicate struct{}

	// An EqualPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is equal to the Value.
	EqualPredicate struct {
		Column *Column       // Column to check.
		Value  scalar.Scalar // Value to check equality for.
	}

	// An InPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is present in the provided Values.
	InPredicate struct {
		Column *Column         // Column to check.
		Values []scalar.Scalar // Values to check for inclusion.
	}

	// A GreaterThanPredicate is a [Predicate] which asserts that a row may only
	// be included if the Value of the Column is greater than the provided Value.
	GreaterThanPredicate struct {
		Column *Column       // Column to check.
		Value  scalar.Scalar // Value for which rows in Column must be greater than.
	}

	// A LessThanPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column is less than the provided Value.
	LessThanPredicate struct {
		Column *Column       // Column to check.
		Value  scalar.Scalar // Value for which rows in Column must be less than.
	}

	// FuncPredicate is a [Predicate] which asserts that a row may only be
	// included if the Value of the Column passes the Keep function.
	//
	// Instances of FuncPredicate are ineligible for page filtering and should
	// only be used when there isn't a more explicit Predicate implementation.
	FuncPredicate struct {
		Column *Column // Column to check.

		// Keep is invoked with the column and value pair to check. Keep is given
		// the Column instance to allow for reusing the same function across
		// multiple columns, if necessary.
		//
		// If Keep returns true, the row is kept.
		Keep func(column *Column, value scalar.Scalar) bool
	}
)

func (AndPredicate) isPredicate()         {}
func (OrPredicate) isPredicate()          {}
func (NotPredicate) isPredicate()         {}
func (TruePredicate) isPredicate()        {}
func (FalsePredicate) isPredicate()       {}
func (EqualPredicate) isPredicate()       {}
func (InPredicate) isPredicate()          {}
func (GreaterThanPredicate) isPredicate() {}
func (LessThanPredicate) isPredicate()    {}
func (FuncPredicate) isPredicate()        {}

// walkPredicate traverses a predicate in depth-first order: it starts by
// calling fn(p). If fn(p) returns true, walkPredicate is invoked recursively
// with fn for each of the non-nil children of p, followed by a call of
// fn(nil).
func walkPredicate(p Predicate, fn func(Predicate) bool) {
	if p == nil || !fn(p) {
		return
	}

	switch p := p.(type) {
	case AndPredicate:
		walkPredicate(p.Left, fn)
		walkPredicate(p.Right, fn)

	case OrPredicate:
		walkPredicate(p.Left, fn)
		walkPredicate(p.Right, fn)

	case NotPredicate:
		walkPredicate(p.Inner, fn)

	case TruePredicate: // No children.
	case FalsePredicate: // No children.
	case EqualPredicate: // No children.
	case InPredicate: // No children.
	case GreaterThanPredicate: // No children.
	case LessThanPredicate: // No children.
	case FuncPredicate: // No children.

	default:
		panic("logs.walkPredicate: unsupported predicate type")
	}

	fn(nil)
}

// predicateColumns returns a slice of all columns referenced in the given predicates.
// It ensures that each column is only included once, even if it appears in multiple predicates.
func predicateColumns(predicates []Predicate) []*Column {
	exists := make(map[*Column]struct{})
	columns := make([]*Column, 0, len(predicates))

	// append column if it is not a duplicate.
	appendColumn := func(c *Column) {
		if _, ok := exists[c]; ok {
			return
		}

		columns = append(columns, c)
		exists[c] = struct{}{}
	}

	for _, p := range predicates {
		walkPredicate(p, func(p Predicate) bool {
			switch p := p.(type) {
			case nil: // End of walk; nothing to do.

			case AndPredicate: // Nothing to do.
			case OrPredicate: // Nothing to do.
			case NotPredicate: // Nothing to do.
			case TruePredicate: // Nothing to do.
			case FalsePredicate: // Nothing to do.

			case EqualPredicate:
				appendColumn(p.Column)
			case InPredicate:
				appendColumn(p.Column)
			case GreaterThanPredicate:
				appendColumn(p.Column)
			case LessThanPredicate:
				appendColumn(p.Column)
			case FuncPredicate:
				appendColumn(p.Column)

			default:
				panic(fmt.Sprintf("logs.predicateColumns: unsupported predicate type %T", p))
			}

			return true
		})
	}

	return columns
}

// selectivityScore represents how selective a predicate is expected to be.
// Lower scores mean more selective (fewer matching rows).
type selectivityScore float64

const (
	noMatchSelectivity  selectivityScore = 0.0 // No match
	matchAllSelectivity selectivityScore = 1.0 // Matches all rows
)

// orderPredicates orders the predicates based on their selectivity and cost as a simple heuristic.
// - Lower selectivity (more filtering) is better
// - Lower cost of processing a row is better
//
// TODO: a permutation-based approach would be more accurate as it would account
// for the cost of processing a predicate and how a predicate's selectivity affects
// the row count for subsequent predicates reducing their processing cost.
func orderPredicates(predicates []dataset.Predicate) []dataset.Predicate {
	if len(predicates) <= 1 {
		return predicates
	}

	sort.Slice(predicates, func(i, j int) bool {
		s1 := getPredicateSelectivity(predicates[i])
		s2 := getPredicateSelectivity(predicates[j])

		// for selectivity of 0, we can assume there is no cost to reading the column since the reader will prune the rows
		// return early in such cases to avoid cost calculations
		if s1 == 0 {
			return true
		} else if s2 == 0 {
			return false
		}

		return float64(s1)*float64(getRowEvaluationCost(predicates[i])) < float64(s2)*float64(getRowEvaluationCost(predicates[j]))
	})

	return predicates
}

// getPredicateSelectivity returns a selectivity score representing the estimated percentage
// of rows that will match (0.0 to 1.0). Lower scores mean more selective (fewer matching rows).
func getPredicateSelectivity(p dataset.Predicate) selectivityScore {
	if p == nil {
		return matchAllSelectivity
	}

	switch p := p.(type) {
	case dataset.EqualPredicate:
		info := p.Column.ColumnDesc()
		if info.Statistics == nil || info.Statistics.CardinalityCount == 0 {
			return getBaseSelectivity(p)
		}

		if info.Statistics.MinValue != nil && info.Statistics.MaxValue != nil {
			var minValue, maxValue dataset.Value
			if e1, e2 := minValue.UnmarshalBinary(info.Statistics.MinValue), maxValue.UnmarshalBinary(info.Statistics.MaxValue); e1 == nil && e2 == nil {
				if dataset.CompareValues(&p.Value, &minValue) < 0 || dataset.CompareValues(&p.Value, &maxValue) > 0 {
					// no rows will match
					return noMatchSelectivity
				}
			}
		}

		// For equality, estimate rows per unique value
		// selectivity = rows per unique value / total rows
		matchingRows := float64(info.ValuesCount) / float64(info.Statistics.CardinalityCount)
		return selectivityScore(matchingRows / float64(info.RowsCount))

	case dataset.InPredicate:
		info := p.Column.ColumnDesc()
		if info.Statistics == nil || info.Statistics.CardinalityCount == 0 {
			return getBaseSelectivity(p)
		}

		valuesInRange := p.Values.Size()
		if info.Statistics.MinValue != nil && info.Statistics.MaxValue != nil {
			var minValue, maxValue dataset.Value
			if e1, e2 := minValue.UnmarshalBinary(info.Statistics.MinValue), maxValue.UnmarshalBinary(info.Statistics.MaxValue); e1 == nil && e2 == nil {
				valuesInRange = 0

				for v := range p.Values.Iter() {
					if dataset.CompareValues(&v, &minValue) >= 0 && dataset.CompareValues(&v, &maxValue) <= 0 {
						valuesInRange++
					}
				}

				if valuesInRange == 0 {
					// no rows will match
					return noMatchSelectivity
				}
			}
		}

		// Similar to equality but for multiple values
		matchingRows := float64(info.ValuesCount) / float64(info.Statistics.CardinalityCount)
		estimatedRows := matchingRows * float64(valuesInRange)
		return selectivityScore(min(estimatedRows/float64(info.RowsCount), 1.0))

	case dataset.GreaterThanPredicate:
		var (
			info               = p.Column.ColumnDesc()
			minValue, maxValue dataset.Value
		)

		if info.Statistics == nil || info.Statistics.MinValue == nil || info.Statistics.MaxValue == nil {
			return getBaseSelectivity(p)
		}

		if e1, e2 := minValue.UnmarshalBinary(info.Statistics.MinValue), maxValue.UnmarshalBinary(info.Statistics.MaxValue); e1 == nil && e2 == nil {
			if dataset.CompareValues(&p.Value, &minValue) < 0 {
				return selectivityScore(1.0)
			}
			if dataset.CompareValues(&p.Value, &maxValue) > 0 {
				return selectivityScore(0.0)
			}

			// Estimate percentage of rows greater than the value assuming uniform distribution
			position := float64(p.Value.Int64()-minValue.Int64()) /
				float64(maxValue.Int64()-minValue.Int64())
			return selectivityScore(1.0 - position)
		}
		return getBaseSelectivity(p)

	case dataset.LessThanPredicate:
		var (
			info               = p.Column.ColumnDesc()
			minValue, maxValue dataset.Value
		)

		if info.Statistics == nil || info.Statistics.MinValue == nil || info.Statistics.MaxValue == nil {
			return getBaseSelectivity(p)
		}

		if e1, e2 := minValue.UnmarshalBinary(info.Statistics.MinValue), maxValue.UnmarshalBinary(info.Statistics.MaxValue); e1 == nil && e2 == nil {
			if dataset.CompareValues(&p.Value, &minValue) < 0 {
				return selectivityScore(0.0)
			}
			if dataset.CompareValues(&p.Value, &maxValue) > 0 {
				return selectivityScore(1.0)
			}

			// Estimate percentage of rows less than the value assuming uniform distribution
			position := float64(p.Value.Int64()-minValue.Int64()) /
				float64(maxValue.Int64()-minValue.Int64())
			return selectivityScore(position)
		}
		return getBaseSelectivity(p)

	case dataset.NotPredicate:
		s := getPredicateSelectivity(p.Inner)
		return selectivityScore(1 - float64(s)) // Invert selectivity for NOT predicates

	case dataset.AndPredicate:
		// In some best case scenarios, we could multify selectivities and return s1 * s2
		// However, predicates may be on the same or correlated columns,
		// which would make multiplication an overestimate. We conservatively use min(s1, s2) instead.
		return min(getPredicateSelectivity(p.Left), getPredicateSelectivity(p.Right))
	case dataset.OrPredicate:
		// Conservative estimate assuming there is no overlap between the returned rows
		return selectivityScore(min(getPredicateSelectivity(p.Left)+getPredicateSelectivity(p.Right), 1.0))

	case dataset.FuncPredicate:
		// For custom functions, we cannot use the stats to estimate selectivity or to prune the rows.
		// We might want these evaluated towards the end.
		return selectivityScore(0.7)
	default:
		panic("unknown predicate type")
	}
}

// getBaseSelectivity returns a conservative estimate when no stats are available
func getBaseSelectivity(p dataset.Predicate) selectivityScore {
	switch p.(type) {
	// equality predicates are preferred
	case dataset.EqualPredicate:
		return selectivityScore(0.1)
	case dataset.InPredicate:
		return selectivityScore(0.3)
	// range predicates are next
	case dataset.GreaterThanPredicate, dataset.LessThanPredicate:
		return selectivityScore(0.5)
	case dataset.FuncPredicate:
		return selectivityScore(0.7)
	default:
		return selectivityScore(1.0)
	}
}

// getRowEvaluationCost measures the cost of evaluating a row using the bytes that need to be processed.
func getRowEvaluationCost(p dataset.Predicate) int64 {
	if p == nil {
		return 0
	}

	// Use a map to track unique columns and their sizes
	columnSizes := make(map[dataset.Column]int64)
	dataset.WalkPredicate(p, func(p dataset.Predicate) bool {
		switch p := p.(type) {
		case dataset.EqualPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnDesc().UncompressedSize)
		case dataset.InPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnDesc().UncompressedSize)
		case dataset.GreaterThanPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnDesc().UncompressedSize)
		case dataset.LessThanPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnDesc().UncompressedSize)
		case dataset.FuncPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnDesc().UncompressedSize)
		}
		return true
	})

	// Sum up the sizes of unique columns
	totalSize := int64(0)
	for _, size := range columnSizes {
		totalSize += size
	}

	return totalSize
}
