package types

// RangeAggregationType represents the type of range aggregation operation
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

// VectorAggregationType represents the type of vector aggregation operation
type VectorAggregationType int

const (
	VectorAggregationTypeInvalid VectorAggregationType = iota

	VectorAggregationTypeSum // Represents sum vector aggregation
)

func (op VectorAggregationType) String() string {
	switch op {
	case VectorAggregationTypeSum:
		return "sum"
	default:
		return "invalid"
	}
}
