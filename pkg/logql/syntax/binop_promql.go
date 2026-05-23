//go:build !js

package syntax

import (
	"github.com/prometheus/prometheus/promql"
)

// MergeBinOp performs `op` on `left` and `right` arguments and returns the
// `promql.Sample` value. In case of vector and scalar arguments, MergeBinOp
// assumes `left` is always vector. Pass `swap=true` otherwise.
//
// This matters because, either it's (vector op scalar) or (scalar op vector),
// the return sample value should always be the sample value of the vector
// argument. https://github.com/grafana/loki/issues/10741
//
// Build tag note: this file is `!js` because it imports prometheus/promql which
// drags in a large server-side dep chain (util/logging→k8s/client-go,
// util/stats→otel, tsdb/*). The analyzer's WASM build only needs the float
// arithmetic (mergeBinOpFloat in binop.go) for AST constant folding; the
// PromQL Sample identity-preserving semantics are server-side concerns.
func MergeBinOp(op string, left, right *promql.Sample, swap, filter, isVectorComparison bool) (*promql.Sample, error) {
	if left == nil || right == nil {
		return nil, nil
	}

	val, ok, err := mergeBinOpFloat(op, left.F, right.F, filter)
	if err != nil {
		return nil, err
	}

	// Build the arithmetic/comparison result Sample. ok=false means the
	// comparison failed with filter=true — translated to nil to match the
	// original behavior.
	var res *promql.Sample
	if ok {
		r := *left
		r.F = val
		res = &r
	}

	if !isVectorComparison {
		return res, nil
	}

	// Vector comparison with filter: when the predicate held, return the
	// vector-side operand's full identity (labels, timestamp), not the
	// {0,1} truthy result.
	if filter && res != nil {
		retSample := left
		if swap {
			retSample = right
		}
		return retSample, nil
	}
	return res, nil
}
