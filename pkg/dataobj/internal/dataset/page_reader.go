package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

type pageReader struct {
	page         Page
	physicalType datasetmd.PhysicalType
	compression  datasetmd.CompressionType
	ready        bool // Whether the pageReader is initialized for page.

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

// Read returns an array of up to the next count values from the page.
// At the end of the page, Read returns nil, io.EOF.
//
// If there was an error reading the page, Read returns the error with
// no array.
func (pr *pageReader) Read(ctx context.Context, alloc *memory.Allocator, count int) (columnar.Array, error) {
	// We need to initialize our readers before we can read from the page.
	//
	// If we've seeked backwards and our page row is now ahead of the row we want
	// to read, we need to reinitialize the page reader to read from the start of
	// the page.
	if !pr.ready || pr.pageRow > pr.nextRow {
		err := pr.init(ctx)
		if err != nil {
			return nil, err
		}
	}

	// "Skip" rows until we reach the starting row we want to read.
	if err := pr.skipUnwantedRows(alloc); err != nil {
		return nil, err
	}

	// Do the real read now.
	arr, err := pr.readColumnar(alloc, count, false)
	if arr != nil {
		pr.nextRow += int64(arr.Len())
	}
	return arr, err
}

func (pr *pageReader) skipUnwantedRows(alloc *memory.Allocator) error {
	if pr.pageRow >= pr.nextRow {
		// Nothing to skip.
		return nil
	}

	// Since we don't need the values to live beyond this read call, we can
	// create a short-lived allocator. This will also allow the "real" read to
	// reuse any memory that was created during this step.
	tempAlloc := memory.NewAllocator(alloc)
	defer tempAlloc.Free()

	readCount := int(pr.nextRow - pr.pageRow)
	_, err := pr.readColumnar(alloc, readCount, true)
	return err
}

// readColumnar implements the actual Read operation for count rows. If skip is
// true, no array values are returned, permitting for skipping expensive work.
func (pr *pageReader) readColumnar(alloc *memory.Allocator, count int, skip bool) (columnar.Array, error) {
	// First read presence values for the next count rows.
	bm := memory.NewBitmap(alloc, count)
	err := pr.presenceDec.DecodeTo(&bm, count)

	gotCount := bm.Len()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	} else if gotCount == 0 && errors.Is(err, io.EOF) {
		// If we've hit EOF, we can immediately close the inner reader to release
		// any resources back, rather than waiting for the next call to
		// [pageReader.init] to do it.
		_ = pr.Close()

		return nil, io.EOF
	} else if gotCount == 0 {
		return nil, nil
	}

	// The number of bits set to 1 in presenceBuf determines how many values we
	// need to read from the inner page.
	presentCount := bm.SetCount()

	var values columnar.Array

	// Now fill up to presentCount values of concrete values.
	if presentCount > 0 {
		// TODO(rfratto): Add a "skip" mode to decoders to allow them to bypass
		// building an array if it's not going to be used.
		values, err = pr.valuesDec.Decode(alloc, presentCount)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		} else if values == nil {
			return nil, fmt.Errorf("unexpected nil values")
		} else if values.Len() != presentCount {
			return nil, fmt.Errorf("unexpected number of values: %d, expected: %d", values.Len(), presentCount)
		}
	}

	pr.pageRow += int64(gotCount)

	if skip {
		return nil, nil
	}
	return materializeSparseArray(alloc, pr.lastPhysicalType, bm, values)
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

// materializeSparseArray materializes a dense array into a sparse [Value] slice
// based on a presence bitmap. If denseValues is nil, a [columnar.Null] is
// returned with the length of validity.
//
// If denseValues is non-nil, denseValues.Len() must be equal to
// validity.ClearCount().
//
// # Safety
//
// Memory from validity and denseValues may be moved to the returned array.
// These values must have been allocated with alloc to prevent use-after-free.
func materializeSparseArray(alloc *memory.Allocator, typ datasetmd.PhysicalType, validity memory.Bitmap, denseValues columnar.Array) (columnar.Array, error) {
	if denseValues != nil && validity.SetCount() != denseValues.Len() {
		panic(fmt.Sprintf("invariant broken: validity set count (%d) is not array length (%d)", validity.SetCount(), denseValues.Len()))
	}

	switch arr := denseValues.(type) {
	case *columnar.UTF8:
		return materializeSparseUTF8(alloc, validity, arr)
	case *columnar.Number[int64]:
		return materializeSparseNumber[int64](alloc, validity, arr)
	case *columnar.Number[uint64]:
		return materializeSparseNumber[uint64](alloc, validity, arr)
	case nil:
		return materializeNulls(alloc, typ, validity)
	default:
		panic(fmt.Sprintf("found unexpected type %T", arr))
	}
}

