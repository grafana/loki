package executor

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/arrowtest"
)

func Test_changeSchema(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	var (
		oldSchema = arrow.NewSchema([]arrow.Field{
			{Name: "a", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "b", Type: arrow.FixedWidthTypes.Boolean, Nullable: true},
		}, nil)

		newSchema = arrow.NewSchema([]arrow.Field{
			{Name: "number", Type: arrow.PrimitiveTypes.Int64, Nullable: true},
			{Name: "bool", Type: arrow.FixedWidthTypes.Boolean, Nullable: true},
		}, nil)
	)

	var (
		data = arrowtest.Rows{
			{"a": int64(1), "b": true},
			{"a": int64(2), "b": false},
		}

		expect = arrowtest.Rows{
			{"number": int64(1), "bool": true},
			{"number": int64(2), "bool": false},
		}
	)

	rec := data.Record(alloc, oldSchema)
	defer rec.Release()

	newRec, err := changeSchema(rec, newSchema)
	require.NoError(t, err, "changeSchema should not return an error")
	defer newRec.Release()

	actual, err := arrowtest.RecordRows(newRec)
	require.NoError(t, err, "arrowtest.RecordRows should not return an error")
	require.Equal(t, expect, actual, "changed schema should match expected rows")
}
