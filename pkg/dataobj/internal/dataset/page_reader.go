package dataset

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
)

type pageReader struct {
	page        Page
	value       datasetmd.ValueType
	compression datasetmd.CompressionType
	ready       bool // Whether the pageReader is initialized for page.

	lastValue    datasetmd.ValueType
	lastEncoding datasetmd.EncodingType

	closer      io.Closer
	presenceDec *bitmapDecoder
	valuesDec   valueDecoder

	presenceBuf []Value
	valuesBuf   []Value

	pageRow int64
	nextRow int64

	presenceReader *bufio.Reader
	valuesReader   *bufio.Reader
}

// newPageReader returns a new pageReader that reads from the provided page.
// The page must hold values of the provided value type, and be compressed with
// the provided compression type.
func newPageReader(p Page, value datasetmd.ValueType, compression datasetmd.CompressionType) *pageReader {
	var pr pageReader
	pr.Reset(p, value, compression)
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
	pr.presenceBuf = slicegrow.GrowToCap(pr.presenceBuf, len(v))
	pr.presenceBuf = pr.presenceBuf[:len(v)]

	// We want to allow decoders to reuse memory of [Value]s in v while allowing
	// the caller to retain ownership over that memory; to do this safely, we
	// copy memory from v into pr.valuesBuf for our decoders to use.
	//
	// If we didn't do this, then memory backing [Value]s are owned by both
	// pageReader and the caller, which can lead to memory reuse bugs.
	pr.valuesBuf = reuseValuesBuffer(pr.valuesBuf, v)

	// First read presence values for the next len(v) rows.
	count, err := pr.presenceDec.Decode(pr.presenceBuf)
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

	// The number of values in pr.presenceBuf[:count] which are set to 1
	// determines how many values we need to read from the inner page.
	var presentCount int
	for _, p := range pr.presenceBuf[:count] {
		if p.Type() != datasetmd.VALUE_TYPE_UINT64 {
			return n, fmt.Errorf("unexpected presence type: %s", p.Type())
		}
		if p.Uint64() == 1 {
			presentCount++
		}
	}

	// Now fill up to prescentCount values of concrete values.
	var valuesCount int
	if presentCount > 0 {
		valuesCount, err = pr.valuesDec.Decode(pr.valuesBuf[:presentCount])
		if err != nil {
			return n, err
		} else if valuesCount != presentCount {
			return n, fmt.Errorf("unexpected number of values: %d", valuesCount)
		}
	}

	// Finally, copy over count values into v, setting NULL where appropriate and
	// copying from pr.valuesBuf where appropriate.
	var valuesIndex int
	for i, p := range pr.presenceBuf[:count] {
		// Type checking on presence values was already done above; we can call
		// [Value.Uint64] here safely.
		switch p.Uint64() {
		case 1:
			if valuesIndex >= valuesCount {
				return n, fmt.Errorf("unexpected end of values")
			}
			v[i] = pr.valuesBuf[valuesIndex]
			valuesIndex++
		default:
			v[i] = Value{}
		}
	}

	n += count
	pr.pageRow += int64(count)
	return n, nil
}

// reuseValuesBuffer prepares dst for reading up to len(src) values. Non-NULL
// values are appended to dst, with the remainder of the slice set to NULL.
//
// The resulting slice is len(src).
func reuseValuesBuffer(dst []Value, src []Value) []Value {
	dst = slicegrow.GrowToCap(dst, len(src))
	dst = dst[:0]

	// We must maintain ordering against the caller slice here.
	// Otherwise we can move pointers around which can get reused within a read call.
	dst = append(dst, src...)

	filledLength := len(dst)

	dst = dst[:len(src)]
	clear(dst[filledLength:])
	return dst
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
		Info: *pr.page.PageInfo(),
		Data: data,
	}

	presenceReader, valuesReader, err := memPage.reader(pr.compression)
	if err != nil {
		return fmt.Errorf("opening page for reading: %w", err)
	}

	pr.presenceReader = pr.getPresenceReader()
	pr.presenceReader.Reset(presenceReader)
	pr.presenceDec = pr.getPresenceDecoder()
	pr.presenceDec.Reset(pr.presenceReader)

	pr.valuesReader = pr.getValuesReader()
	pr.valuesReader.Reset(valuesReader)
	if pr.valuesDec == nil || pr.lastValue != pr.value || pr.lastEncoding != memPage.Info.Encoding {
		var ok bool
		pr.valuesDec, ok = newValueDecoder(pr.value, memPage.Info.Encoding, pr.valuesReader)
		if !ok {
			return fmt.Errorf("unsupported value encoding %s/%s", pr.value, memPage.Info.Encoding)
		}
	} else {
		pr.valuesDec.Reset(pr.valuesReader)
	}

	pr.ready = true
	pr.closer = valuesReader
	pr.lastValue = pr.value
	pr.lastEncoding = memPage.Info.Encoding
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
		lastRow := int64(pr.page.PageInfo().RowCount)
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
func (pr *pageReader) Reset(page Page, value datasetmd.ValueType, compression datasetmd.CompressionType) {
	pr.page = page
	pr.value = value
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

func (pr *pageReader) getPresenceReader() *bufio.Reader {
	if pr.presenceReader == nil {
		return bufio.NewReader(nil)
	}
	return pr.presenceReader
}

func (pr *pageReader) getPresenceDecoder() *bitmapDecoder {
	if pr.presenceDec == nil {
		return newBitmapDecoder(nil)
	}
	return pr.presenceDec
}

func (pr *pageReader) getValuesReader() *bufio.Reader {
	if pr.valuesReader == nil {
		return bufio.NewReader(nil)
	}
	return pr.valuesReader
}
