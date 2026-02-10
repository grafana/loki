package columnar_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestRecordBatch_Slice(t *testing.T) {
	var alloc memory.Allocator
	defer alloc.Reset()

	record := columnar.NewRecordBatch(
		columnar.NewSchema([]columnar.Column{
			{Name: "name"},
			{Name: "age"},
		}),
		4, // row count
		[]columnar.Array{
			columnartest.Array(t, columnar.KindUTF8, &alloc, "Peter", "Paul", "Mary", "John"),
			columnartest.Array(t, columnar.KindUint64, &alloc, 30, 25, 43, 28),
		},
	)

	slice := record.Slice(1, 3)

	var (
		expectNames = columnartest.Array(t, columnar.KindUTF8, &alloc, "Paul", "Mary")
		expectAges  = columnartest.Array(t, columnar.KindUint64, &alloc, 25, 43)
	)

	columnartest.RequireArraysEqual(t, expectNames, slice.Column(0))
	columnartest.RequireArraysEqual(t, expectAges, slice.Column(1))
}
