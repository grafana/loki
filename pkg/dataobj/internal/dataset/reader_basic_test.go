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
	id         int64
	firstName  string
	middleName string // May be empty
	lastName   string
	birthYear  int64
}

var basicReaderTestData = []testPerson{
	{1, "John", "Robert", "Smith", 1980},
	{2, "Jane", "", "Doe", 1985},
	{3, "Alice", "Marie", "Johnson", 1990},
	{4, "Bob", "", "Williams", 1975},
	{5, "Carol", "Lynn", "Brown", 1982},
	{6, "David", "", "Miller", 1988},
	{7, "Eve", "Grace", "Davis", 1979},
	{8, "Frank", "", "Wilson", 1983},
	{9, "Grace", "Elizabeth", "Taylor", 1987},
	{10, "Henry", "", "Anderson", 1981},
}

func Test_basicReader_ReadAll(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 5)

	br := newBasicReader(columns)
	defer br.Close()

	actualRows, err := readBasicReader(br, 3, nil)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_basicReader_Bounds(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 5)

	br := newBasicReader(columns)
	defer br.Close()

	// Bounds hit after some rows
	actualRows, err := readBasicReader(br, 3, &ColumnBounds{column: columns[0], boundsChecker: BoundsForEqual(Int64Value(5), datasetmd.SORT_DIRECTION_ASCENDING)})
	require.ErrorIs(t, err, ErrBoundExceeded)
	require.Len(t, actualRows, 5)
	require.Equal(t, basicReaderTestData[:5], convertToTestPersons(actualRows))

	br.Reset(columns)

	// Empty result set - all rows exceed bounds
	actualRows, err = readBasicReader(br, 3, &ColumnBounds{column: columns[0], boundsChecker: BoundsForEqual(Int64Value(-1), datasetmd.SORT_DIRECTION_ASCENDING)})
	require.ErrorIs(t, err, ErrBoundExceeded)
	require.Empty(t, actualRows)
}

func Test_basicReader_ReadFromOffset(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 5)

	br := newBasicReader(columns)
	defer br.Close()

	// Seek to row 4
	_, err := br.Seek(4, io.SeekStart)
	require.NoError(t, err)

	actualRows, err := readBasicReader(br, 3, nil)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData[4:], convertToTestPersons(actualRows))
}

func Test_basicReader_SeekToStart(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 5)

	br := newBasicReader(columns)
	defer br.Close()

	// First read everything
	_, err := readBasicReader(br, 3, nil)
	require.NoError(t, err)

	// Seek back to start and read again
	_, err = br.Seek(0, io.SeekStart)
	require.NoError(t, err)

	actualRows, err := readBasicReader(br, 3, nil)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_basicReader_ReadColumns(t *testing.T) {
	columns := buildTestColumns(t)
	require.Len(t, columns, 5)

	br := newBasicReader(columns)
	defer br.Close()

	// Read only birth_year and middle_name columns (indices 4 and 2)
	subset := []Column{columns[4], columns[2]}

	var (
		batch = make([]Row, 3)
		all   []Row
	)

	for {
		// Clear the batch before reading the next set of values. See
		// implementation of readBasicReader for more details.
		clear(batch)

		n, err := br.ReadColumns(context.Background(), subset, batch, nil)
		for _, row := range batch[:n] {
			// Verify that the row has space for all columns
			require.Len(t, row.Values, 5)

			// Verify that unread columns (id, first_name and last_name) are nil
			require.True(t, row.Values[0].IsNil(), "id should be nil")
			require.True(t, row.Values[1].IsNil(), "first_name should be nil")
			require.True(t, row.Values[3].IsNil(), "last_name should be nil")

			// Verify that read columns match the test data
			testPerson := basicReaderTestData[row.Index]
			if testPerson.middleName != "" {
				require.Equal(t, testPerson.middleName, string(row.Values[2].ByteArray()), "middle_name mismatch")
			} else {
				require.True(t, row.Values[2].IsNil(), "middle_name should be nil")
			}
			require.Equal(t, testPerson.birthYear, row.Values[4].Int64(), "birth_year mismatch")

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
	require.Len(t, columns, 5)

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
	n, err := br.Fill(context.Background(), []Column{columns[1]}, buf, nil)
	require.NoError(t, err)
	require.Equal(t, len(buf), n)

	// Verify the filled values
	for _, row := range buf {
		// Check that only firstName is filled
		require.True(t, row.Values[0].IsNil(), "id should be nil")
		require.False(t, row.Values[1].IsNil(), "firstName should not be nil")
		require.True(t, row.Values[2].IsNil(), "middleName should be nil")
		require.True(t, row.Values[3].IsNil(), "lastName should be nil")
		require.True(t, row.Values[4].IsNil(), "birthYear should be nil")

		// Verify the firstName value
		expectedPerson := basicReaderTestData[row.Index]
		require.Equal(t, expectedPerson.firstName, string(row.Values[1].ByteArray()),
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
	require.Len(t, columns, 5)

	br := newBasicReader(columns)
	defer br.Close()

	// First read everything
	_, err := readBasicReader(br, 3, nil)
	require.NoError(t, err)

	// Reset and read again
	br.Reset(columns)

	actualRows, err := readBasicReader(br, 3, nil)
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
	idBuilder := buildInt64Column(t, "id")
	firstNameBuilder := buildStringColumn(t, "first_name")
	middleNameBuilder := buildStringColumn(t, "middle_name")
	lastNameBuilder := buildStringColumn(t, "last_name")
	birthYearBuilder := buildInt64Column(t, "birth_year")

	// Add data to each column
	for i, p := range basicReaderTestData {
		require.NoError(t, idBuilder.Append(i, Int64Value(p.id)))
		require.NoError(t, firstNameBuilder.Append(i, ByteArrayValue([]byte(p.firstName))))
		require.NoError(t, middleNameBuilder.Append(i, ByteArrayValue([]byte(p.middleName))))
		require.NoError(t, lastNameBuilder.Append(i, ByteArrayValue([]byte(p.lastName))))
		require.NoError(t, birthYearBuilder.Append(i, Int64Value(p.birthYear)))
	}

	// Flush all columns
	id, err := idBuilder.Flush()
	require.NoError(t, err)
	firstName, err := firstNameBuilder.Flush()
	require.NoError(t, err)
	middleName, err := middleNameBuilder.Flush()
	require.NoError(t, err)
	lastName, err := lastNameBuilder.Flush()
	require.NoError(t, err)
	birthYear, err := birthYearBuilder.Flush()
	require.NoError(t, err)

	dset := FromMemory([]*MemColumn{id, firstName, middleName, lastName, birthYear})

	cols, err := result.Collect(dset.ListColumns(context.Background()))
	require.NoError(t, err)

	return dset, cols
}

func buildStringColumn(t *testing.T, name string) *ColumnBuilder {
	t.Helper()

	builder, err := NewColumnBuilder(name, BuilderOptions{
		PageSizeHint: 16, // Small page size to force multiple pages
		Value:        datasetmd.VALUE_TYPE_BYTE_ARRAY,
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
func readBasicReader(br *basicReader, batchSize int, bounds *ColumnBounds) ([]Row, error) {
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

		n, err := br.Read(context.Background(), batch, bounds)
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

		p.id = row.Values[0].Int64()
		p.firstName = string(row.Values[1].ByteArray())
		if !row.Values[2].IsNil() {
			p.middleName = string(row.Values[2].ByteArray())
		}
		p.lastName = string(row.Values[3].ByteArray())
		p.birthYear = row.Values[4].Int64()

		out = append(out, p)
	}

	return out
}
