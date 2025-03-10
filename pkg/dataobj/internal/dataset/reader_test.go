package dataset

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_Reader_ReadAll(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewReader(ReaderOptions{Dataset: dset, Columns: columns})
	defer r.Close()

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_Reader_ReadWithPredicate(t *testing.T) {
	dset, columns := buildTestDataset(t)

	// Create a predicate that only returns people born after 1985
	r := NewReader(ReaderOptions{
		Dataset: dset,
		Columns: columns,
		Predicate: GreaterThanPredicate{
			Column: columns[3], // birth_year column
			Value:  Int64Value(1985),
		},
	})
	defer r.Close()

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)

	// Filter expected data manually to verify
	var expected []testPerson
	for _, p := range basicReaderTestData {
		if p.birthYear > 1985 {
			expected = append(expected, p)
		}
	}
	require.Equal(t, expected, convertToTestPersons(actualRows))
}

// Test_Reader_ReadWithPageFiltering tests that a Reader can filter rows based
// on a predicate that has filtered pages out.
func Test_Reader_ReadWithPageFiltering(t *testing.T) {
	dset, columns := buildTestDataset(t)

	r := NewReader(ReaderOptions{
		Dataset: dset,
		Columns: columns,

		// Henry is out of range of most pages except for the first and last, so
		// other pages would be filtered out of testing.
		//
		// TODO(rfratto): make it easier to prove that a predicate includes a value
		// which is out of range of at least one page.
		Predicate: EqualPredicate{
			Column: columns[0], // first_name column
			Value:  StringValue("Henry"),
		},
	})
	defer r.Close()

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)

	// Filter expected data manually to verify
	var expected []testPerson
	for _, p := range basicReaderTestData {
		if p.firstName == "Henry" {
			expected = append(expected, p)
		}
	}
	require.Equal(t, expected, convertToTestPersons(actualRows))
}

func Test_Reader_ReadWithPredicate_NoSecondary(t *testing.T) {
	dset, columns := buildTestDataset(t)

	// Create a predicate that only returns people born after 1985
	r := NewReader(ReaderOptions{
		Dataset: dset,
		Columns: []Column{columns[3]},
		Predicate: GreaterThanPredicate{
			Column: columns[3], // birth_year column
			Value:  Int64Value(1985),
		},
	})
	defer r.Close()

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)

	// Filter expected data manually to verify
	var expected []int
	for _, p := range basicReaderTestData {
		if p.birthYear > 1985 {
			expected = append(expected, int(p.birthYear))
		}
	}

	var actual []int
	for _, row := range actualRows {
		actual = append(actual, int(row.Values[0].Int64()))
	}
	require.Equal(t, expected, actual)
}

func Test_Reader_Reset(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewReader(ReaderOptions{Dataset: dset, Columns: columns})
	defer r.Close()

	// First read everything
	_, err := readDataset(r, 3)
	require.NoError(t, err)

	// Reset and read again
	r.Reset(ReaderOptions{Dataset: dset, Columns: columns})

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_buildMask(t *testing.T) {
	tt := []struct {
		name      string
		fullRange rowRange
		rows      []Row
		expect    []rowRange
	}{
		{
			name:      "no rows",
			fullRange: rowRange{1, 10},
			rows:      nil,
			expect:    []rowRange{{1, 10}},
		},
		{
			name:      "full coverage",
			fullRange: rowRange{1, 10},
			rows:      makeRows(1, 10, 1),
			expect:    nil,
		},
		{
			name:      "full coverage - split",
			fullRange: rowRange{1, 10},
			rows:      mergeRows(makeRows(1, 5, 1), makeRows(6, 10, 1)),
			expect:    nil,
		},
		{
			name:      "partial coverage - front",
			fullRange: rowRange{1, 10},
			rows:      makeRows(1, 5, 1),
			expect:    []rowRange{{6, 10}},
		},
		{
			name:      "partial coverage - middle",
			fullRange: rowRange{1, 10},
			rows:      makeRows(5, 7, 1),
			expect:    []rowRange{{1, 4}, {8, 10}},
		},
		{
			name:      "partial coverage - end",
			fullRange: rowRange{1, 10},
			rows:      makeRows(6, 10, 1),
			expect:    []rowRange{{1, 5}},
		},
		{
			name:      "partial coverage - gaps",
			fullRange: rowRange{1, 10},
			rows:      []Row{{Index: 3}, {Index: 5}, {Index: 7}, {Index: 9}},
			expect:    []rowRange{{1, 2}, {4, 4}, {6, 6}, {8, 8}, {10, 10}},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual := slices.Collect(buildMask(tc.fullRange, tc.rows))
			require.Equal(t, tc.expect, actual)
		})
	}
}

func makeRows(from, to, inc int) []Row {
	var rows []Row
	for i := from; i <= to; i += inc {
		rows = append(rows, Row{Index: i})
	}
	return rows
}

func mergeRows(rows ...[]Row) []Row {
	var res []Row
	for _, r := range rows {
		res = append(res, r...)
	}
	return res
}

// readDataset reads all rows from a Reader using the given batch size.
func readDataset(br *Reader, batchSize int) ([]Row, error) {
	var (
		all []Row

		batch = make([]Row, batchSize)
	)

	for {
		// Clear the batch for each read, to ensure that any memory in Row and
		// Value doesn't get reused. See comment in implmentation of
		// [readBasicReader] for more information.
		clear(batch)

		n, err := br.Read(context.Background(), batch)
		all = append(all, batch[:n]...)
		if errors.Is(err, io.EOF) {
			return all, nil
		} else if err != nil {
			return all, err
		}
	}
}
