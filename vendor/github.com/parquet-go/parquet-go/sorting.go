package parquet

import (
	"io"
	"sort"
)

// SortingWriter is a type similar to GenericWriter but it ensures that rows
// are sorted according to the sorting columns configured on the writer.
//
// The writer accumulates rows in an in-memory buffer which is sorted when it
// reaches the target number of rows, then written to a temporary row group.
// When the writer is flushed or closed, the temporary row groups are merged
// into a row group in the output file, ensuring that rows remain sorted in the
// final row group.
//
// Because row groups get encoded and compressed, they hold a lot less memory
// than if all rows were retained in memory. Sorting then merging rows chunks
// also tends to be a lot more efficient than sorting all rows in memory as it
// results in better CPU cache utilization since sorting multi-megabyte arrays
// causes a lot of cache misses since the data set cannot be held in CPU caches.
type SortingWriter[T any] struct {
	rowbuf  *RowBuffer[T]
	writer  *GenericWriter[T]
	output  *GenericWriter[T]
	buffer  io.ReadWriteSeeker
	maxRows int64
	numRows int64
	sorting SortingConfig
	dedupe  dedupe
}

// NewSortingWriter constructs a new sorting writer which writes a parquet file
// where rows of each row group are ordered according to the sorting columns
// configured on the writer.
//
// The sortRowCount argument defines the target number of rows that will be
// sorted in memory before being written to temporary row groups. The greater
// this value the more memory is needed to buffer rows in memory. Choosing a
// value that is too small limits the maximum number of rows that can exist in
// the output file since the writer cannot create more than 32K temporary row
// groups to hold the sorted row chunks.
func NewSortingWriter[T any](output io.Writer, sortRowCount int64, options ...WriterOption) *SortingWriter[T] {
	config, err := NewWriterConfig(options...)
	if err != nil {
		panic(err)
	}
	return &SortingWriter[T]{
		rowbuf: NewRowBuffer[T](&RowGroupConfig{
			Schema:  config.Schema,
			Sorting: config.Sorting,
		}),
		writer: NewGenericWriter[T](io.Discard, &WriterConfig{
			CreatedBy:            config.CreatedBy,
			ColumnPageBuffers:    config.ColumnPageBuffers,
			ColumnIndexSizeLimit: config.ColumnIndexSizeLimit,
			PageBufferSize:       config.PageBufferSize,
			WriteBufferSize:      config.WriteBufferSize,
			DataPageVersion:      config.DataPageVersion,
			Schema:               config.Schema,
			Compression:          config.Compression,
			Sorting:              config.Sorting,
		}),
		output:  NewGenericWriter[T](output, config),
		maxRows: sortRowCount,
		sorting: config.Sorting,
	}
}

func (w *SortingWriter[T]) Close() error {
	if err := w.Flush(); err != nil {
		return err
	}
	return w.output.Close()
}

func (w *SortingWriter[T]) Flush() error {
	defer w.resetSortingBuffer()

	if err := w.sortAndWriteBufferedRows(); err != nil {
		return err
	}

	if w.numRows == 0 {
		return nil
	}

	if err := w.writer.Close(); err != nil {
		return err
	}

	size, err := w.buffer.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	f, err := OpenFile(newReaderAt(w.buffer), size,
		&FileConfig{
			SkipPageIndex:    true,
			SkipBloomFilters: true,
			ReadBufferSize:   defaultReadBufferSize,
		},
	)
	if err != nil {
		return err
	}

	m, err := MergeRowGroups(f.RowGroups(),
		&RowGroupConfig{
			Schema:  w.Schema(),
			Sorting: w.sorting,
		},
	)
	if err != nil {
		return err
	}

	rows := m.Rows()
	defer rows.Close()

	reader := RowReader(rows)
	if w.sorting.DropDuplicatedRows {
		reader = DedupeRowReader(rows, w.rowbuf.compare)
	}

	if _, err := CopyRows(w.output, reader); err != nil {
		return err
	}

	return w.output.Flush()
}

func (w *SortingWriter[T]) Reset(output io.Writer) {
	w.output.Reset(output)
	w.rowbuf.Reset()
	w.resetSortingBuffer()
}

func (w *SortingWriter[T]) resetSortingBuffer() {
	w.writer.Reset(io.Discard)
	w.numRows = 0

	if w.buffer != nil {
		w.sorting.SortingBuffers.PutBuffer(w.buffer)
		w.buffer = nil
	}
}

func (w *SortingWriter[T]) Write(rows []T) (int, error) {
	return w.writeRows(len(rows), func(i, j int) (int, error) { return w.rowbuf.Write(rows[i:j]) })
}

func (w *SortingWriter[T]) WriteRows(rows []Row) (int, error) {
	return w.writeRows(len(rows), func(i, j int) (int, error) { return w.rowbuf.WriteRows(rows[i:j]) })
}

func (w *SortingWriter[T]) writeRows(numRows int, writeRows func(i, j int) (int, error)) (int, error) {
	wn := 0

	for wn < numRows {
		if w.rowbuf.NumRows() >= w.maxRows {
			if err := w.sortAndWriteBufferedRows(); err != nil {
				return wn, err
			}
		}

		n := int(w.maxRows - w.rowbuf.NumRows())
		n += wn
		if n > numRows {
			n = numRows
		}

		n, err := writeRows(wn, n)
		wn += n

		if err != nil {
			return wn, err
		}
	}

	return wn, nil
}

func (w *SortingWriter[T]) SetKeyValueMetadata(key, value string) {
	w.output.SetKeyValueMetadata(key, value)
}

func (w *SortingWriter[T]) Schema() *Schema {
	return w.output.Schema()
}

func (w *SortingWriter[T]) sortAndWriteBufferedRows() error {
	if w.rowbuf.Len() == 0 {
		return nil
	}

	defer w.rowbuf.Reset()
	sort.Sort(w.rowbuf)

	if w.sorting.DropDuplicatedRows {
		w.rowbuf.rows = w.rowbuf.rows[:w.dedupe.deduplicate(w.rowbuf.rows, w.rowbuf.compare)]
		defer w.dedupe.reset()
	}

	rows := w.rowbuf.Rows()
	defer rows.Close()

	if w.buffer == nil {
		w.buffer = w.sorting.SortingBuffers.GetBuffer()
		w.writer.Reset(w.buffer)
	}

	n, err := CopyRows(w.writer, rows)
	if err != nil {
		return err
	}

	if err := w.writer.Flush(); err != nil {
		return err
	}

	w.numRows += n
	return nil
}
