package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
	"github.com/grafana/loki/v3/pkg/memory"
)

type pageReader struct {
	page         Page
	physicalType datasetmd.PhysicalType
	compression  datasetmd.CompressionType
	ready        bool // Whether the pageReader is initialized for page.
	alloc        memory.Allocator

	lastPhysicalType datasetmd.PhysicalType
	lastEncoding     datasetmd.EncodingType

	closer      io.Closer
	presenceDec *bitmapDecoder
	valuesDec   valueDecoder

	pageRow int64
	nextRow int64
}

// newPageReader returns a new pageReader that reads from the provided page.
// The page must hold values of the provided value type, and be compressed with
// the provided compression type.
func newPageReader(p Page, physicalType datasetmd.PhysicalType, compression datasetmd.CompressionType) *pageReader {
	var pr pageReader
	pr.Reset(p, physicalType, compression)
	return &pr
}

// Read reads up to the next len(v) values from the page into v. It returns the
// number of values read and any error encountered. At the end of the page,
// Read returns 0, io.EOF.
func (pr *pageReader) Read(ctx context.Context, v []Value) (n int, err error) {
	// We need to initialize our readers before we can read from the page.
	//
	// If we've seeked backwards and our page row is now ahead of the row we want
	// to read, we need to reinitialize the page reader to read from the start of
	// the page.
	if !pr.ready || pr.pageRow > pr.nextRow {
		err := pr.init(ctx)
		if err != nil {
			return n, err
		}
	}

	// "Skip" rows until we reach the starting row we want to read. We do this by
	// reading garbage values into v.
	for pr.pageRow < pr.nextRow {
		maxCount := min(len(v), int(pr.nextRow-pr.pageRow))
		_, err := pr.read(v[:maxCount])
		if err != nil {
			return n, err
		}
	}

	n, err = pr.read(v)
	pr.nextRow += int64(n)
	return n, err
}

// read reads up to the next len(v) values from the page into v, without
// considering the current row offset.
//
// read advances pr.pageRow but not pr.nextRow.
func (pr *pageReader) read(v []Value) (n int, err error) {
	// Reclaim any memory allocated since the previous read call. We don't use
	// Reset here, otherwise a call to [pageReader.read] that only retrieve
	// NULLs will cause allocated memory for non-NULL data to be prematurely
	// released.
	//
	// NOTE(rfratto): This is only safe as the pageReader owns the allocator and
	// copies memory into v. Once we return allocated memory directly, we will
	// need to find a new mechanism to prevent over-allocating.
	pr.alloc.Reclaim()

	// First read presence values for the next len(v) rows.
	bm := memory.MakeBitmap(&pr.alloc, len(v))
	err = pr.presenceDec.DecodeTo(&bm, len(v))
	count := bm.Len()
	if err != nil && !errors.Is(err, io.EOF) {
		return n, err
	} else if count == 0 && errors.Is(err, io.EOF) {
		// If we've hit EOF, we can immediately close the inner reader to release
		// any resources back, rather than waiting for the next call to
		// [pageReader.init] to do it.
		_ = pr.Close()

		return n, io.EOF
	} else if count == 0 {
		return 0, nil
	}

	// The number of bits set to 1 in presenceBuf determines how many values we
	// need to read from the inner page.
	presentCount := bm.SetCount()

	var values columnar.Array

	// Now fill up to presentCount values of concrete values.
	if presentCount > 0 {
		values, err = pr.valuesDec.Decode(&pr.alloc, presentCount)
		if err != nil && !errors.Is(err, io.EOF) {
			return n, err
		} else if values == nil {
			return n, fmt.Errorf("unexpected nil values")
		} else if values.Len() != presentCount {
			return n, fmt.Errorf("unexpected number of values: %d, expected: %d", values.Len(), presentCount)
		}
	}

	if err := materializeSparseArray(v, bm, values); err != nil {
		return n, err
	}

	n += count
	pr.pageRow += int64(count)
	return n, nil
}

// materializeSparseArray materializes a dense array into a sparse [Value] slice
// based on a presence bitmap.
//
// len(dst) must be at least validity.Len().
func materializeSparseArray(dst []Value, validity memory.Bitmap, denseValues columnar.Array) error {
	if len(dst) < validity.Len() {
		panic(fmt.Sprintf("invariant broken: dst len (%d) is less than validity len (%d)", len(dst), validity.Len()))
	}

	switch arr := denseValues.(type) {
	case *columnar.UTF8:
		return materializeSparseUTF8(dst, validity, arr)
	case *columnar.Int64:
		return materializeSparseInt64(dst, validity, arr)
	case nil:
		return materializeNulls(dst, validity)
	default:
		panic(fmt.Sprintf("found unexpected type %T", arr))
	}
}

func materializeSparseUTF8(dst []Value, validity memory.Bitmap, denseValues *columnar.UTF8) error {
	var denseIndex int

	for i := range validity.Len() {
		if !validity.Get(i) {
			dst[i] = Value{}
			continue
		} else if denseIndex >= denseValues.Len() {
			return fmt.Errorf("unexpected end of values")
		}

		srcBuf := denseValues.Get(denseIndex)
		denseIndex++

		dstBuf := slicegrow.GrowToCap(dst[i].Buffer(), len(srcBuf))
		dstBuf = dstBuf[:len(srcBuf)]
		copy(dstBuf, srcBuf)

		dst[i] = BinaryValue(dstBuf)
	}

	return nil
}

