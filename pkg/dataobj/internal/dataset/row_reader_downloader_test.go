package dataset

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/readbudget"
)

// reserverFunc adapts a function to a [readbudget.Reserver].
type reserverFunc func(ctx context.Context, bytes int64) (*readbudget.Lease, error)

func (f reserverFunc) Reserve(ctx context.Context, bytes int64) (*readbudget.Lease, error) {
	return f(ctx, bytes)
}

// failingReadDataset fails every page download.
type failingReadDataset struct {
	Dataset
	err error
}

func (d failingReadDataset) ReadPages(context.Context, []Page) result.Seq[PageData] {
	return func(yield func(result.Result[PageData]) bool) {
		yield(result.Error[PageData](d.err))
	}
}

// pagesOf returns the pages of every column of the reader.
func pagesOf(r *RowReader) []*readerPage {
	var pages []*readerPage
	for _, col := range r.dl.allColumns {
		pages = append(pages, col.(*readerColumn).pages...)
	}
	return pages
}

// cachedBytesOf returns the compressed size of the pages that hold data.
func cachedBytesOf(r *RowReader) int64 {
	var size int64
	for _, page := range pagesOf(r) {
		if page.data != nil {
			size += int64(page.PageDesc().CompressedSize)
		}
	}
	return size
}

// readOneRowAtATime reads the dataset one row per call and runs check after every call that
// returns a row. check receives the first row of the read.
func readOneRowAtATime(t *testing.T, r *RowReader, check func(readStart uint64)) {
	t.Helper()

	batch := make([]Row, 1)
	for {
		n, err := r.Read(t.Context(), batch)
		if errors.Is(err, io.EOF) {
			return
		}
		require.NoError(t, err)
		require.Equal(t, 1, n)

		check(uint64(r.row - 1))
	}
}

func Test_RowReader_FreesPassedPages(t *testing.T) {
	t.Run("clears the pages that end before each read and keeps the later ones", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{Dataset: dset, Columns: columns, PrefetchAllOnOpen: true})
		defer r.Close()
		require.NoError(t, r.Open(t.Context()))

		var cleared bool
		readOneRowAtATime(t, r, func(readStart uint64) {
			for _, page := range pagesOf(r) {
				if page.rows.End <= readStart {
					require.Nil(t, page.data, "rows %v end before the read at row %d", page.rows, readStart)
					cleared = true
				} else {
					require.NotNil(t, page.data, "rows %v end after the read at row %d", page.rows, readStart)
				}
			}
		})

		require.True(t, cleared, "expected the read to pass at least one page")
	})

	t.Run("keeps the held bytes equal to the cached pages when a predicate prunes pages", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		budget := readbudget.New(1<<40, nil)
		r := NewRowReader(RowReaderOptions{
			Dataset:           dset,
			Columns:           columns,
			PrefetchAllOnOpen: true,
			Reserver:          budget.Section("object", 0),
			Predicates: []Predicate{
				OrPredicate{
					Left: EqualPredicate{Column: columns[0], Value: BinaryValue([]byte("John"))},
					Right: OrPredicate{
						Left:  EqualPredicate{Column: columns[0], Value: BinaryValue([]byte("Eve"))},
						Right: EqualPredicate{Column: columns[0], Value: BinaryValue([]byte("Henry"))},
					},
				},
			},
		})
		require.NoError(t, r.Open(t.Context()))

		var uncached int
		for _, page := range pagesOf(r) {
			if page.data == nil {
				uncached++
			}
		}
		require.NotZero(t, uncached, "expected the predicate to prune at least one page")
		require.Equal(t, cachedBytesOf(r), budget.Held())

		batch := make([]Row, 1)
		for {
			_, err := r.Read(t.Context(), batch)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(t, err)

			require.Equal(t, cachedBytesOf(r), budget.Held())
			for _, page := range pagesOf(r) {
				if page.rows.End <= r.dl.readRange.Start {
					require.Nil(t, page.data, "rows %v end before the read at row %d", page.rows, r.dl.readRange.Start)
				}
			}
		}

		require.NoError(t, r.Close())
		require.Zero(t, budget.Held())
	})
}

