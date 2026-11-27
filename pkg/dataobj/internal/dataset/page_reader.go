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

	pageRow int64 // Number of rows of the page that the decoders have read or skipped.
	nextRow int64 // Row of the page where the next Read starts. Seek and Read move it.

	rowCount int64 // Number of rows that the descriptor of the page declares.
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
// A corrupt object can have a page with a different number of rows than its
// descriptor declares. Read then returns an error that wraps
// [io.ErrUnexpectedEOF]. Read finds the difference only when it reaches the
// declared row count or the end of the page.
//
// If there was an error reading the page, Read returns the error with
// no array. After an error other than [io.EOF], a retry of Read reads the same
// rows again.
func (pr *pageReader) Read(ctx context.Context, alloc *memory.Allocator, count int) (arr columnar.Array, err error) {
	// A failed read can leave the decoders ahead of pageRow. This deferred reset
	// makes the next call initialize the reader again. A retry then reads the
	// same rows again and never returns wrong rows.
	defer func() {
		if err != nil && !errors.Is(err, io.EOF) {
			pr.ready = false
		}
	}()

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
	arr, err = pr.readColumnar(alloc, count, false)
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

	// Skipping allocates only the presence bitmap, because the decoders move past
	// the values without building arrays. A short-lived child allocator would add
	// allocation overhead and free nothing worth reusing.
	readCount := int(pr.nextRow - pr.pageRow)
	_, err := pr.readColumnar(alloc, readCount, true)
	return err
}

// readColumnar implements the actual Read operation for count rows. If skip is
// true, readColumnar moves past the rows and returns a nil array.
func (pr *pageReader) readColumnar(alloc *memory.Allocator, count int, skip bool) (columnar.Array, error) {
	// First read presence values for the next count rows.
	bm := memory.NewBitmap(alloc, count)
	err := pr.presenceDec.DecodeTo(&bm, count)

	gotCount := bm.Len()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	if rowCountErr := pr.checkRowCount(gotCount, errors.Is(err, io.EOF)); rowCountErr != nil {
		return nil, rowCountErr
	}

	if gotCount == 0 && errors.Is(err, io.EOF) {
		// If we've hit EOF, we can immediately close the inner reader to release
		// any resources back, rather than waiting for the next call to
		// [pageReader.init] to do it.
		_ = pr.Close()

		return nil, io.EOF
	} else if gotCount == 0 {
		return nil, nil
	}

	// The number of bits set to 1 in bm determines how many values we need to
	// read or skip in the inner page.
	presentCount := bm.SetCount()

	var values columnar.Array

	// Read or skip the presentCount concrete values.
	if presentCount > 0 {
		if skip {
			skipped, skipErr := pr.valuesDec.Skip(presentCount)
			if skipErr != nil {
				return nil, skipErr
			} else if skipped != presentCount {
				return nil, fmt.Errorf("unexpected number of skipped values: %d, expected: %d", skipped, presentCount)
			}
		} else {
			values, err = pr.valuesDec.Decode(alloc, presentCount)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			} else if values == nil {
				return nil, fmt.Errorf("unexpected nil values")
			} else if values.Len() != presentCount {
				return nil, fmt.Errorf("unexpected number of values: %d, expected: %d", values.Len(), presentCount)
			}
		}
	}

	pr.pageRow += int64(gotCount)

	if skip {
		return nil, nil
	}
	return materializeSparseArray(alloc, pr.lastPhysicalType, bm, values)
}

// checkRowCount returns an error if the presence bitmap and the page descriptor
// disagree on the row count.
//
// decoded is the number of rows that the last decode of the presence bitmap
// wrote. hitEOF reports whether that decode returned [io.EOF]. That happens
// when the bitmap ends before the decode gets all the rows it asked for.
//
// checkRowCount finds a mismatch only when a read reaches or exceeds the
// declared row count, or hits [io.EOF]. A read that ends before the declared
// row count and before the end of the bitmap raises no error.
func (pr *pageReader) checkRowCount(decoded int, hitEOF bool) error {
	// The page checksum covers the page bytes and not the descriptor. A
	// descriptor with a wrong row count passes the checksum. The column reader
	// derives the row ranges of the pages from the descriptor counts, so every
	// later page shifts.
	//
	// If the checksum is in the page, a descriptor that points to another page
	// also passes the checksum.
	//
	// The check cannot detect a descriptor that points to another page with the
	// same row count.

	rows := pr.pageRow + int64(decoded)

	switch {
	// A decode that stops exactly at the declared row count gets no io.EOF,
	// even when the bitmap holds more rows. Exhausted finds those rows.
	case rows > pr.rowCount, rows == pr.rowCount && !pr.presenceDec.Exhausted():
		return fmt.Errorf("page has more rows than the %d in its descriptor: %w", pr.rowCount, io.ErrUnexpectedEOF)

	case rows < pr.rowCount && hitEOF:
		return fmt.Errorf("page has %d rows, fewer than the %d in its descriptor: %w", rows, pr.rowCount, io.ErrUnexpectedEOF)
	}
	return nil
}

func (pr *pageReader) init(ctx context.Context) error {
	// The close below releases the buffer that the decoders read from. A failure
	// before the end of init must not leave the reader ready on that buffer.
	pr.ready = false

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
	pr.rowCount = int64(memPage.Desc.RowCount)
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
	case *columnar.Number[int32]:
		return materializeSparseNumber[int32](alloc, validity, arr)
	case *columnar.Number[int64]:
		return materializeSparseNumber[int64](alloc, validity, arr)
	case *columnar.Number[uint32]:
		return materializeSparseNumber[uint32](alloc, validity, arr)
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
	pr.rowCount = 0

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
