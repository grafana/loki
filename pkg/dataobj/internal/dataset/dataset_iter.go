package dataset

import (
	"context"
	"math"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Iter iterates over the rows for the given list of columns. Each [Row] in the
// returned sequence will only contain values for the columns matching the
// columns argument. Values in each row match the order of the columns argument
// slice.
//
// Iter lazily fetches pages as needed.
func Iter(ctx context.Context, columns []Column) result.Seq[Row] {
	// TODO(rfratto): Iter is insufficient for reading at scale:
	//
	// * Pages are lazily fetched one at a time, which would cause many round
	//   trips to the underlying storage.
	//
	// * There's no support for filtering at the page or row level, meaning we
	//   may overfetch data.
	//
	// The current implementation is acceptable only for in-memory Datasets. A
	// more efficient implementation is needed for reading Datasets backed by
	// object storage.

	totalRows := math.MinInt64
	for _, col := range columns {
		totalRows = max(totalRows, col.ColumnInfo().RowsCount)
	}

	type pullColumnIter struct {
		Next func() (result.Result[Value], bool)
		Stop func()
	}

	return result.Iter(func(yield func(Row) bool) error {
		// Create pull-based iters for all of our columns; this will allow us to
		// get one value at a time per column.
		pullColumns := make([]pullColumnIter, 0, len(columns))
		for _, col := range columns {
			pages, err := result.Collect(col.ListPages(ctx))
			if err != nil {
				return err
			}

			next, stop := result.Pull(lazyColumnIter(ctx, col.ColumnInfo(), pages))
			pullColumns = append(pullColumns, pullColumnIter{Next: next, Stop: stop})
		}

		// Start emitting rows; each row is composed of the next value from all of
		// our columns. If a column ends early, it's left as the zero [Value],
		// corresponding to a NULL.
		for rowIndex := 0; rowIndex < totalRows; rowIndex++ {
			rowValues := make([]Value, len(pullColumns))

			for i, column := range pullColumns {
				res, ok := column.Next()
				value, err := res.Value()
				if !ok {
					continue
				} else if err != nil {
					return err
				}

				rowValues[i] = value
			}

			row := Row{Index: rowIndex, Values: rowValues}
			if !yield(row) {
				return nil
			}
		}

		return nil
	})
}

func lazyColumnIter(ctx context.Context, column *ColumnInfo, pages []Page) result.Seq[Value] {
	return result.Iter(func(yield func(Value) bool) error {
		for _, page := range pages {
			pageData, err := page.ReadPage(ctx)
			if err != nil {
				return err
			}
			memPage := &MemPage{
				Info: *page.PageInfo(),
				Data: pageData,
			}

			for result := range iterMemPage(memPage, column.Type, column.Compression) {
				val, err := result.Value()
				if err != nil {
					return err
				} else if !yield(val) {
					return nil
				}
			}
		}

		return nil
	})
}

// A Row in a Dataset is a set of values across multiple columns with the same
// row number.
type Row struct {
	Index  int     // Index of the row in the dataset.
	Values []Value // Values for the row, one per [Column].
}
