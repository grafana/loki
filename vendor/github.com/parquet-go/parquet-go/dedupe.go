package parquet

// DedupeRowReader constructs a row reader which drops duplicated consecutive
// rows, according to the comparator function passed as argument.
//
// If the underlying reader produces a sequence of rows sorted by the same
// comparison predicate, the output is guaranteed to produce unique rows only.
func DedupeRowReader(reader RowReader, compare func(Row, Row) int) RowReader {
	return &dedupeRowReader{reader: reader, compare: compare}
}

type dedupeRowReader struct {
	reader  RowReader
	compare func(Row, Row) int
	dedupe
}

func (d *dedupeRowReader) ReadRows(rows []Row) (int, error) {
	for {
		n, err := d.reader.ReadRows(rows)
		n = d.deduplicate(rows[:n], d.compare)

		if n > 0 || err != nil {
			return n, err
		}
	}
}

// DedupeRowWriter constructs a row writer which drops duplicated consecutive
// rows, according to the comparator function passed as argument.
//
// If the writer is given a sequence of rows sorted by the same comparison
// predicate, the output is guaranteed to contain unique rows only.
func DedupeRowWriter(writer RowWriter, compare func(Row, Row) int) RowWriter {
	return &dedupeRowWriter{writer: writer, compare: compare}
}

type dedupeRowWriter struct {
	writer  RowWriter
	compare func(Row, Row) int
	dedupe
	rows []Row
}

func (d *dedupeRowWriter) WriteRows(rows []Row) (int, error) {
	// We need to make a copy because we cannot modify the rows slice received
	// as argument to respect the RowWriter contract.
	d.rows = append(d.rows[:0], rows...)
	defer func() {
		for i := range d.rows {
			d.rows[i] = Row{}
		}
	}()

	if n := d.deduplicate(d.rows, d.compare); n > 0 {
		w, err := d.writer.WriteRows(d.rows[:n])
		if err != nil {
			return w, err
		}
	}

	// Return the number of rows received instead of the number of deduplicated
	// rows actually written to the underlying writer because we have to repsect
	// the RowWriter contract.
	return len(rows), nil
}

type dedupe struct {
	lastRow Row
	uniq    []Row
	dupe    []Row
}

func (d *dedupe) reset() {
	d.lastRow = d.lastRow[:0]
}

func (d *dedupe) deduplicate(rows []Row, compare func(Row, Row) int) int {
	defer func() {
		for i := range d.uniq {
			d.uniq[i] = Row{}
		}
		for i := range d.dupe {
			d.dupe[i] = Row{}
		}
		d.uniq = d.uniq[:0]
		d.dupe = d.dupe[:0]
	}()

	lastRow := d.lastRow

	for _, row := range rows {
		if len(lastRow) != 0 && compare(row, lastRow) == 0 {
			d.dupe = append(d.dupe, row)
		} else {
			lastRow = row
			d.uniq = append(d.uniq, row)
		}
	}

	rows = rows[:0]
	rows = append(rows, d.uniq...)
	rows = append(rows, d.dupe...)

	d.lastRow = append(d.lastRow[:0], lastRow...)
	return len(d.uniq)
}
