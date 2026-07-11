package physicalpb

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/engine/internal/proto/testutils"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// The tests in this file guard against silent drift between the proto enums
// declared in physicalpb.pb.go, the corresponding types.* enums declared in
// pkg/engine/internal/types, and the lookup tables in marshal_types.go /
// unmarshal_types.go.
//
// Whenever a new value is added to either the proto enum or the types.* enum
// but the lookup tables are not updated, conversions silently fail at runtime.
// These tests catch that drift at build time.

func TestLookupCompleteness_AggregateRangeOp(t *testing.T) {
	testutils.CheckEnumLookup(t,
		"AggregateRangeOp",
		AggregateRangeOp_name,
		testutils.CountConstantsOfType(t, "../../types/aggregations.go", "RangeAggregationType"),
		nativeRangeAggregationLookup,
		protoAggregateRangeLookup,
		func(v int32) AggregateRangeOp { return AggregateRangeOp(v) },
		func(v AggregateRangeOp) (types.RangeAggregationType, error) { return v.marshalType() },
		func(v types.RangeAggregationType) (AggregateRangeOp, error) {
			var p AggregateRangeOp
			return p, p.unmarshalType(v)
		},
	)
}

func TestLookupCompleteness_AggregateVectorOp(t *testing.T) {
	testutils.CheckEnumLookup(t,
		"AggregateVectorOp",
		AggregateVectorOp_name,
		testutils.CountConstantsOfType(t, "../../types/aggregations.go", "VectorAggregationType"),
		nativeVectorAggregationLookup,
		protoAggregateVectorLookup,
		func(v int32) AggregateVectorOp { return AggregateVectorOp(v) },
		func(v AggregateVectorOp) (types.VectorAggregationType, error) { return v.marshalType() },
		func(v types.VectorAggregationType) (AggregateVectorOp, error) {
			var p AggregateVectorOp
			return p, p.unmarshalType(v)
		},
	)
}
