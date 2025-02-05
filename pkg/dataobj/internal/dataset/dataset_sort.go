package dataset

import (
	"context"
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Sort returns a new Dataset with rows sorted by the given sortBy columns in
// ascending order. The order of columns in the new Dataset will match the
// order in set. pageSizeHint specifies the page size to target for newly
// created pages.
//
// If sortBy is empty or if the columns in sortBy contain no rows, Sort returns
// set.
func Sort(ctx context.Context, set Dataset, sortBy []Column, pageSizeHint int) (Dataset, error) {
	if len(sortBy) == 0 {
		return set, nil
	}

	// First, we need to know the final sort over of entries in sortBy. Then, we
	// sort the rows by sortBy.
	var sortedRows []Row
	for ent := range Iter(ctx, sortBy) {
		row, err := ent.Value()
		if err != nil {
			return nil, fmt.Errorf("iterating over existing dataset: %w", err)
		} else if len(row.Values) != len(sortBy) {
			return nil, fmt.Errorf("row column mismatch: expected %d, got %d", len(sortBy), len(row.Values))
		}
		sortedRows = append(sortedRows, row)
	}

	if len(sortedRows) == 0 {
		// Empty dataset; nothing to sort.
		return set, nil
	}

	// Order sortedRows in the proper order. After this, sortRows[0] correponds
	// to what should be the new first row, while sortRows[0].Index tells us what
	// its original index (in the unsorted Dataset) was.
	slices.SortStableFunc(sortedRows, func(a, b Row) int {
		for i, aValue := range a.Values {
			if i >= len(b.Values) {
				// a.Values and b.Values are always be the same length, but we check
				// here anyways to avoid a panic.
				break
			}
			bValue := b.Values[i]

			// We return the first non-zero comparison result. Otherwise, the rows
			// are equal (in terms of sortBy).
			switch CompareValues(aValue, bValue) {
			case -1:
				return -1
			case 1:
				return 1
			}
		}

		return 0
	})

	origColumns, err := result.Collect(set.ListColumns(ctx))
	if err != nil {
		return nil, fmt.Errorf("listing columns: %w", err)
	}

	newColumns := make([]*MemColumn, 0, len(origColumns))
	for _, origColumn := range origColumns {
		origInfo := origColumn.ColumnInfo()
		pages, err := result.Collect(origColumn.ListPages(ctx))
		if err != nil {
			return nil, fmt.Errorf("getting pages: %w", err)
		} else if len(pages) == 0 {
			return nil, fmt.Errorf("unexpected column with no pages")
		}

		newColumn, err := NewColumnBuilder(origInfo.Name, BuilderOptions{
			PageSizeHint: pageSizeHint,
			Value:        origInfo.Type,
			Compression:  origInfo.Compression,

			// TODO(rfratto): This only works now as all pages have the same
			// encoding. If we add support for mixed encoding we'll need to do
			// something different here.
			Encoding: pages[0].PageInfo().Encoding,
		})
		if err != nil {
			return nil, fmt.Errorf("creating new column: %w", err)
		}

		// newColumn becomes populated in sorted order by populating row 0 from
		// sortedRows[0].Index, row 1 from sortedRows[1].Index, and so on.
		//
		// While sortedRows contains all rows across the columns to sort by,
		// columns to sort by have a smaller memory footprint than other columns
		// (for example, loading the entire set of timestamps is far smaller than
		// the entire set of log lines).
		//
		// To minimize the memory overhead of sorting, we only collect values from
		// a single page at a time. This is more memory-efficient, but comes at the
		// cost of more page reads for very unsorted data.
		var (
			curPage       Page    // Current page for iteration.
			curPageValues []Value // Values in the current page.
		)
		for i, sortRow := range sortedRows {
			// To avoid re-reading pages for every row to sort, we cache the most
			// recent nextPage and its values. This way we only need to read a
			// nextPage once sortRow crosses over to a new nextPage.
			//
			// If origColumn is shorter than other columns in set, pageForRow will
			// return (nil, -1), indicating that the row isn't available in the
			// column. In case, we want to append the zero [Value] to denote a NULL.
			nextPage, rowInPage := pageForRow(pages, sortRow.Index)
			if nextPage != nil && nextPage != curPage {
				curPage = nextPage

				data, err := curPage.ReadPage(ctx)
				if err != nil {
					return nil, fmt.Errorf("getting page data: %w", err)
				}

				memPage := &MemPage{
					Info: *curPage.PageInfo(),
					Data: data,
				}
				curPageValues, err = result.Collect(iterMemPage(memPage, origInfo.Type, origInfo.Compression))
				if err != nil {
					return nil, fmt.Errorf("reading page: %w", err)
				}
			}

			var value Value
			if rowInPage != -1 {
				value = curPageValues[rowInPage]
			}

			if err := newColumn.Append(i, value); err != nil {
				return nil, fmt.Errorf("appending value: %w", err)
			}
		}

		memColumn, err := newColumn.Flush()
		if err != nil {
			return nil, fmt.Errorf("flushing column: %w", err)
		}
		newColumns = append(newColumns, memColumn)
	}

	return FromMemory(newColumns), nil
}

// pageForRow returns the page that contains the provided column row number
// along with the relative row number for that page.
func pageForRow(pages []Page, row int) (Page, int) {
	startRow := 0

	for _, page := range pages {
		info := page.PageInfo()

		if row >= startRow && row < startRow+info.RowCount {
			return page, row - startRow
		}

		startRow += info.RowCount
	}

	return nil, -1
}
