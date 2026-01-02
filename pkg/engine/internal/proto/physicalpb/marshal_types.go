package physicalpb

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var (
	nativeRangeAggregationLookup = map[AggregateRangeOp]types.RangeAggregationType{
		AGGREGATE_RANGE_OP_INVALID: types.RangeAggregationTypeInvalid,
		AGGREGATE_RANGE_OP_COUNT:   types.RangeAggregationTypeCount,
		AGGREGATE_RANGE_OP_SUM:     types.RangeAggregationTypeSum,
		AGGREGATE_RANGE_OP_MAX:     types.RangeAggregationTypeMax,
		AGGREGATE_RANGE_OP_MIN:     types.RangeAggregationTypeMin,
		AGGREGATE_RANGE_OP_BYTES:   types.RangeAggregationTypeBytes,
	}

	nativeVectorAggregationLookup = map[AggregateVectorOp]types.VectorAggregationType{
		AGGREGATE_VECTOR_OP_INVALID:   types.VectorAggregationTypeInvalid,
		AGGREGATE_VECTOR_OP_SUM:       types.VectorAggregationTypeSum,
		AGGREGATE_VECTOR_OP_MAX:       types.VectorAggregationTypeMax,
		AGGREGATE_VECTOR_OP_MIN:       types.VectorAggregationTypeMin,
		AGGREGATE_VECTOR_OP_COUNT:     types.VectorAggregationTypeCount,
		AGGREGATE_VECTOR_OP_AVG:       types.VectorAggregationTypeAvg,
		AGGREGATE_VECTOR_OP_STDDEV:    types.VectorAggregationTypeStddev,
		AGGREGATE_VECTOR_OP_STDVAR:    types.VectorAggregationTypeStdvar,
		AGGREGATE_VECTOR_OP_BOTTOMK:   types.VectorAggregationTypeBottomK,
		AGGREGATE_VECTOR_OP_TOPK:      types.VectorAggregationTypeTopK,
		AGGREGATE_VECTOR_OP_SORT:      types.VectorAggregationTypeSort,
		AGGREGATE_VECTOR_OP_SORT_DESC: types.VectorAggregationTypeSortDesc,
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
