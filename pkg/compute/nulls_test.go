package compute_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestPropagateNulls(t *testing.T) {
	var alloc memory.Allocator

	input := columnartest.Array(t, types.KindInt64, &alloc, int64(1), nil, int64(3))
	validity := memory.NewBitmap(&alloc, 3)
	validity.AppendValues(true, true, false)

	actual, err := compute.PropagateNulls(&alloc, input, validity)
	require.NoError(t, err)
	expect := columnartest.Array(t, types.KindInt64, &alloc, int64(1), nil, nil)
	columnartest.RequireDatumsEqual(t, expect, actual, memory.Bitmap{})

	invalid := memory.NewBitmap(&alloc, 2)
	invalid.AppendValues(true, true)
	_, err = compute.PropagateNulls(&alloc, columnar.NewNumber([]int64{1, 2, 3}, memory.Bitmap{}), invalid)
	require.ErrorContains(t, err, "validity bitmap length mismatch")

	_, err = compute.PropagateNulls(&alloc, &columnar.NumberScalar[int64]{Value: 1}, memory.Bitmap{})
	require.ErrorContains(t, err, "requires an Array input")
}
