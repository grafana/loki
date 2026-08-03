package columnar_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestBroadcast(t *testing.T) {
	var alloc memory.Allocator

	tests := []struct {
		name   string
		scalar columnar.Scalar
		expect columnar.Array
	}{
		{"null", &columnar.NullScalar{}, columnartest.Array(t, types.KindNull, &alloc, nil, nil, nil)},
		{"bool", &columnar.BoolScalar{Value: true}, columnartest.Array(t, types.KindBool, &alloc, true, true, true)},
		{"int32", &columnar.NumberScalar[int32]{Value: 1}, columnartest.Array(t, types.KindInt32, &alloc, int32(1), int32(1), int32(1))},
		{"int64", &columnar.NumberScalar[int64]{Value: 2}, columnartest.Array(t, types.KindInt64, &alloc, int64(2), int64(2), int64(2))},
		{"uint32", &columnar.NumberScalar[uint32]{Value: 3}, columnartest.Array(t, types.KindUint32, &alloc, uint32(3), uint32(3), uint32(3))},
		{"uint64", &columnar.NumberScalar[uint64]{Value: 4}, columnartest.Array(t, types.KindUint64, &alloc, uint64(4), uint64(4), uint64(4))},
		{"utf8", &columnar.UTF8Scalar{Value: []byte("value")}, columnartest.Array(t, types.KindUTF8, &alloc, "value", "value", "value")},
		{"typed null", &columnar.NumberScalar[int64]{Null: true}, columnartest.Array(t, types.KindInt64, &alloc, nil, nil, nil)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := columnar.Broadcast(&alloc, test.scalar, 3)
			columnartest.RequireArraysEqual(t, test.expect, actual, memory.Bitmap{})
		})
	}

	t.Run("zero length", func(t *testing.T) {
		actual := columnar.Broadcast(&alloc, &columnar.UTF8Scalar{Value: []byte("value")}, 0)
		require.Zero(t, actual.Len())
	})
}
