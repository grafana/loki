package physicalpb

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	protoAggregateRangeLookup = map[types.RangeAggregationType]AggregateRangeOp{
		types.RangeAggregationTypeInvalid: AGGREGATE_RANGE_OP_INVALID,
		types.RangeAggregationTypeCount:   AGGREGATE_RANGE_OP_COUNT,
		types.RangeAggregationTypeSum:     AGGREGATE_RANGE_OP_SUM,
		types.RangeAggregationTypeMax:     AGGREGATE_RANGE_OP_MAX,
		types.RangeAggregationTypeMin:     AGGREGATE_RANGE_OP_MIN,
		types.RangeAggregationTypeBytes:   AGGREGATE_RANGE_OP_BYTES,
		types.RangeAggregationTypeAvg:     AGGREGATE_RANGE_OP_AVG,
	}

	protoAggregateVectorLookup = map[types.VectorAggregationType]AggregateVectorOp{
		types.VectorAggregationTypeInvalid:  AGGREGATE_VECTOR_OP_INVALID,
		types.VectorAggregationTypeSum:      AGGREGATE_VECTOR_OP_SUM,
		types.VectorAggregationTypeMax:      AGGREGATE_VECTOR_OP_MAX,
		types.VectorAggregationTypeMin:      AGGREGATE_VECTOR_OP_MIN,
		types.VectorAggregationTypeCount:    AGGREGATE_VECTOR_OP_COUNT,
		types.VectorAggregationTypeAvg:      AGGREGATE_VECTOR_OP_AVG,
		types.VectorAggregationTypeStddev:   AGGREGATE_VECTOR_OP_STDDEV,
		types.VectorAggregationTypeStdvar:   AGGREGATE_VECTOR_OP_STDVAR,
		types.VectorAggregationTypeBottomK:  AGGREGATE_VECTOR_OP_BOTTOMK,
		types.VectorAggregationTypeTopK:     AGGREGATE_VECTOR_OP_TOPK,
		types.VectorAggregationTypeSort:     AGGREGATE_VECTOR_OP_SORT,
		types.VectorAggregationTypeSortDesc: AGGREGATE_VECTOR_OP_SORT_DESC,
	}
)

func (op *AggregateRangeOp) unmarshalType(from types.RangeAggregationType) error {
	if result, ok := protoAggregateRangeLookup[from]; ok {
		*op = result
		return nil
	}
	return fmt.Errorf("unknown RangeAggregationType: %v", from)
}

func (op *AggregateVectorOp) unmarshalType(from types.VectorAggregationType) error {
	if result, ok := protoAggregateVectorLookup[from]; ok {
		*op = result
		return nil
	}
	return fmt.Errorf("unknown VectorAggregationType: %v", from)
}
