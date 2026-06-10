package physicalpb

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	protoAggregateRangeLookup = map[types.RangeAggregationType]AggregateRangeOp{
		types.RangeAggregationTypeInvalid: AggregateRangeOp_AGGREGATE_RANGE_OP_INVALID,
		types.RangeAggregationTypeCount:   AggregateRangeOp_AGGREGATE_RANGE_OP_COUNT,
		types.RangeAggregationTypeSum:     AggregateRangeOp_AGGREGATE_RANGE_OP_SUM,
		types.RangeAggregationTypeMax:     AggregateRangeOp_AGGREGATE_RANGE_OP_MAX,
		types.RangeAggregationTypeMin:     AggregateRangeOp_AGGREGATE_RANGE_OP_MIN,
		types.RangeAggregationTypeBytes:   AggregateRangeOp_AGGREGATE_RANGE_OP_BYTES,
		types.RangeAggregationTypeAvg:     AggregateRangeOp_AGGREGATE_RANGE_OP_AVG,
	}

	protoAggregateVectorLookup = map[types.VectorAggregationType]AggregateVectorOp{
		types.VectorAggregationTypeInvalid:  AggregateVectorOp_AGGREGATE_VECTOR_OP_INVALID,
		types.VectorAggregationTypeSum:      AggregateVectorOp_AGGREGATE_VECTOR_OP_SUM,
		types.VectorAggregationTypeMax:      AggregateVectorOp_AGGREGATE_VECTOR_OP_MAX,
		types.VectorAggregationTypeMin:      AggregateVectorOp_AGGREGATE_VECTOR_OP_MIN,
		types.VectorAggregationTypeCount:    AggregateVectorOp_AGGREGATE_VECTOR_OP_COUNT,
		types.VectorAggregationTypeAvg:      AggregateVectorOp_AGGREGATE_VECTOR_OP_AVG,
		types.VectorAggregationTypeStddev:   AggregateVectorOp_AGGREGATE_VECTOR_OP_STDDEV,
		types.VectorAggregationTypeStdvar:   AggregateVectorOp_AGGREGATE_VECTOR_OP_STDVAR,
		types.VectorAggregationTypeBottomK:  AggregateVectorOp_AGGREGATE_VECTOR_OP_BOTTOMK,
		types.VectorAggregationTypeTopK:     AggregateVectorOp_AGGREGATE_VECTOR_OP_TOPK,
		types.VectorAggregationTypeSort:     AggregateVectorOp_AGGREGATE_VECTOR_OP_SORT,
		types.VectorAggregationTypeSortDesc: AggregateVectorOp_AGGREGATE_VECTOR_OP_SORT_DESC,
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
