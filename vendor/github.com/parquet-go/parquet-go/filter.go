package parquet

// FilterRowReader constructs a RowReader which exposes rows from reader for
// which the predicate has returned true.
func FilterRowReader(reader RowReader, predicate func(Row) bool) RowReader {
	f := &filterRowReader{reader: reader, predicate: predicate}
	for i := range f.rows {
		f.rows[i] = f.values[i : i : i+1]
	}
	return f
}

type filterRowReader struct {
	reader    RowReader
	predicate func(Row) bool
	rows      [defaultRowBufferSize]Row
	values    [defaultRowBufferSize]Value
}

func (f *filterRowReader) ReadRows(rows []Row) (n int, err error) {
	for n < len(rows) {
		r := len(rows) - n

		if r > len(f.rows) {
			r = len(f.rows)
		}

		r, err = f.reader.ReadRows(f.rows[:r])

		for i := 0; i < r; i++ {
			if f.predicate(f.rows[i]) {
				rows[n] = append(rows[n][:0], f.rows[i]...)
				n++
			}
		}

		if err != nil {
			break
		}
	}
	return n, err
}

// FilterRowWriter constructs a RowWriter which writes rows to writer for which
// the predicate has returned true.
func FilterRowWriter(writer RowWriter, predicate func(Row) bool) RowWriter {
	return &filterRowWriter{writer: writer, predicate: predicate}
}

type filterRowWriter struct {
	writer    RowWriter
	predicate func(Row) bool
	rows      [defaultRowBufferSize]Row
}

func (f *filterRowWriter) WriteRows(rows []Row) (n int, err error) {
	defer func() {
		clear := f.rows[:]
		for i := range clear {
			clearValues(clear[i])
		}
	}()

	for n < len(rows) {
		i := 0
		j := len(rows) - n

		if j > len(f.rows) {
			j = len(f.rows)
		}

		for _, row := range rows[n : n+j] {
			if f.predicate(row) {
				f.rows[i] = row
				i++
			}
		}

		if i > 0 {
			_, err := f.writer.WriteRows(f.rows[:i])
			if err != nil {
				break
			}
		}

		n += j
	}

	return n, err
}
