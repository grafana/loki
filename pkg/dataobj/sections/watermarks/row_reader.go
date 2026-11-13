package watermarks

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/slicegrow"
)

// RowReader reads the watermarks of one [Section].
type RowReader struct {
	sec   *Section
	ready bool
	empty bool // The section has no columns, so there is nothing to read.

	buf []dataset.Row

	reader *dataset.RowReader
}

var errRowReaderNotOpen = errors.New("row reader not opened")

// NewRowReader creates a new RowReader for the given section.
//
// Call [RowReader.Open] before calling [RowReader.Read].
func NewRowReader(sec *Section) *RowReader {
	var r RowReader
	r.Reset(sec)
	return &r
}

// Open initializes RowReader resources.
//
// Open must be called before [RowReader.Read]. Open is safe to call multiple
// times. Open is a no-op when the reader has no section.
func (r *RowReader) Open(ctx context.Context) error {
	if r.sec == nil || r.ready {
		return nil
	}

	if err := r.initReader(ctx); err != nil {
		return errors.Join(fmt.Errorf("initializing row reader: %w", err), r.Close())
	}
	return nil
}

// Read reads up to the next len(s) watermarks from the reader and stores them
// into s. It returns the number of watermarks read and any error encountered.
// At the end of the watermarks section, Read returns 0, io.EOF.
//
// Read returns io.EOF when the reader has no section. It returns an error if
// the reader has a section but [RowReader.Open] did not succeed. If a row fails
// to decode, Read returns the number of rows decoded before it.
func (r *RowReader) Read(ctx context.Context, s []Watermark) (int, error) {
	if r.sec == nil {
		return 0, io.EOF
	}

	if !r.ready {
		return 0, errRowReaderNotOpen
	}
	if r.empty {
		return 0, io.EOF
	}

	r.buf = slicegrow.GrowToCap(r.buf, len(s))
	r.buf = r.buf[:len(s)]
	n, err := r.reader.Read(ctx, r.buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, fmt.Errorf("reading rows: %w", err)
	} else if n == 0 && errors.Is(err, io.EOF) {
		return 0, io.EOF
	}

	for i := range r.buf[:n] {
		if err := decodeRow(r.sec.Columns(), r.buf[i], &s[i]); err != nil {
			return i, fmt.Errorf("decoding watermark: %w", err)
		}
	}

	return n, nil
}

func (r *RowReader) initReader(ctx context.Context) error {
	if r.sec.isEmpty() {
		r.empty = true
		r.ready = true
		return nil
	}
	r.empty = false

	dset, err := r.sec.makeDataset()
	if err != nil {
		return fmt.Errorf("creating section dataset: %w", err)
	}

	readerOpts := dataset.RowReaderOptions{
		Dataset:           dset,
		ProjectedColumns:  dset.Columns(),
		PrefetchAllOnOpen: true,
	}

	if r.reader == nil {
		r.reader = dataset.NewRowReader(readerOpts)
	} else {
		r.reader.Reset(readerOpts)
	}
	if err := r.reader.Open(ctx); err != nil {
		return fmt.Errorf("opening row reader: %w", err)
	}

	r.ready = true
	return nil
}

// Reset resets the RowReader with a new section to read from. Reset allows
// reusing a RowReader without allocating a new one.
//
// Reset may be called with a nil section to clear the RowReader without
// needing a new section.
func (r *RowReader) Reset(sec *Section) {
	r.sec = sec
	r.ready = false
}

// Close closes the RowReader and releases any resources it holds. Closed
// RowReaders can be reused by calling [RowReader.Reset].
func (r *RowReader) Close() error {
	r.ready = false
	r.empty = false
	if r.reader != nil {
		return r.reader.Close()
	}
	return nil
}
