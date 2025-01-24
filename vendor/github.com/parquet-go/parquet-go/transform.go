package parquet

// TransformRowReader constructs a RowReader which applies the given transform
// to each row rad from reader.
//
// The transformation function appends the transformed src row to dst, returning
// dst and any error that occurred during the transformation. If dst is returned
// unchanged, the row is skipped.
func TransformRowReader(reader RowReader, transform func(dst, src Row) (Row, error)) RowReader {
	return &transformRowReader{reader: reader, transform: transform}
}

type transformRowReader struct {
	reader    RowReader
	transform func(Row, Row) (Row, error)
	rows      []Row
	offset    int
	length    int
}

func (t *transformRowReader) ReadRows(rows []Row) (n int, err error) {
	if len(t.rows) == 0 {
		t.rows = makeRows(len(rows))
	}

	for {
		for n < len(rows) && t.offset < t.length {
			dst := rows[n][:0]
			src := t.rows[t.offset]
			rows[n], err = t.transform(dst, src)
			if err != nil {
				return n, err
			}
			clearValues(src)
			t.rows[t.offset] = src[:0]
			t.offset++
			n++
		}

		if n == len(rows) {
			return n, nil
		}

		r, err := t.reader.ReadRows(t.rows)
		if r == 0 && err != nil {
			return n, err
		}
		t.offset = 0
		t.length = r
	}
}

type transformRowBuffer struct {
	buffer []Row
	offset int32
	length int32
}

func (b *transformRowBuffer) init(n int) {
	b.buffer = makeRows(n)
	b.offset = 0
	b.length = 0
}

func (b *transformRowBuffer) discard() {
	row := b.buffer[b.offset]
	clearValues(row)
	b.buffer[b.offset] = row[:0]

	if b.offset++; b.offset == b.length {
		b.reset(0)
	}
}

func (b *transformRowBuffer) reset(n int) {
	b.offset = 0
	b.length = int32(n)
}

func (b *transformRowBuffer) rows() []Row {
	return b.buffer[b.offset:b.length]
}

func (b *transformRowBuffer) cap() int {
	return len(b.buffer)
}

func (b *transformRowBuffer) len() int {
	return int(b.length - b.offset)
}

// TransformRowWriter constructs a RowWriter which applies the given transform
// to each row writter to writer.
//
// The transformation function appends the transformed src row to dst, returning
// dst and any error that occurred during the transformation. If dst is returned
// unchanged, the row is skipped.
func TransformRowWriter(writer RowWriter, transform func(dst, src Row) (Row, error)) RowWriter {
	return &transformRowWriter{writer: writer, transform: transform}
}

type transformRowWriter struct {
	writer    RowWriter
	transform func(Row, Row) (Row, error)
	rows      []Row
}

func (t *transformRowWriter) WriteRows(rows []Row) (n int, err error) {
	if len(t.rows) == 0 {
		t.rows = makeRows(len(rows))
	}

	for n < len(rows) {
		numRows := len(rows) - n
		if numRows > len(t.rows) {
			numRows = len(t.rows)
		}
		if err := t.writeRows(rows[n : n+numRows]); err != nil {
			return n, err
		}
		n += numRows
	}

	return n, nil
}

func (t *transformRowWriter) writeRows(rows []Row) (err error) {
	numRows := 0
	defer func() { clearRows(t.rows[:numRows]) }()

	for _, row := range rows {
		t.rows[numRows], err = t.transform(t.rows[numRows][:0], row)
		if err != nil {
			return err
		}
		if len(t.rows[numRows]) != 0 {
			numRows++
		}
	}

	_, err = t.writer.WriteRows(t.rows[:numRows])
	return err
}
