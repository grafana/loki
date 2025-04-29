package dataset

import (
	"sort"
)

// SelectivityScore represents how selective a predicate is expected to be.
// Lower scores mean more selective (fewer matching rows).
type SelectivityScore float64

const (
	noMatchSelectivity  SelectivityScore = 0.0 // No match
	matchAllSelectivity SelectivityScore = 1.0 // Matches all rows
)

// OrderPredicates orders the predicates based on their selectivity and cost as a simple heuristic.
// - Lower selectivity (more filtering) is better
// - Lower cost of processing a row is better
//
// TODO: a permutation-based approach would be more accurate as it would account
// for the cost of processing a predicate and how a predicate's selectivity affects
// the row count for subsequent predicates reducing their processing cost.
func OrderPredicates(predicates []Predicate) []Predicate {
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
func getPredicateSelectivity(p Predicate) SelectivityScore {
	if p == nil {
		return matchAllSelectivity
	}

	switch p := p.(type) {
	case EqualPredicate:
		info := p.Column.ColumnInfo()
		if info.Statistics == nil || info.Statistics.CardinalityCount == 0 {
			return getBaseSelectivity(p)
		}

		if info.Statistics.MinValue != nil && info.Statistics.MaxValue != nil {
			var minValue, maxValue Value
			if e1, e2 := minValue.UnmarshalBinary(info.Statistics.MinValue), maxValue.UnmarshalBinary(info.Statistics.MaxValue); e1 == nil && e2 == nil {
				if CompareValues(p.Value, minValue) < 0 || CompareValues(p.Value, maxValue) > 0 {
					// no rows will match
					return noMatchSelectivity
				}
			}
		}

		// For equality, estimate rows per unique value
		// selectivity = rows per unique value / total rows
		matchingRows := float64(info.ValuesCount) / float64(info.Statistics.CardinalityCount)
		return SelectivityScore(matchingRows / float64(info.RowsCount))

	case InPredicate:
		info := p.Column.ColumnInfo()
		if info.Statistics == nil || info.Statistics.CardinalityCount == 0 {
			return getBaseSelectivity(p)
		}

		valuesInRange := len(p.Values)
		if info.Statistics.MinValue != nil && info.Statistics.MaxValue != nil {
			var minValue, maxValue Value
			if e1, e2 := minValue.UnmarshalBinary(info.Statistics.MinValue), maxValue.UnmarshalBinary(info.Statistics.MaxValue); e1 == nil && e2 == nil {
				valuesInRange = 0

				for _, v := range p.Values {
					if CompareValues(v, minValue) >= 0 && CompareValues(v, maxValue) <= 0 {
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
		return SelectivityScore(min(estimatedRows/float64(info.RowsCount), 1.0))

	case GreaterThanPredicate:
		var (
			info               = p.Column.ColumnInfo()
			minValue, maxValue Value
		)

		if info.Statistics == nil || info.Statistics.MinValue == nil || info.Statistics.MaxValue == nil {
			return getBaseSelectivity(p)
		}

		if e1, e2 := minValue.UnmarshalBinary(info.Statistics.MinValue), maxValue.UnmarshalBinary(info.Statistics.MaxValue); e1 == nil && e2 == nil {
			if CompareValues(p.Value, minValue) < 0 {
				return SelectivityScore(1.0)
			}
			if CompareValues(p.Value, maxValue) > 0 {
				return SelectivityScore(0.0)
			}

			// Estimate percentage of rows greater than the value assuming uniform distribution
			position := float64(p.Value.Int64()-minValue.Int64()) /
				float64(maxValue.Int64()-minValue.Int64())
			return SelectivityScore(1.0 - position)
		} else {
			return getBaseSelectivity(p)
		}

	case LessThanPredicate:
		var (
			info               = p.Column.ColumnInfo()
			minValue, maxValue Value
		)

		if info.Statistics == nil || info.Statistics.MinValue == nil || info.Statistics.MaxValue == nil {
			return getBaseSelectivity(p)
		}

		if e1, e2 := minValue.UnmarshalBinary(info.Statistics.MinValue), maxValue.UnmarshalBinary(info.Statistics.MaxValue); e1 == nil && e2 == nil {
			if CompareValues(p.Value, minValue) < 0 {
				return SelectivityScore(0.0)
			}
			if CompareValues(p.Value, maxValue) > 0 {
				return SelectivityScore(1.0)
			}

			// Estimate percentage of rows less than the value assuming uniform distribution
			position := float64(p.Value.Int64()-minValue.Int64()) /
				float64(maxValue.Int64()-minValue.Int64())
			return SelectivityScore(position)
		} else {
			return getBaseSelectivity(p)
		}

	case NotPredicate:
		s := getPredicateSelectivity(p.Inner)
		return SelectivityScore(1 - float64(s)) // Invert selectivity for NOT predicates

	case AndPredicate:
		// In some best case scenarios, we could multify selectivities and return s1 * s2
		// However, predicates may be on the same or correlated columns,
		// which would make multiplication an overestimate. We conservatively use min(s1, s2) instead.
		return min(getPredicateSelectivity(p.Left), getPredicateSelectivity(p.Right))
	case OrPredicate:
		// Conservative estimate assuming there is no overlap between the returned rows
		return SelectivityScore(min(getPredicateSelectivity(p.Left)+getPredicateSelectivity(p.Right), 1.0))

	case FuncPredicate:
		// For custom functions, we cannot use the stats to estimate selectivity or to prune the rows.
		// We might want these evaluated towards the end.
		return SelectivityScore(0.7)
	default:
		panic("unknown predicate type")
	}
}

// getBaseSelectivity returns a conservative estimate when no stats are available
func getBaseSelectivity(p Predicate) SelectivityScore {
	switch p.(type) {
	// equality predicates are preferred
	case EqualPredicate:
		return SelectivityScore(0.1)
	case InPredicate:
		return SelectivityScore(0.3)
	// range predicates are next
	case GreaterThanPredicate, LessThanPredicate:
		return SelectivityScore(0.5)
	case FuncPredicate:
		return SelectivityScore(0.7)
	default:
		return SelectivityScore(1.0)
	}
}

// getRowEvaluationCost measures the cost of evaluating a row using the bytes that need to be processed.
func getRowEvaluationCost(p Predicate) int64 {
	if p == nil {
		return 0
	}

	// Use a map to track unique columns and their sizes
	columnSizes := make(map[Column]int64)
	WalkPredicate(p, func(p Predicate) bool {
		switch p := p.(type) {
		case EqualPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnInfo().UncompressedSize)
		case InPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnInfo().UncompressedSize)
		case GreaterThanPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnInfo().UncompressedSize)
		case LessThanPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnInfo().UncompressedSize)
		case FuncPredicate:
			columnSizes[p.Column] = int64(p.Column.ColumnInfo().UncompressedSize)
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
