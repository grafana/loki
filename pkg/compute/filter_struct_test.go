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

func TestFilter_Struct(t *testing.T) {
	var alloc memory.Allocator

	schema := columnar.NewSchema([]columnar.Column{{Name: "x"}, {Name: "y"}})
	input := columnar.NewStruct(schema, []columnar.Array{
		columnartest.Array(t, types.KindInt64, &alloc, int64(1), int64(2), int64(3)),
		columnartest.Array(t, types.KindUTF8, &alloc, "a", "b", "c"),
	}, 3, memory.Bitmap{})

	mask := memory.NewBitmap(&alloc, 3)
	mask.AppendValues(true, false, true)

	result, err := compute.Filter(&alloc, input, mask)
	require.NoError(t, err)

	rs := result.(*columnar.Struct)
	require.Equal(t, 2, rs.Len())
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(1), int64(3)),
		rs.Field(0), memory.Bitmap{},
	)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindUTF8, &alloc, "a", "c"),
		rs.Field(1), memory.Bitmap{},
	)
}

func TestFilter_Struct_WithValidity(t *testing.T) {
	var alloc memory.Allocator

	schema := columnar.NewSchema([]columnar.Column{{Name: "x"}})
	validity := memory.NewBitmap(&alloc, 3)
	validity.AppendValues(true, false, true)
	input := columnar.NewStruct(schema, []columnar.Array{
		columnartest.Array(t, types.KindInt64, &alloc, int64(1), int64(0), int64(3)),
	}, 3, validity)

	mask := memory.NewBitmap(&alloc, 3)
	mask.AppendValues(true, true, false)

	result, err := compute.Filter(&alloc, input, mask)
	require.NoError(t, err)

	rs := result.(*columnar.Struct)
	require.Equal(t, 2, rs.Len())
	require.Equal(t, 1, rs.Nulls())
	require.False(t, rs.IsNull(0))
	require.True(t, rs.IsNull(1))
}

func TestFilter_Struct_EmptyMask(t *testing.T) {
	var alloc memory.Allocator

	schema := columnar.NewSchema([]columnar.Column{{Name: "x"}})
	input := columnar.NewStruct(schema, []columnar.Array{
		columnartest.Array(t, types.KindInt64, &alloc, int64(1), int64(2)),
	}, 2, memory.Bitmap{})

	result, err := compute.Filter(&alloc, input, memory.Bitmap{})
	require.NoError(t, err)
	require.Same(t, input, result)
}