func Test_RowReader_Reserver(t *testing.T) {
	t.Run("reserves the compressed size of every prefetched page before it downloads any", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		budget := readbudget.New(1<<40, nil)

		var r *RowReader
		var reserved int64
		r = NewRowReader(RowReaderOptions{
			Dataset:           dset,
			Columns:           columns,
			PrefetchAllOnOpen: true,
			Reserver: reserverFunc(func(ctx context.Context, bytes int64) (*readbudget.Lease, error) {
				require.Zero(t, cachedBytesOf(r), "the reader downloaded pages before it reserved")
				reserved = bytes
				return budget.Section("object", 0).Reserve(ctx, bytes)
			}),
		})
		defer r.Close()

		require.NoError(t, r.Open(t.Context()))

		require.NotZero(t, reserved)
		require.Equal(t, cachedBytesOf(r), reserved)
		require.Equal(t, reserved, budget.Held())
	})

	t.Run("holds exactly the bytes of the cached pages while the read advances, and nothing after close", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		budget := readbudget.New(1<<40, nil)
		r := NewRowReader(RowReaderOptions{
			Dataset:           dset,
			Columns:           columns,
			PrefetchAllOnOpen: true,
			Reserver:          budget.Section("object", 0),
		})
		require.NoError(t, r.Open(t.Context()))
		opened := budget.Held()
		require.NotZero(t, opened)

		readOneRowAtATime(t, r, func(uint64) {
			require.Equal(t, cachedBytesOf(r), budget.Held())
		})
		require.Less(t, budget.Held(), opened, "expected the read to give back the pages it passed")

		require.NoError(t, r.Close())
		require.Zero(t, budget.Held())
		require.Zero(t, cachedBytesOf(r))
	})

	t.Run("returns the bytes when the reader resets without a close", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		budget := readbudget.New(1<<40, nil)
		r := NewRowReader(RowReaderOptions{
			Dataset:           dset,
			Columns:           columns,
			PrefetchAllOnOpen: true,
			Reserver:          budget.Section("object", 0),
		})
		require.NoError(t, r.Open(t.Context()))
		require.NotZero(t, budget.Held())

		r.Reset(RowReaderOptions{Dataset: dset, Columns: columns})

		require.Zero(t, budget.Held())
	})

	t.Run("fails the open and downloads nothing when the reservation fails", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		errNoRoom := errors.New("no room")

		r := NewRowReader(RowReaderOptions{
			Dataset:           dset,
			Columns:           columns,
			PrefetchAllOnOpen: true,
			Reserver: reserverFunc(func(context.Context, int64) (*readbudget.Lease, error) {
				return nil, errNoRoom
			}),
		})

		require.ErrorIs(t, r.Open(t.Context()), errNoRoom)
		require.Zero(t, cachedBytesOf(r))
	})

	t.Run("returns the reserved bytes when the download fails after the reservation", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		errDownload := errors.New("download failed")
		budget := readbudget.New(1<<40, nil)
		r := NewRowReader(RowReaderOptions{
			Dataset:           failingReadDataset{Dataset: dset, err: errDownload},
			Columns:           columns,
			PrefetchAllOnOpen: true,
			Reserver:          budget.Section("object", 0),
		})

		require.ErrorIs(t, r.Open(t.Context()), errDownload)
		require.Zero(t, budget.Held())
	})

	t.Run("fails to open when the reader downloads lazily", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{
			Dataset:           dset,
			Columns:           columns,
			PrefetchAllOnOpen: false,
			Reserver: reserverFunc(func(context.Context, int64) (*readbudget.Lease, error) {
				require.FailNow(t, "the lazy reader reserved bytes")
				return nil, nil
			}),
		})
		defer r.Close()

		require.ErrorContains(t, r.Open(t.Context()), "needs PrefetchAllOnOpen")
	})
}
