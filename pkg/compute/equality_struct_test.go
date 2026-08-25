package compute_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestEquals_Struct(t *testing.T) {
	var alloc memory.Allocator

	left := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2), int64(3)),
		columnartest.Field("y", types.KindUTF8, "a", "b", "c"),
	)
	right := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(99), int64(3)),
		columnartest.Field("y", types.KindUTF8, "a", "b", "z"),
	)

	actual, err := compute.Equals(&alloc, left, right, memory.Bitmap{})
	require.NoError(t, err)

	expect := columnartest.Array(t, types.KindBool, &alloc, true, false, false)
	columnartest.RequireDatumsEqual(t, expect, actual, memory.Bitmap{})
}

func TestNotEquals_Struct(t *testing.T) {
	var alloc memory.Allocator

	left := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2)),
	)
	right := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(99)),
	)

	actual, err := compute.NotEquals(&alloc, left, right, memory.Bitmap{})
	require.NoError(t, err)

	expect := columnartest.Array(t, types.KindBool, &alloc, false, true)
	columnartest.RequireDatumsEqual(t, expect, actual, memory.Bitmap{})
}

func TestEquals_Struct_WithNulls(t *testing.T) {
	var alloc memory.Allocator

	left := columnartest.StructWithValidity(t, &alloc, []bool{true, false},
		columnartest.Field("x", types.KindInt64, int64(1), int64(0)),
	)
	right := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(0)),
	)

	actual, err := compute.Equals(&alloc, left, right, memory.Bitmap{})
	require.NoError(t, err)

	expect := columnartest.Array(t, types.KindBool, &alloc, true, nil)
	columnartest.RequireDatumsEqual(t, expect, actual, memory.Bitmap{})
}

func TestEquals_Struct_WithFieldNulls(t *testing.T) {
	var alloc memory.Allocator

	left := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), nil),
	)
	right := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2)),
	)

	actual, err := compute.Equals(&alloc, left, right, memory.Bitmap{})
	require.NoError(t, err)

	expect := columnartest.Array(t, types.KindBool, &alloc, true, nil)
	columnartest.RequireDatumsEqual(t, expect, actual, memory.Bitmap{})
}

func TestEquals_Struct_NameMismatch(t *testing.T) {
	var alloc memory.Allocator

	left := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1)),
		columnartest.Field("y", types.KindInt64, int64(2)),
	)
	right := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1)),
		columnartest.Field("z", types.KindInt64, int64(2)),
	)

	_, err := compute.Equals(&alloc, left, right, memory.Bitmap{})
	require.EqualError(t, err, `field 1 name mismatch: "y" != "z"`)

	_, err = compute.NotEquals(&alloc, left, right, memory.Bitmap{})
	require.EqualError(t, err, `field 1 name mismatch: "y" != "z"`)
}

func TestNotEquals_Struct_WithFieldNulls(t *testing.T) {
	var alloc memory.Allocator

	left := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), nil),
	)
	right := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2)),
	)

	actual, err := compute.NotEquals(&alloc, left, right, memory.Bitmap{})
	require.NoError(t, err)

	expect := columnartest.Array(t, types.KindBool, &alloc, false, nil)
	columnartest.RequireDatumsEqual(t, expect, actual, memory.Bitmap{})
}
