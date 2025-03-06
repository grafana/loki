// Package dataset contains utilities for working with datasets. Datasets hold
// columnar data across multiple pages.
package dataset

import (
	"context"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// A Dataset holds a collection of [Columns], each of which is split into a set
// of [Pages] and further split into a sequence of [Values].
//
// Dataset is read-only; callers must not modify any of the values returned by
// methods in Dataset.
type Dataset interface {
	// ListColumns returns the set of [Column]s in the Dataset. The order of
	// Columns in the returned sequence must be consistent across calls.
	ListColumns(ctx context.Context) result.Seq[Column]

	// ListPages retrieves a set of [Pages] given a list of [Column]s.
	// Implementations of Dataset may use ListPages to optimize for batch reads.
	// The order of [Pages] in the returned sequence must match the order of the
	// columns argument.
	ListPages(ctx context.Context, columns []Column) result.Seq[Pages]

	// ReadPages returns the set of [PageData] for the specified slice of pages.
	// Implementations of Dataset may use ReadPages to optimize for batch reads.
	// The order of [PageData] in the returned sequence must match the order of
	// the pages argument.
	ReadPages(ctx context.Context, pages []Page) result.Seq[PageData]
}

// FromMemory returns an in-memory [Dataset] from the given list of
// [MemColumn]s.
func FromMemory(columns []*MemColumn) Dataset {
	return memDataset(columns)
}

type memDataset []*MemColumn

func (d memDataset) ListColumns(_ context.Context) result.Seq[Column] {
	return result.Iter(func(yield func(Column) bool) error {
		for _, c := range d {
			if !yield(c) {
				return nil
			}
		}

		return nil
	})
}

func (d memDataset) ListPages(ctx context.Context, columns []Column) result.Seq[Pages] {
	return result.Iter(func(yield func(Pages) bool) error {
		for _, c := range columns {
			pages, err := result.Collect(c.ListPages(ctx))
			if err != nil {
				return err
			} else if !yield(Pages(pages)) {
				return nil
			}
		}

		return nil
	})
}

func (d memDataset) ReadPages(ctx context.Context, pages []Page) result.Seq[PageData] {
	return result.Iter(func(yield func(PageData) bool) error {
		for _, p := range pages {
			data, err := p.ReadPage(ctx)
			if err != nil {
				return err
			} else if !yield(data) {
				return nil
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
