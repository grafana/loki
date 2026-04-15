package columnar_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestStruct_Basic(t *testing.T) {
	var alloc memory.Allocator

	schema := columnar.NewSchema([]columnar.Column{
		{Name: "name"},
		{Name: "age"},
	})
	actual := columnar.NewStruct(schema, []columnar.Array{
		columnartest.Array(t, columnar.KindUTF8, &alloc, "alice", "bob"),
		columnartest.Array(t, columnar.KindInt64, &alloc, int64(30), int64(25)),
	}, 2, memory.Bitmap{})

	expect := columnartest.Struct(t, &alloc,
		columnartest.Field("name", columnar.KindUTF8, "alice", "bob"),
		columnartest.Field("age", columnar.KindInt64, int64(30), int64(25)),
	)
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})
}

func TestStruct_Nullable(t *testing.T) {
	var alloc memory.Allocator

	schema := columnar.NewSchema([]columnar.Column{{Name: "x"}})

	validity := memory.NewBitmap(&alloc, 2)
	validity.AppendValues(true, false)

	actual := columnar.NewStruct(schema, []columnar.Array{
		columnartest.Array(t, columnar.KindInt64, &alloc, int64(10), int64(0)),
	}, 2, validity)

	expect := columnartest.StructWithValidity(t, &alloc, []bool{true, false},
		columnartest.Field("x", columnar.KindInt64, int64(10), int64(0)),
	)
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})
}

func TestStruct_Slice(t *testing.T) {
	var alloc memory.Allocator

	in := columnartest.Struct(t, &alloc,
		columnartest.Field("x", columnar.KindInt64, int64(10), int64(20), int64(30)),
	)

	expect := columnartest.Struct(t, &alloc,
		columnartest.Field("x", columnar.KindInt64, int64(20), int64(30)),
	)
	actual := in.Slice(1, 3)
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})
}

func TestStruct_Slice_WithValidity(t *testing.T) {
	var alloc memory.Allocator

	in := columnartest.StructWithValidity(t, &alloc, []bool{true, false, true},
		columnartest.Field("x", columnar.KindInt64, int64(10), int64(0), int64(30)),
	)

	expect := columnartest.StructWithValidity(t, &alloc, []bool{false, true},
		columnartest.Field("x", columnar.KindInt64, int64(0), int64(30)),
	)
	actual := in.Slice(1, 3)
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})
}

func TestStruct_Size(t *testing.T) {
	var alloc memory.Allocator

	s := columnartest.Struct(t, &alloc,
		columnartest.Field("x", columnar.KindInt64, int64(10)),
	)
	require.Equal(t, s.Field(0).Size(), s.Size())
}
