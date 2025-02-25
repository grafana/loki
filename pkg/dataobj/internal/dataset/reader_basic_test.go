package dataset

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

type testPerson struct {
	firstName  string
	middleName string // May be empty
	lastName   string
	birthYear  int64
}

var basicReaderTestData = []testPerson{
	{"John", "Robert", "Smith", 1980},
	{"Jane", "", "Doe", 1985},
	{"Alice", "Marie", "Johnson", 1990},
	{"Bob", "", "Williams", 1975},
	{"Carol", "Lynn", "Brown", 1982},
	{"David", "", "Miller", 1988},
	{"Eve", "Grace", "Davis", 1979},
	{"Frank", "", "Wilson", 1983},
	{"Grace", "Elizabeth", "Taylor", 1987},
	{"Henry", "", "Anderson", 1981},
}

func Test_basicReader_ReadAll(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 4)

	br := newBasicReader(columns)
	defer br.Close()

	actualRows, err := readBasicReader(br, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_basicReader_ReadFromOffset(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 4)

	br := newBasicReader(columns)
	defer br.Close()

	// Seek to row 4
	_, err := br.Seek(4, io.SeekStart)
	require.NoError(t, err)

	actualRows, err := readBasicReader(br, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData[4:], convertToTestPersons(actualRows))
}

func Test_basicReader_SeekToStart(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 4)

	br := newBasicReader(columns)
	defer br.Close()

	// First read everything
	_, err := readBasicReader(br, 3)
	require.NoError(t, err)

	// Seek back to start and read again
	_, err = br.Seek(0, io.SeekStart)
	require.NoError(t, err)

	actualRows, err := readBasicReader(br, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_basicReader_ReadColumns(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 4)

	br := newBasicReader(columns)
	defer br.Close()

	// Read only birth_year and middle_name columns (indices 3 and 1)
	subset := []Column{columns[3], columns[1]}

	var (
		batch = make([]Row, 3)
		all   []Row
	)

	for {
		// Clear the batch before reading the next set of values. See
		// implementation of readBasicReader for more details.
		clear(batch)

		n, err := br.ReadColumns(context.Background(), subset, batch)
		for _, row := range batch[:n] {
			// Verify that the row has space for all columns
			require.Len(t, row.Values, 4)

			// Verify that unread columns (first_name and last_name) are nil
			require.True(t, row.Values[0].IsNil(), "first_name should be nil")
			require.True(t, row.Values[2].IsNil(), "last_name should be nil")

			// Verify that read columns match the test data
			testPerson := basicReaderTestData[row.Index]
			if testPerson.middleName != "" {
				require.Equal(t, testPerson.middleName, row.Values[1].String(), "middle_name mismatch")
			} else {
				require.True(t, row.Values[1].IsNil(), "middle_name should be nil")
			}
			require.Equal(t, testPerson.birthYear, row.Values[3].Int64(), "birth_year mismatch")

			all = append(all, row)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	require.Len(t, all, len(basicReaderTestData))
}

func Test_basicReader_Fill(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 4)

	br := newBasicReader(columns)
	defer br.Close()

	// Create rows with specific indices we want to fill
	buf := []Row{
		{Index: 1},
		{Index: 3},
		{Index: 5},
		{Index: 7},
		{Index: 9},
	}

	// Fill only the firstName column
	n, err := br.Fill(context.Background(), []Column{columns[0]}, buf)
	require.NoError(t, err)
	require.Equal(t, len(buf), n)

	// Verify the filled values
	for _, row := range buf {
		// Check that only firstName is filled
		require.False(t, row.Values[0].IsNil(), "firstName should not be nil")
		require.True(t, row.Values[1].IsNil(), "middleName should be nil")
		require.True(t, row.Values[2].IsNil(), "lastName should be nil")
		require.True(t, row.Values[3].IsNil(), "birthYear should be nil")

		// Verify the firstName value
		expectedPerson := basicReaderTestData[row.Index]
		require.Equal(t, expectedPerson.firstName, row.Values[0].String(),
			"firstName mismatch at index %d", row.Index)
	}
}

func Test_partitionRows(t *testing.T) {
	tt := []struct {
		name   string
		in     []int
		expect [][]int
	}{
		{
			name:   "empty",
			in:     nil,
			expect: nil,
		},
		{
			name:   "contiguous range",
			in:     []int{1, 2, 3, 4, 5},
			expect: [][]int{{1, 2, 3, 4, 5}},
		},
		{
			name:   "split range",
			in:     []int{1, 2, 4, 5},
			expect: [][]int{{1, 2}, {4, 5}},
		},
		{
			name:   "single rows",
			in:     []int{1, 3, 5, 7},
			expect: [][]int{{1}, {3}, {5}, {7}},
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]Row, len(tc.in))
			for i, idx := range tc.in {
				rows[i].Index = idx
			}

			var actual [][]int
			for part := range partitionRows(rows) {
				var actualRows []int
				for _, row := range part {
					actualRows = append(actualRows, row.Index)
				}
				actual = append(actual, actualRows)
			}

			require.Equal(t, tc.expect, actual)
		})
	}

}

