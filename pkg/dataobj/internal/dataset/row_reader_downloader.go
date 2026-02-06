package dataset

import (
	"context"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/sliceclear"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// rowReaderDownloader is a utility for downloading pages in bulk from a
// [Dataset]. It works by caching page data from an inner dataset, and
// downloading pages in bulk any time an uncached page is requested.
//
// # Bulk download behavior
//
// Downloading pages in bulk is important to minimize round trips to the
// backend storage. The proper behavior of bulk downloads is tied to
// [RowReader.Read] operating in two phases:
//
//  1. Rows from primary columns are read and filtered by a predicate
//  2. Rows from secondary columns are read into the filtered rows
//
// Pages can be classified as a primary page (from a primary column) or a
// secondary page (from a secondary column).
//
// Anytime an uncached page is requested, the downloader will download a batch
// of page, assigning other pages a priority level:
//
//   - P1: Other pages of the same phase that overlap with the current read
//     range from [rowReaderDownloader.SetReadRange] and are not masked by
//     [rowReaderDownloader.SetMask].
//
//   - P2: Secondary pages that overlap with the current read range and are not
//     masked.
//
//     If the current phase is secondary, then there are no pages at this
//     priority level; as all secondary pages in the current read range would
//     be included in P1.
//
//   - P3: All pages that include rows after the end of the read range.
//
//     This excludes any page that is outside of the dataset ranges passed to
//     [newReaderDownloader] and [rowReaderDownloader.Reset].
//
// The rowReaderDownloader targets a configurable batch size, which is the target
// size of pages to cache in memory at once.
//
// Batches of pages to download are built in four steps:
//
//  1. Adding every uncached P1 page to the batch, even if this would exceed the
//     target size.
//
//  2. Continually add one P2 page across each column. Iteration stops if the
//     target size would be exceeded by a P2 page.
//
//  3. Continually added one P3 page across each primary column. Iteration stops
//     if the target size would be exceeded by a P3 page.
//
//  4. Continually add one P3 page across each secondary column. Iteration stops
//     if the target size would be exceeded by a P3 page.
//
// After every step, if the target size has been reached, the batch is
// downloaded without progressing to the following step.
//
// These rules provide some important properties:
//
//   - The minimum number of pages needed to download an entire dataset is one,
//     if every page in that dataset is less than the target size.
//
//   - The minimum number of pages needed to download a single [RowReader.Read] call
//     is zero, if all pages have been downloaded in a previous call.
//
//   - The maximum number of pages needed to download a single [RowReader.Read] call
//     is two: one for the primary phase, and another for the secondary phase.
//
//   - The separation of phases allows for the [RowReader] to mask additional ranges
//     before the secondary phase. This helps reduce the number of P1 pages
//     that are downloaded during the secondary phase.
//
// Some unused secondary pages may still be downloaded if there was space in
// the batch before a mask was added.
//
// Cached pages before the read range are cleared when a new uncached page is
// requested.
type rowReaderDownloader struct {
	inner Dataset

	origColumns, origPrimary, origSecondary []Column
	allColumns, primary, secondary          []Column

	dsetRanges rangeset.Set // Ranges of rows to _include_ in the download.

	readRange rangeset.Range // Current range being read.
	rangeMask rangeset.Set   // Inverse of dsetRanges: ranges to _exclude_ from download.
}

// newReaderDataset creates a new readerDataset wrapping around an inner
// Dataset. The resulting Dataset only wraps around the provided columns.
//
// All uncached pages that have not been pruned by
// [rowReaderDownloader.SetDatasetRanges] will be downloaded in bulk when an
// uncached page is requested.
//
// # Initialization
//
// After a rowReaderDownloader is created, it must be initialized by calling:
//
//  1. [rowReaderDownloader.AddColumn] with each column that will be read, and
//  2. [rowReaderDownloader.SetDatasetRanges] to define the valid ranges acrsos
//     the entire dataset.
//
// # Usage
//
// Use [rowReaderDownloader.AllColumns], [rowReaderDownloader.PrimaryColumns], and
// [rowReaderDownloader.SecondaryColumns] to enable page batching; any pages
// loaded from these columns will trigger a bulk download.
//
// Before each usage of the columns, users should call
// [rowReaderDownloader.SetReadRange] to define the range of rows that will be
// read next.
//
// If applicable, users should additionally call [rowReaderDownloader.Mask] to
// exclude any ranges of rows that should not be read; pages that are entirely
// within the mask will not be downloaded.
func newRowReaderDownloader(dset Dataset) *rowReaderDownloader {
	var rd rowReaderDownloader
	rd.Reset(dset)
	return &rd
}

// AddColumn adds a column to the rowReaderDownloader. This should be called
// before the downloader is used.
//
// AddColumn must be called matching the order of columns in
// [ReaderOptions.Columns].
func (dl *rowReaderDownloader) AddColumn(col Column, primary bool) {
	wrappedCol := newReaderColumn(dl, col, primary)

	dl.origColumns = append(dl.origColumns, col)
	dl.allColumns = append(dl.allColumns, wrappedCol)

	if primary {
		dl.origPrimary = append(dl.origPrimary, col)
		dl.primary = append(dl.primary, wrappedCol)
	} else {
		dl.origSecondary = append(dl.origSecondary, col)
		dl.secondary = append(dl.secondary, wrappedCol)
	}
}

// SetDatasetRanges sets the valid ranges of rows that will be read. Pages
// which do not overlap with these ranges will never be downloaded.
func (dl *rowReaderDownloader) SetDatasetRanges(r rangeset.Set) {
	dl.dsetRanges = r
}

// SetReadRange sets the row ranges that are currently being read. This is used
// to prioritize which pages to download in a batch. Pages that end before this
// range are never included in a batch.
//
// This method clears any previously set mask.
func (dl *rowReaderDownloader) SetReadRange(r rangeset.Range) {
	dl.readRange = r
	dl.rangeMask.Reset()
}

// Mask marks a subset of the current read range as excluded. Mask may be
// called multiple times to exclude multiple ranges. Any page that is entirely
// within the combined mask will not be downloaded.
func (dl *rowReaderDownloader) Mask(r rangeset.Range) {
	dl.rangeMask.Add(r)
}

// OrigColumns returns the original columns of the rowReaderDownloader in the order
// they were added.
func (dl *rowReaderDownloader) OrigColumns() []Column { return dl.origColumns }

// OrigPrimaryColumns returns the original primary columns of the
// rowReaderDownloader in the order they were added.
func (dl *rowReaderDownloader) OrigPrimaryColumns() []Column { return dl.origPrimary }

// OrigSecondaryColumns returns the original secondary columns of the
// rowReaderDownloader in the order they were added.
func (dl *rowReaderDownloader) OrigSecondaryColumns() []Column { return dl.origSecondary }

// AllColumns returns the wrapped columns of the rowReaderDownloader in the order
// they were added.
func (dl *rowReaderDownloader) AllColumns() []Column { return dl.allColumns }

// PrimaryColumns returns the wrapped primary columns of the rowReaderDownloader
// in the order they were added.
func (dl *rowReaderDownloader) PrimaryColumns() []Column { return dl.primary }

// SecondaryColumns returns the wrapped secondary columns of the
// rowReaderDownloader in the order they were added.
func (dl *rowReaderDownloader) SecondaryColumns() []Column { return dl.secondary }

// initColumnPages populates the pages of all columns in the downloader.
func (dl *rowReaderDownloader) initColumnPages(ctx context.Context) error {
	columns := dl.allColumns

	var idx int

	for result := range dl.inner.ListPages(ctx, dl.origColumns) {
		pages, err := result.Value()
		if err != nil {
			return err
		}

		columns[idx].(*readerColumn).processPages(pages)
		idx++
	}

	return nil
}

// downloadBatch downloads a batch of pages from the inner dataset.
func (dl *rowReaderDownloader) downloadBatch(ctx context.Context, requestor *readerPage) error {
	for _, col := range dl.allColumns {
		// Garbage collect any unused pages; this prevents them from being included
		// in the batchSize calculation and also allows them to be freed by the GC.
		col := col.(*readerColumn)
		col.GC()
	}

	batch, err := dl.buildDownloadBatch(ctx, requestor)
	if err != nil {
		return err
	}

	if region := xcap.RegionFromContext(ctx); region != nil {
		for _, page := range batch {
			if page.column.primary {
				region.Record(xcap.StatDatasetPrimaryPagesDownloaded.Observe(1))
				region.Record(xcap.StatDatasetPrimaryColumnBytes.Observe(int64(page.inner.PageDesc().CompressedSize)))
				region.Record(xcap.StatDatasetPrimaryColumnUncompressedBytes.Observe(int64(page.inner.PageDesc().UncompressedSize)))
			} else {
				region.Record(xcap.StatDatasetSecondaryPagesDownloaded.Observe(1))
				region.Record(xcap.StatDatasetSecondaryColumnBytes.Observe(int64(page.inner.PageDesc().CompressedSize)))
				region.Record(xcap.StatDatasetSecondaryColumnUncompressedBytes.Observe(int64(page.inner.PageDesc().UncompressedSize)))
			}
		}
	}

	// Build the set of inner pages that will be passed to the inner Dataset for
	// downloading.
	innerPages := make([]Page, len(batch))
	for i, page := range batch {
		innerPages[i] = page.inner
	}

	var i int

	for result := range dl.inner.ReadPages(ctx, innerPages) {
		data, err := result.Value()
		if err != nil {
			return err
		}

		batch[i].data = data
		i++
	}

	return nil
}

func (dl *rowReaderDownloader) buildDownloadBatch(ctx context.Context, requestor *readerPage) ([]*readerPage, error) {
	var pageBatch []*readerPage

	// Figure out how large our batch already is based on cache pages.
	var batchSize int
	for _, col := range dl.allColumns {
		batchSize += col.(*readerColumn).Size()
	}

	// Always add the requestor page to the batch if it's uncached.
	if len(requestor.data) == 0 {
		pageBatch = append(pageBatch, requestor)
	}

	// Add uncached P1 pages to the batch. We add all P1 pages, even if it would
	// exceed the target size.
	for result := range dl.iterP1Pages(ctx, requestor.column.primary) {
		page, err := result.Value()
		if err != nil {
			return nil, err
		} else if page.data != nil {
			continue
		} else if page == requestor {
			continue // Already added.
		}

		pageBatch = append(pageBatch, page)
	}

	// Now we add P2 and P3 pages. We ignore pages that would have us exceed the
	// target size.
	//
	// We don't add any P3 pages if any P2 pages were ignored; P3 pages are only
	// pages we may hypothetically need, so it's better to let more iteration
	// happen (so that some P3 pages may be filtered out) rather than trying to
	// stuff our batch size as full as possible and downloading pages that never
	// get used.

	var targetReached bool

	for result := range dl.iterP2Pages(ctx, requestor.column.primary) {
		page, err := result.Value()
		if err != nil {
			return nil, err
		} else if page.data != nil {
			continue
		} else if page == requestor {
			continue // Already added.
		}

		pageBatch = append(pageBatch, page)
	}
	if targetReached {
		return pageBatch, nil
	}

	for result := range dl.iterP3Pages(ctx, requestor.column.primary) {
		page, err := result.Value()
		if err != nil {
			return nil, err
		} else if page.data != nil {
			continue
		} else if page == requestor {
			continue // Already added.
		}

		pageBatch = append(pageBatch, page)
	}

	return pageBatch, nil
}

// iterP1Pages returns an iterator over P1 pages in round-robin column order,
// with one page per column.
func (dl *rowReaderDownloader) iterP1Pages(ctx context.Context, primary bool) result.Seq[*readerPage] {
	return result.Iter(func(yield func(*readerPage) bool) error {
		for result := range dl.iterColumnPages(ctx, primary) {
			page, err := result.Value()
			if err != nil {
				return err
			}

			// A P1 page must:
			//
			//  1. Overlap with the current read range.
			//  2. Be included in the set of valid dataset ranges.
			//  3. Not be masked by the range mask.
			if !dl.readRange.Overlaps(page.rows) {
				continue
			} else if !dl.dsetRanges.Overlaps(page.rows) {
				continue
			} else if dl.rangeMask.IncludesRange(page.rows) {
				continue
			}

			if !yield(page) {
				return nil
			}
		}

		return nil
	})
}

// iterColumnPages returns an iterator over pages in columns in round-robin
// order across all columns (first page from each column, then second page from
// each column, etc.).
func (dl *rowReaderDownloader) iterColumnPages(ctx context.Context, primary bool) result.Seq[*readerPage] {
	phaseColumns := dl.primary
	if !primary {
		phaseColumns = dl.secondary
	}

	return result.Iter(func(yield func(*readerPage) bool) error {
		var pageIndex int

		for {
			var foundPages bool

			for _, col := range phaseColumns {
				col := col.(*readerColumn)
				if len(col.pages) == 0 {
					if err := dl.initColumnPages(ctx); err != nil {
						return err
					}
				} else if pageIndex >= len(col.pages) {
					continue
				}

				page := col.pages[pageIndex]
				foundPages = true
				if !yield(page) {
					return nil
				}
			}
			if !foundPages {
				return nil
			}

			pageIndex++
		}
	})
}

// iterP2Pages returns an iterator over P2 pages in round-robin column order,
// with one page per column.
func (dl *rowReaderDownloader) iterP2Pages(ctx context.Context, primary bool) result.Seq[*readerPage] {
	// For the primary phase, P2 pages are pages that would be P1 for the
	// secondary phase. This means we can express it as iterP1Pages(ctx, !primary).
	//
	// However, if we're in the secondary phase, then there are no P2 pages.
	if !primary {
		return result.Iter(func(_ func(*readerPage) bool) error {
			return nil
		})
	}

	return dl.iterP1Pages(ctx, !primary)
}

// iterP3Pages returns an iterator over P3 pages in round-robin column order,
// with one page per column.
func (dl *rowReaderDownloader) iterP3Pages(ctx context.Context, primary bool) result.Seq[*readerPage] {
	return result.Iter(func(yield func(*readerPage) bool) error {
		for result := range dl.iterColumnPages(ctx, primary) {
			page, err := result.Value()
			if err != nil {
				return err
			}

			// A P3 page must:
			//
			//  1. Start *after* the end of the current read range
			//  2. Be included in the set of valid dataset ranges.
			//  3. Not be masked by the range mask.
			if page.rows.Start < dl.readRange.End {
				continue
			} else if !dl.dsetRanges.Overlaps(page.rows) {
				continue
			} else if dl.rangeMask.IncludesRange(page.rows) {
				continue
			}

			if !yield(page) {
				return nil
			}
		}

		return nil
	})
}

func (dl *rowReaderDownloader) Reset(dset Dataset) {
	dl.inner = dset

	dl.readRange = rangeset.Range{}

	dl.origColumns = sliceclear.Clear(dl.origColumns)
	dl.origPrimary = sliceclear.Clear(dl.origPrimary)
	dl.origSecondary = sliceclear.Clear(dl.origSecondary)

	dl.allColumns = sliceclear.Clear(dl.allColumns)
	dl.primary = sliceclear.Clear(dl.primary)
	dl.secondary = sliceclear.Clear(dl.secondary)

	dl.rangeMask = rangeset.Set{}

	// dl.dsetRanges isn't owned by the downloader, so we don't use
	// sliceclear.Clear.
	dl.dsetRanges = rangeset.Set{}
}

type readerColumn struct {
	dl      *rowReaderDownloader
	inner   Column
	primary bool // Whether this column is a primary column.

	pages []*readerPage
}

var _ Column = (*readerColumn)(nil)

func newReaderColumn(dl *rowReaderDownloader, col Column, primary bool) *readerColumn {
	return &readerColumn{
		dl:      dl,
		inner:   col,
		primary: primary,
	}
}

func (col *readerColumn) ColumnDesc() *ColumnDesc {
	// Implementations of Column are expected to cache ColumnInfo when the Column
	// is built, so there's no need to cache it a second time here.
	return col.inner.ColumnDesc()
}

func (col *readerColumn) ListPages(ctx context.Context) result.Seq[Page] {
	return result.Iter(func(yield func(Page) bool) error {
		if len(col.pages) == 0 {
			err := col.dl.initColumnPages(ctx)
			if err != nil {
				return err
			}
		}

		for _, p := range col.pages {
			if !yield(p) {
				return nil
			}
		}

		return nil
	})
}

func (col *readerColumn) processPages(pages Pages) {
	var startRow uint64

	for _, innerPage := range pages {
		pageRange := rangeset.Range{
			Start: startRow,
			End:   startRow + uint64(innerPage.PageDesc().RowCount),
		}
		startRow = pageRange.End

		col.pages = append(col.pages, newReaderPage(col, innerPage, pageRange))
	}
}

// GC garbage collects cached data from pages which will no longer be read: any
// page which ends before the read row range of the downloader.
//
// Using the minimum read row range permits failed calls to [RowReader.Read] to be
// retried without needing to redownload the pages involved in that call.
func (col *readerColumn) GC() {
	for _, page := range col.pages {
		if page.rows.End <= col.dl.readRange.Start {
			// This page is entirely before the read range. We can clear it.
			//
			// TODO(rfratto): should this be released back to some kind of pool that
			// decoders use so we don't have to allocate bytes every time a page is
			// downloaded?
			page.data = nil
		}
	}
}

// Size returns the total byte size of all cached pages in col.
func (col *readerColumn) Size() int {
	var size int
	for _, page := range col.pages {
		if page.data != nil {
			size += len(page.data)
		}
	}
	return size
}

type readerPage struct {
	column *readerColumn
	inner  Page
	rows   rangeset.Range

	data PageData // data holds cached PageData.
}

var _ Page = (*readerPage)(nil)

func newReaderPage(col *readerColumn, inner Page, rows rangeset.Range) *readerPage {
	return &readerPage{
		column: col,
		inner:  inner,
		rows:   rows,
	}
}

func (page *readerPage) PageDesc() *PageDesc {
	// Implementations of Page are expected to cache PageInfo when the Page is
	// built, so there's no need to cache it a second time here.
	return page.inner.PageDesc()
}

func (page *readerPage) ReadPage(ctx context.Context) (PageData, error) {
	region := xcap.RegionFromContext(ctx)
	region.Record(xcap.StatDatasetPagesScanned.Observe(1))
	if page.data != nil {
		region.Record(xcap.StatDatasetPagesFoundInCache.Observe(1))
		return page.data, nil
	}

	region.Record(xcap.StatDatasetPageDownloadRequests.Observe(1))
	if err := page.column.dl.downloadBatch(ctx, page); err != nil {
		return nil, err
	}

	// The call to downloadBatch is supposed to populate page.data. If it didn't,
	// that's a bug. However, to keep things working we'll fall back to the inner
	// page.
	if page.data != nil {
		return page.data, nil
	}

	// TODO(rfratto): we should never hit this unless there's a bug; this needs
	// to log something or increment some kind of counter so we can catch and fix
	// the bug.
	data, err := page.inner.ReadPage(ctx)
	if err != nil {
		return nil, err
	}
	page.data = data
	return data, nil
}
