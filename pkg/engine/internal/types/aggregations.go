package types

type RangeAggregationType int

const (
	RangeAggregationTypeInvalid RangeAggregationType = iota

	RangeAggregationTypeCount // Represents count_over_time range aggregation
)

func (op RangeAggregationType) String() string {
	switch op {
	case RangeAggregationTypeCount:
		return "count"
	default:
		return "invalid"
	}
}
