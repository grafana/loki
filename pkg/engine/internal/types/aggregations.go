package types //nolint:revive

// RangeAggregationType represents the type of range aggregation operation
type RangeAggregationType int

const (
	RangeAggregationTypeInvalid RangeAggregationType = iota

	RangeAggregationTypeCount // Represents count_over_time range aggregation
	RangeAggregationTypeSum   // Represents sum_over_time range aggregation
	RangeAggregationTypeMax   // Represents max_over_time range aggregation
	RangeAggregationTypeMin   // Represents min_over_time range aggregation
	RangeAggregationTypeAvg   // Represents avg_over_time range aggregation
	RangeAggregationTypeBytes // Represents bytes_over_time range aggregation
)

func (op RangeAggregationType) String() string {
	switch op {
	case RangeAggregationTypeCount:
		return "count"
	case RangeAggregationTypeSum:
		return "sum"
	case RangeAggregationTypeMax:
		return "max"
	case RangeAggregationTypeMin:
		return "min"
	case RangeAggregationTypeAvg:
		return "avg"
	case RangeAggregationTypeBytes:
		return "bytes"
	default:
		return "invalid"
	}
}

// VectorAggregationType represents the type of vector aggregation operation
type VectorAggregationType int

const (
	VectorAggregationTypeInvalid VectorAggregationType = iota

	VectorAggregationTypeSum      // Represents sum vector aggregation
	VectorAggregationTypeMax      // Represents max vector aggregation
	VectorAggregationTypeMin      // Represents min vector aggregation
	VectorAggregationTypeCount    // Represents count vector aggregation
	VectorAggregationTypeAvg      // Represents avg vector aggregation
	VectorAggregationTypeStddev   // Represents stddev vector aggregation
	VectorAggregationTypeStdvar   // Represents stdvar vector aggregation
	VectorAggregationTypeBottomK  // Represents bottomk vector aggregation
	VectorAggregationTypeTopK     // Represents topk vector aggregation
	VectorAggregationTypeSort     // Represents sort vector aggregation
	VectorAggregationTypeSortDesc // Represents sort_desc vector aggregation
)

func (op VectorAggregationType) String() string {
	switch op {
	case VectorAggregationTypeSum:
		return "sum"
	case VectorAggregationTypeMax:
		return "max"
	case VectorAggregationTypeMin:
		return "min"
	case VectorAggregationTypeCount:
		return "count"
	case VectorAggregationTypeAvg:
		return "avg"
	case VectorAggregationTypeStddev:
		return "stddev"
	case VectorAggregationTypeStdvar:
		return "stdvar"
	case VectorAggregationTypeBottomK:
		return "bottomk"
	case VectorAggregationTypeTopK:
		return "topk"
	case VectorAggregationTypeSort:
		return "sort"
	case VectorAggregationTypeSortDesc:
		return "sort_desc"
	default:
		return "invalid"
	}
}
