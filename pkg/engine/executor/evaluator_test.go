package executor

import (
	"iter"
	"slices"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/csv"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/grafana/loki/v3/pkg/engine/internal/arrowtest"
	"github.com/grafana/loki/v3/pkg/engine/planner/physical"
	"github.com/stretchr/testify/require"
)

func Test_evaluator_processLimit(t *testing.T) {
	alloc := memory.NewCheckedAllocator(memory.DefaultAllocator)
	defer alloc.AssertSize(t, 0)

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: &arrow.StringType{}, Nullable: false},
		{Name: "age", Type: &arrow.Int32Type{}, Nullable: false},
		{Name: "city", Type: &arrow.StringType{}, Nullable: true},
	}, nil)

	in := `
		Alice,25,San Francisco
		Bob,30,New York
		Charlie,35,NULL
		David,40,Los Angeles
		Eve,45,Chicago
		Frank,28,Seattle
		Grace,33,Boston
		Henry,38,NULL
		Ivy,42,Miami
		Jack,37,Portland
	`

	rec, err := arrowtest.ReadCSV(in, schema, csv.WithAllocator(alloc))
	require.NoError(t, err)
	defer rec.Release()
	require.EqualValues(t, 10, rec.NumRows())

	var (
		sliceA = rec.NewSlice(0, 3)
		sliceB = rec.NewSlice(3, 7)
		sliceC = rec.NewSlice(7, 10)
	)
	defer sliceA.Release()
	defer sliceB.Release()
	defer sliceC.Release()

	eval := evaluator{mem: alloc}

	r := eval.processLimit(&physical.Limit{
		Offset: 0,
		Limit:  5,
	}, []iter.Seq[Result]{
		slices.Values([]Result{recordResult(sliceA), recordResult(sliceB), recordResult(sliceC)}),
	})

	records := unwrapResults(t, r)
	defer func() {
		for _, r := range records {
			r.Release()
		}
	}()
	require.EqualValues(t, 2, len(records))

	expect := arrowtest.SanitizeCSV(`
		Alice,25,San Francisco
		Bob,30,New York
		Charlie,35,NULL
		David,40,Los Angeles
		Eve,45,Chicago
	`)

	actual, err := arrowtest.WriteCSV(schema, records...)
	require.NoError(t, err)
	require.Equal(t, expect, actual)
}

// unwrapResults consumes a sequence of [Result] instances and returns a slice
// or [arrow.Record] instances from the results.
//
// unwrapResults fails if any of the results has an error.
//
// The returned records must be Released by the caller.
func unwrapResults(t testing.TB, seq iter.Seq[Result]) []arrow.Record {
	t.Helper()

	var (
		records []arrow.Record
		errs    []error
	)

	for res := range seq {
		rec, err := res.Value()
		if rec != nil {
			records = append(records, rec)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		// Release all records before failing.
		for _, r := range records {
			r.Release()
		}

		t.Fatalf("errors found: %v", errs)
		return nil
	}

	return records
}
