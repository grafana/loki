package syntax

import (
	"math"

	"github.com/pkg/errors"
)

// mergeBinOpFloat performs `op` on two float64 operands. This is the parser-side
// core used for constant folding (reduceBinOp); the server-side MergeBinOp wraps
// this for promql.Sample arithmetic. Keeping the float path here lets the parser
// run on js/wasm without importing prometheus/prometheus/promql, which would
// pull in a large server-only dep chain (util/logging→k8s, util/stats→otel, etc).
//
// Returns (value, ok). When op is a comparison operator and the comparison is
// false, ok is false only if filter is true — matching the original MergeBinOp
// semantics of "comparison-with-filter returns nil when the predicate fails."
// For arithmetic ops ok is always true.
func mergeBinOpFloat(op string, left, right float64, filter bool) (float64, bool, error) {
	switch op {
	case OpTypeAdd:
		return left + right, true, nil
	case OpTypeSub:
		return left - right, true, nil
	case OpTypeMul:
		return left * right, true, nil
	case OpTypeDiv:
		if right == 0 {
			return math.NaN(), true, nil
		}
		return left / right, true, nil
	case OpTypeMod:
		if right == 0 {
			return math.NaN(), true, nil
		}
		return math.Mod(left, right), true, nil
	case OpTypePow:
		return math.Pow(left, right), true, nil
	case OpTypeCmpEQ:
		if left == right {
			return 1, true, nil
		}
		if filter {
			return 0, false, nil
		}
		return 0, true, nil
	case OpTypeNEQ:
		if left != right {
			return 1, true, nil
		}
		if filter {
			return 0, false, nil
		}
		return 0, true, nil
	case OpTypeGT:
		if left > right {
			return 1, true, nil
		}
		if filter {
			return 0, false, nil
		}
		return 0, true, nil
	case OpTypeGTE:
		if left >= right {
			return 1, true, nil
		}
		if filter {
			return 0, false, nil
		}
		return 0, true, nil
	case OpTypeLT:
		if left < right {
			return 1, true, nil
		}
		if filter {
			return 0, false, nil
		}
		return 0, true, nil
	case OpTypeLTE:
		if left <= right {
			return 1, true, nil
		}
		if filter {
			return 0, false, nil
		}
		return 0, true, nil
	}
	return 0, false, errors.Errorf("should never happen: unexpected operation: (%s)", op)
}