func Test_basicReader_Reset(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 4)

	br := newBasicReader(columns)
	defer br.Close()

	// First read everything
	_, err := readBasicReader(br, 3)
	require.NoError(t, err)

	// Reset and read again
	br.Reset(columns)

	actualRows, err := readBasicReader(br, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

// buildTestColumns creates a set of columns with test data.
func buildTestColumns(t *testing.T) []Column {
	t.Helper()

	_, cols := buildTestDataset(t)
	return cols
}

// buildTestDataset creates a set of columns with test data.
func buildTestDataset(t *testing.T) (Dataset, []Column) {
	t.Helper()

	// Create builders for each column
	firstNameBuilder := buildStringColumn(t, "first_name")
	middleNameBuilder := buildStringColumn(t, "middle_name")
	lastNameBuilder := buildStringColumn(t, "last_name")
	birthYearBuilder := buildInt64Column(t, "birth_year")

	// Add data to each column
	for i, p := range basicReaderTestData {
		require.NoError(t, firstNameBuilder.Append(i, StringValue(p.firstName)))
		require.NoError(t, middleNameBuilder.Append(i, StringValue(p.middleName)))
		require.NoError(t, lastNameBuilder.Append(i, StringValue(p.lastName)))
		require.NoError(t, birthYearBuilder.Append(i, Int64Value(p.birthYear)))
	}

	// Flush all columns
	firstName, err := firstNameBuilder.Flush()
	require.NoError(t, err)
	middleName, err := middleNameBuilder.Flush()
	require.NoError(t, err)
	lastName, err := lastNameBuilder.Flush()
	require.NoError(t, err)
	birthYear, err := birthYearBuilder.Flush()
	require.NoError(t, err)

	dset := FromMemory([]*MemColumn{firstName, middleName, lastName, birthYear})

	cols, err := result.Collect(dset.ListColumns(context.Background()))
	require.NoError(t, err)

	return dset, cols
}

func buildStringColumn(t *testing.T, name string) *ColumnBuilder {
	t.Helper()

	builder, err := NewColumnBuilder(name, BuilderOptions{
		PageSizeHint: 16, // Small page size to force multiple pages
		Value:        datasetmd.VALUE_TYPE_STRING,
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_PLAIN,

		Statistics: StatisticsOptions{StoreRangeStats: true},
	})
	require.NoError(t, err)
	return builder
}

func buildInt64Column(t *testing.T, name string) *ColumnBuilder {
	t.Helper()

	builder, err := NewColumnBuilder(name, BuilderOptions{
		PageSizeHint: 16, // Small page size to force multiple pages
		Value:        datasetmd.VALUE_TYPE_INT64,
		Compression:  datasetmd.COMPRESSION_TYPE_SNAPPY,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,

		Statistics: StatisticsOptions{StoreRangeStats: true},
	})
	require.NoError(t, err)
	return builder
}

// readBasicReader reads all rows from a basicReader using the given batch size.
func readBasicReader(br *basicReader, batchSize int) ([]Row, error) {
	var (
		all []Row

		batch = make([]Row, batchSize)
	)

	for {
		// Clear the batch for each read; this is required to ensure that any
		// memory inside Row and Value doesn't get reused.
		//
		// This requires any Row/Value provided by br.Read is owned by the caller
		// and is not retained by the reader; if a test fails and appears to have
		// memory reuse, it's likely because code in basicReader changed and broke
		// ownership semantics.
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

// convertToTestPersons converts a slice of rows to test persons.
func convertToTestPersons(rows []Row) []testPerson {
	out := make([]testPerson, 0, len(rows))

	for _, row := range rows {
		var p testPerson

		p.firstName = row.Values[0].String()
		if !row.Values[1].IsNil() {
			p.middleName = row.Values[1].String()
		}
		p.lastName = row.Values[2].String()
		p.birthYear = row.Values[3].Int64()

		out = append(out, p)
	}

	return out
}
