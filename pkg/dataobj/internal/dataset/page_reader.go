package dataset

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
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

	pageRow int64
	nextRow int64
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
		_, err := pr.read(ctx, v[:maxCount])
		if err != nil {
			return n, err
		}
	}

	n, err = pr.read(ctx, v)
	pr.nextRow += int64(n)
	return n, err
}

// read reads up to the next len(v) values from the page into v, without
// considering the current row offset.
//
// read advances pr.pageRow but not pr.nextRow.
func (pr *pageReader) read(ctx context.Context, v []Value) (n int, err error) {
	// TODO(rfratto):
	//
	// * Allow batching reads from the presence and value decoders.
	// * Allow reusing any allocated memory in v[i] if relevant (such as byte
	//   slices).
	for range len(v) {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}

		var value Value

		present, err := pr.presenceDec.Decode()
		if errors.Is(err, io.EOF) {
			return n, io.EOF
		} else if err != nil {
			return n, fmt.Errorf("decoding presence: %w", err)
		} else if present.Type() != datasetmd.VALUE_TYPE_UINT64 {
			return n, fmt.Errorf("unexpected presence type: %s", present.Type())
		}

		// value is currently nil. If the presence bitmap says our row has a
		// value, we decode it into value.
		if present.Uint64() == 1 {
			value, err = pr.valuesDec.Decode()
			if err != nil {
				return n, fmt.Errorf("decoding expected value: %w", err)
			}
		}

		v[n] = value
		n++
		pr.pageRow++
	}

	return n, nil
}

func (pr *pageReader) init(ctx context.Context) error {
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

	// Close any existing reader from a previous pageReader init.
	if err := pr.Close(); err != nil {
		return fmt.Errorf("closing previous page: %w", err)
	}

	if pr.presenceDec == nil {
		pr.presenceDec = newBitmapDecoder(bufio.NewReader(presenceReader))
	} else {
		pr.presenceDec.Reset(bufio.NewReader(presenceReader))
	}

	if pr.valuesDec == nil || pr.lastValue != pr.value || pr.lastEncoding != memPage.Info.Encoding {
		var ok bool
		pr.valuesDec, ok = newValueDecoder(pr.value, memPage.Info.Encoding, bufio.NewReader(valuesReader))
		if !ok {
			return fmt.Errorf("unsupported value encoding %s/%s", pr.value, memPage.Info.Encoding)
		}
	} else {
		pr.valuesDec.Reset(bufio.NewReader(valuesReader))
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
