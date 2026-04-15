package columnartest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestStruct_Helper(t *testing.T) {
	var alloc memory.Allocator

	s := columnartest.Struct(t, &alloc,
		columnartest.Field("name", columnar.KindUTF8, "alice", "bob"),
		columnartest.Field("age", columnar.KindInt64, int64(30), int64(25)),
	)

	require.Equal(t, 2, s.Len())
	require.Equal(t, 2, s.NumFields())
	require.Equal(t, columnar.KindUTF8, s.Field(0).Kind())
	require.Equal(t, columnar.KindInt64, s.Field(1).Kind())
}

func TestRequireArraysEqual_Struct(t *testing.T) {
	var alloc memory.Allocator

	a := columnartest.Struct(t, &alloc,
		columnartest.Field("x", columnar.KindInt64, int64(1), int64(2)),
	)
	b := columnartest.Struct(t, &alloc,
		columnartest.Field("x", columnar.KindInt64, int64(1), int64(2)),
	)

	columnartest.RequireArraysEqual(t, a, b, memory.Bitmap{})
}
