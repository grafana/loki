package dataset

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
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

func Test_BuildPredicateRanges(t *testing.T) {
	ds, cols := buildMemDatasetWithStats(t)
	tt := []struct {
		name      string
		predicate Predicate
		want      rowRanges
	}{
		{
			name:      "nil predicate returns full range",
			predicate: nil,
			want:      rowRanges{{Start: 0, End: 999}}, // Full dataset range
		},
		{
			name:      "equal predicate in range",
			predicate: EqualPredicate{Column: cols[1], Value: Int64Value(50)},
			want:      rowRanges{{Start: 0, End: 249}}, // Page 1 of Timestamp column
		},
		{
			name:      "equal predicate not in any range",
			predicate: EqualPredicate{Column: cols[1], Value: Int64Value(1500)},
			want:      nil, // No ranges should match
		},
		{
			name:      "greater than predicate",
			predicate: GreaterThanPredicate{Column: cols[1], Value: Int64Value(400)},
			want:      rowRanges{{Start: 250, End: 749}, {Start: 750, End: 999}}, // Pages 2 and 3 of Timestamp column
		},
		{
			name:      "less than predicate",
			predicate: LessThanPredicate{Column: cols[1], Value: Int64Value(300)},
			want:      rowRanges{{Start: 0, End: 249}, {Start: 250, End: 749}}, // Pages 1 and 2 of Timestamp column
		},
		{
			name: "and predicate",
			predicate: AndPredicate{
				Left:  EqualPredicate{Column: cols[0], Value: Int64Value(1)},      // Rows 0 - 299 of stream column
				Right: LessThanPredicate{Column: cols[1], Value: Int64Value(600)}, // Rows 0 - 249, 250 - 749 of timestamp column
			},
			want: rowRanges{{Start: 0, End: 249}, {Start: 250, End: 299}},
		},
		{
			name: "or predicate",
			predicate: OrPredicate{
				Left:  EqualPredicate{Column: cols[0], Value: Int64Value(1)},         // Rows 0 - 299 of stream column
				Right: GreaterThanPredicate{Column: cols[1], Value: Int64Value(800)}, // Rows 750 - 999 of timestamp column
			},
			want: rowRanges{{Start: 0, End: 299}, {Start: 750, End: 999}}, // Rows 0 - 299, 750 - 999
		},
	}

	ctx := context.Background()
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			r := NewReader(ReaderOptions{
				Dataset:   ds,
				Columns:   cols,
				Predicate: tc.predicate,
			})
			defer r.Close()

			// Initialize downloader
			require.NoError(t, r.initDownloader(ctx))

			got, err := r.buildPredicateRanges(ctx, tc.predicate)
			require.NoError(t, err)

			require.Equal(t, tc.want, got, "row ranges should match expected ranges")
		})
	}
}

// buildMemDatasetWithStats creates a test dataset with only column and page stats.
func buildMemDatasetWithStats(t *testing.T) (Dataset, []Column) {
	t.Helper()

	dset := FromMemory([]*MemColumn{
		{
			Info: ColumnInfo{
				Name:      "stream",
				Type:      datasetmd.VALUE_TYPE_INT64,
				RowsCount: 1000, // 0 - 999
			},
			Pages: []*MemPage{
				{
					Info: PageInfo{
						RowCount: 300, // 0 - 299
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 1),
							MaxValue: encodeInt64Value(t, 2),
						},
					},
				},
				{
					Info: PageInfo{
						RowCount: 700, // 300 - 999
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 2),
							MaxValue: encodeInt64Value(t, 2),
						},
					},
				},
			},
		},
		{
			Info: ColumnInfo{
				Name:      "timestamp",
				Type:      datasetmd.VALUE_TYPE_INT64,
				RowsCount: 1000, // 0 - 999
			},
			Pages: []*MemPage{
				{
					Info: PageInfo{
						RowCount: 250, // 0 - 249
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 0),
							MaxValue: encodeInt64Value(t, 100),
						},
					},
				},
				{
					Info: PageInfo{
						RowCount: 500, // 249 - 749
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 200),
							MaxValue: encodeInt64Value(t, 500),
						},
					},
				},
				{
					Info: PageInfo{
						RowCount: 250, // 750 - 999
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 800),
							MaxValue: encodeInt64Value(t, 1000),
						},
					},
				},
			},
		},
	})

	cols, err := result.Collect(dset.ListColumns(context.Background()))
	require.NoError(t, err)

	return dset, cols
}

// Helper function to encode an integer value for statistics
func encodeInt64Value(t *testing.T, v int64) []byte {
	t.Helper()

	data, err := Int64Value(v).MarshalBinary()
	require.NoError(t, err)
	return data
}
