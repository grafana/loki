package parquet

import "io"

// ScanRowReader constructs a RowReader which exposes rows from reader until
// the predicate returns false for one of the rows, or EOF is reached.
func ScanRowReader(reader RowReader, predicate func(Row, int64) bool) RowReader {
	return &scanRowReader{reader: reader, predicate: predicate}
}

type scanRowReader struct {
	reader    RowReader
	predicate func(Row, int64) bool
	rowIndex  int64
}

func (s *scanRowReader) ReadRows(rows []Row) (int, error) {
	if s.rowIndex < 0 {
		return 0, io.EOF
	}

	n, err := s.reader.ReadRows(rows)

	for i, row := range rows[:n] {
		if !s.predicate(row, s.rowIndex) {
			s.rowIndex = -1
			return i, io.EOF
		}
		s.rowIndex++
	}

	return n, err
}
