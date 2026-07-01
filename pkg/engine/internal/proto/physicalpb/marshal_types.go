package physicalpb

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	nativeRangeAggregationLookup = map[AggregateRangeOp]types.RangeAggregationType{
		AggregateRangeOp_AGGREGATE_RANGE_OP_INVALID: types.RangeAggregationTypeInvalid,
		AggregateRangeOp_AGGREGATE_RANGE_OP_COUNT:   types.RangeAggregationTypeCount,
		AggregateRangeOp_AGGREGATE_RANGE_OP_SUM:     types.RangeAggregationTypeSum,
		AggregateRangeOp_AGGREGATE_RANGE_OP_MAX:     types.RangeAggregationTypeMax,
		AggregateRangeOp_AGGREGATE_RANGE_OP_MIN:     types.RangeAggregationTypeMin,
		AggregateRangeOp_AGGREGATE_RANGE_OP_BYTES:   types.RangeAggregationTypeBytes,
		AggregateRangeOp_AGGREGATE_RANGE_OP_AVG:     types.RangeAggregationTypeAvg,
	}

	nativeVectorAggregationLookup = map[AggregateVectorOp]types.VectorAggregationType{
		AggregateVectorOp_AGGREGATE_VECTOR_OP_INVALID:   types.VectorAggregationTypeInvalid,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_SUM:       types.VectorAggregationTypeSum,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_MAX:       types.VectorAggregationTypeMax,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_MIN:       types.VectorAggregationTypeMin,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_COUNT:     types.VectorAggregationTypeCount,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_AVG:       types.VectorAggregationTypeAvg,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_STDDEV:    types.VectorAggregationTypeStddev,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_STDVAR:    types.VectorAggregationTypeStdvar,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_BOTTOMK:   types.VectorAggregationTypeBottomK,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_TOPK:      types.VectorAggregationTypeTopK,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_SORT:      types.VectorAggregationTypeSort,
		AggregateVectorOp_AGGREGATE_VECTOR_OP_SORT_DESC: types.VectorAggregationTypeSortDesc,
	}
)

func (op AggregateRangeOp) marshalType() (types.RangeAggregationType, error) {
	if result, ok := nativeRangeAggregationLookup[op]; ok {
		return result, nil
	}
	return types.RangeAggregationTypeInvalid, fmt.Errorf("unknown AggregateRangeOp: %v", op)
}

func (op AggregateVectorOp) marshalType() (types.VectorAggregationType, error) {
	if result, ok := nativeVectorAggregationLookup[op]; ok {
		return result, nil
	}
	return types.VectorAggregationTypeInvalid, fmt.Errorf("unknown AggregateVectorOp: %v", op)
}