func materializeSparseInt64(dst []Value, validity memory.Bitmap, denseValues *columnar.Int64) error {
	srcValues := denseValues.Values()

	var srcIndex int
	for i := range validity.Len() {
		if !validity.Get(i) {
			dst[i].Zero()
			continue
		} else if srcIndex >= len(srcValues) {
			return fmt.Errorf("unexpected end of values")
		}

		dst[i] = Int64Value(srcValues[srcIndex])
		srcIndex++
	}

	return nil
}

func materializeNulls(dst []Value, validity memory.Bitmap) error {
	if validity.SetCount() > 0 {
		return fmt.Errorf("unexpected non-null values")
	}

	for i := range validity.Len() {
		dst[i].Zero()
	}
	return nil
}

func (pr *pageReader) init(ctx context.Context) error {
	// Close any existing reader from a previous pageReader init. Even though
	// this also happens in [pageReader.Close], we want to do it here as well in
	// case we seeked backwards in a file.
	if err := pr.Close(); err != nil {
		return fmt.Errorf("closing previous page: %w", err)
	}

	data, err := pr.page.ReadPage(ctx)
	if err != nil {
		return err
	}

	memPage := &MemPage{
		Desc: *pr.page.PageDesc(),
		Data: data,
	}

	openedPage, pageCloser, err := memPage.open(pr.compression)
	if err != nil {
		return fmt.Errorf("opening page for reading: %w", err)
	}

	pr.presenceDec = pr.getPresenceDecoder()
	pr.presenceDec.Reset(openedPage.PresenceData)

	if pr.valuesDec == nil || pr.lastPhysicalType != pr.physicalType || pr.lastEncoding != memPage.Desc.Encoding {
		var ok bool
		pr.valuesDec, ok = newValueDecoder(pr.physicalType, memPage.Desc.Encoding, openedPage.ValueData)
		if !ok {
			return fmt.Errorf("unsupported value encoding %s/%s", pr.physicalType, memPage.Desc.Encoding)
		}
	} else {
		pr.valuesDec.Reset(openedPage.ValueData)
	}

	pr.ready = true
	pr.closer = pageCloser
	pr.lastPhysicalType = pr.physicalType
	pr.lastEncoding = memPage.Desc.Encoding
	pr.pageRow = 0
	return nil
}

// Seek sets the row offset for the next Read call, interpreted according to
// whence:
//
//   - [io.SeekStart] seeks relative to the start of the page,
//   - [io.SeekCurrent] seeks relative to the current offset, and
//   - [io.SeekEnd] seeks relative to the end (for example, offset = -2
//     specifies the penultimate row of the page).
//
// Row offsets are relative to pages and not a column that the page may belong
// to.
//
// Seek returns the new offset relative to the start of the page or an error,
// if any.
//
// To retrieve the current offset without modification, call Seek with 0 and
// [io.SeekCurrent].
//
// Seeking to an offset before the start of the page is an error. Seeking to
// beyond the end of the page will cause the next Read to return [io.EOF].
func (pr *pageReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		if offset < 0 {
			return 0, errors.New("invalid offset")
		}
		pr.nextRow = offset

	case io.SeekCurrent:
		if pr.nextRow+offset < 0 {
			return 0, errors.New("invalid offset")
		}
		pr.nextRow += offset

	case io.SeekEnd:
		lastRow := int64(pr.page.PageDesc().RowCount)
		if lastRow+offset < 0 {
			return 0, errors.New("invalid offset")
		}
		pr.nextRow = lastRow + offset

	default:
		return 0, fmt.Errorf("invalid whence value %d", whence)
	}

	return pr.nextRow, nil
}

// Reset resets the page reader to read from the start of the provided page.
// This permits reusing a page reader rather than allocating a new one.
func (pr *pageReader) Reset(page Page, physicalType datasetmd.PhysicalType, compression datasetmd.CompressionType) {
	pr.page = page
	pr.physicalType = physicalType
	pr.compression = compression
	pr.ready = false

	pr.alloc.Reset()

	if pr.presenceDec != nil {
		pr.presenceDec.Reset(nil)
	}
	if pr.valuesDec != nil {
		pr.valuesDec.Reset(nil)
	}

	pr.pageRow = 0
	pr.nextRow = 0

	// Close the underlying reader if one is open so resources get released
	// sooner.
	_ = pr.Close()
}

// Close closes the pageReader. Closed pageReaders can be reused by calling
// [pageReader.Reset].
func (pr *pageReader) Close() error {
	if pr.closer != nil {
		err := pr.closer.Close()
		pr.closer = nil
		return err
	}
	return nil
}

func (pr *pageReader) getPresenceDecoder() *bitmapDecoder {
	if pr.presenceDec == nil {
		return newBitmapDecoder(nil)
	}
	return pr.presenceDec
}