func materializeSparseUTF8(alloc *memory.Allocator, validity memory.Bitmap, denseValues *columnar.UTF8) (columnar.Array, error) {
	// The data buffer can remain the same, but we need to make a new offsets
	// buffer to account for all the nulls.
	offsetsBuf := memory.NewBuffer[int32](alloc, validity.Len()+1)
	offsetsBuf.Resize(validity.Len() + 1)
	offsets := offsetsBuf.Data()

	// Since we're moving the data directly from the dense values array, our
	// offsets need to start whenever the source offsets starts. Based on our
	// decoders, this will always be 0, but we keep this logic here to be
	// defensive.
	srcOffsets := denseValues.Offsets()
	offsets[0] = srcOffsets[0]

	var (
		denseIndex = 0
		lastOffset = offsets[0]
	)

	for i := range validity.Len() {
		if !validity.Get(i) {
			offsets[i+1] = lastOffset
			continue
		}

		// Find the end offset to push from the src.
		srcEnd := srcOffsets[denseIndex+1]
		denseIndex++

		offsets[i+1] = srcEnd
		lastOffset = srcEnd
	}

	return columnar.NewUTF8(denseValues.Data(), offsets, validity), nil
}

func materializeSparseNumber[T columnar.Numeric](alloc *memory.Allocator, validity memory.Bitmap, denseValues *columnar.Number[T]) (columnar.Array, error) {
	valuesBuf := memory.NewBuffer[T](alloc, validity.Len())
	valuesBuf.Resize(validity.Len())
	values := valuesBuf.Data()

	srcValues := denseValues.Values()

	var srcIndex int
	for i := range validity.Len() {
		if !validity.Get(i) {
			continue
		}
		values[i] = srcValues[srcIndex]
		srcIndex++
	}
	return columnar.NewNumber[T](values, validity), nil
}

func materializeNulls(alloc *memory.Allocator, typ datasetmd.PhysicalType, validity memory.Bitmap) (columnar.Array, error) {
	if validity.SetCount() > 0 {
		panic(fmt.Sprintf("unexpected non-null values: %d", validity.SetCount()))
	}

	// NOTE(rfratto): we need to return an array of the expected type here,
	// since other operations will require all arrays to be of the same type.
	//
	// TODO(rfratto): Should we update functions like [columnar.Concat] to
	// accept some of the arrays being Null? Would that slow things down too
	// much?
	switch typ {
	case datasetmd.PHYSICAL_TYPE_INT64:
		valuesBuffer := memory.NewBuffer[int64](alloc, validity.Len())
		valuesBuffer.Resize(validity.Len())
		valuesBuffer.Clear()

		return columnar.NewNumber[int64](valuesBuffer.Data(), validity), nil

	case datasetmd.PHYSICAL_TYPE_UINT64:
		valuesBuffer := memory.NewBuffer[uint64](alloc, validity.Len())
		valuesBuffer.Resize(validity.Len())
		valuesBuffer.Clear()

		return columnar.NewNumber[uint64](valuesBuffer.Data(), validity), nil

	case datasetmd.PHYSICAL_TYPE_BINARY:
		offsetsBuffer := memory.NewBuffer[int32](alloc, validity.Len()+1)
		offsetsBuffer.Resize(validity.Len() + 1)
		offsetsBuffer.Clear()

		return columnar.NewUTF8(nil, offsetsBuffer.Data(), validity), nil

	default:
		return columnar.NewNull(validity), nil
	}
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
