package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"math/rand"
	"slices"
	"strconv"
	"testing"

	"github.com/dustin/go-humanize"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func Test_RowReader_Open_Prefetch(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewRowReader(RowReaderOptions{Dataset: dset, Columns: columns, Prefetch: true})
	defer r.Close()

	require.NoError(t, r.Open(t.Context()))

	require.NotEqual(t, 0, len(r.dl.allColumns), "expected at least one column")
	for i, col := range r.dl.allColumns {
		rc := col.(*readerColumn)

		assert.NotEqual(t, 0, len(rc.pages), "expected at least one page in column %d", i)
		for j, page := range rc.pages {
			assert.NotNil(t, page.data, "Expected column %d page %d to be prefetched", i, j)
		}
	}
}

func Test_Reader_ReadAll(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewRowReader(RowReaderOptions{Dataset: dset, Columns: columns})
	defer r.Close()

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_Reader_ReadBeforeOpen(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewRowReader(RowReaderOptions{Dataset: dset, Columns: columns})
	defer r.Close()

	_, err := r.Read(context.Background(), make([]Row, 1))
	require.ErrorIs(t, err, errRowReaderNotOpen)
}

func Test_Reader_ReadWithPredicate(t *testing.T) {
	dset, columns := buildTestDataset(t)

	// Create a predicate that only returns people born after 1985
	r := NewRowReader(RowReaderOptions{
		Dataset: dset,
		Columns: columns,
		Predicates: []Predicate{
			GreaterThanPredicate{
				Column: columns[3], // birth_year column
				Value:  Int64Value(1985),
			},
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

// TestRowReader_ReadWithPageFiltering tests that a RowReader can filter rows based
// on a predicate that has filtered pages out.
func TestRowReader_ReadWithPageFiltering(t *testing.T) {
	dset, columns := buildTestDataset(t)

	r := NewRowReader(RowReaderOptions{
		Dataset: dset,
		Columns: columns,

		// Henry is out of range of most pages except for the first and last, so
		// other pages would be filtered out of testing.
		//
		// TODO(rfratto): make it easier to prove that a predicate includes a value
		// which is out of range of at least one page.
		Predicates: []Predicate{
			EqualPredicate{
				Column: columns[0], // first_name column
				Value:  BinaryValue([]byte("Henry")),
			},
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

// TestRowReader_ReadWithPageFilteringOnEmptyPredicate tests that a RowReader filters rows with empty predicate values.
// Filtering for an explicitly empty value also includes Null values and these rows should not be excluded by page skipping.
func TestRowReader_ReadWithPageFilteringOnEmptyPredicate(t *testing.T) {
	// Create builders for each column
	firstNameBuilder := buildStringColumn(t, "first_name")
	lastNameBuilder := buildStringColumn(t, "middle_name")

	// Row with both values.
	firstNameBuilder.append(0, BinaryValue([]byte("John")))
	lastNameBuilder.append(0, BinaryValue([]byte("Doe")))

	// Row with null value.
	firstNameBuilder.append(2, BinaryValue([]byte("Jim")))
	lastNameBuilder.append(2, Value{})

	firstName, err := firstNameBuilder.Flush()
	require.NoError(t, err)
	lastName, err := lastNameBuilder.Flush()
	require.NoError(t, err)

	dset := FromMemory([]*MemColumn{firstName, lastName})
	cols, err := result.Collect(dset.ListColumns(context.Background()))
	require.NoError(t, err)

	r := NewRowReader(RowReaderOptions{
		Dataset: dset,
		Columns: cols,

		Predicates: []Predicate{
			// Imitate predicate from logql: {last_name=""}
			EqualPredicate{
				Column: cols[1], // last_name column
				Value:  BinaryValue([]byte("")),
			},
		},
	})
	defer r.Close()

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)

	actualFirstNames := make([]string, 0, len(actualRows))
	actualLastNames := make([]string, 0, len(actualRows))
	for _, row := range actualRows {
		if row.Values[0].IsNil() && row.Values[1].IsNil() {
			continue
		}
		actualFirstNames = append(actualFirstNames, string(row.Values[0].Binary()))
		if !row.Values[1].IsNil() {
			actualLastNames = append(actualLastNames, string(row.Values[1].Binary()))
		} else {
			actualLastNames = append(actualLastNames, "[nil]")
		}
	}

	// Filter expected data manually to verify
	expectedFirstNames := []string{"Jim"}
	expectedLastNames := []string{"[nil]"}
	require.Equal(t, expectedFirstNames, actualFirstNames)
	require.Equal(t, expectedLastNames, actualLastNames)
}

func Test_Reader_ReadWithPredicate_NoSecondary(t *testing.T) {
	dset, columns := buildTestDataset(t)

	// Create a predicate that only returns people born after 1985
	r := NewRowReader(RowReaderOptions{
		Dataset: dset,
		Columns: []Column{columns[3]},
		Predicates: []Predicate{
			GreaterThanPredicate{
				Column: columns[3], // birth_year column
				Value:  Int64Value(1985),
			},
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
	r := NewRowReader(RowReaderOptions{Dataset: dset, Columns: columns})
	defer r.Close()

	// First read everything
	_, err := readDataset(r, 3)
	require.NoError(t, err)

	// Reset and read again
	r.Reset(RowReaderOptions{Dataset: dset, Columns: columns})

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_buildMask(t *testing.T) {
	tt := []struct {
		name      string
		fullRange rangeset.Range
		rows      []Row
		expect    []rangeset.Range
	}{
		{
			name:      "no rows",
			fullRange: rangeset.Range{Start: 1, End: 10},
			rows:      nil,
			expect:    []rangeset.Range{{Start: 1, End: 10}},
		},
		{
			name:      "full coverage",
			fullRange: rangeset.Range{Start: 1, End: 10},
			rows:      makeRows(1, 10, 1),
			expect:    nil,
		},
		{
			name:      "full coverage - split",
			fullRange: rangeset.Range{Start: 1, End: 10},
			rows:      mergeRows(makeRows(1, 5, 1), makeRows(5, 10, 1)),
			expect:    nil,
		},
		{
			name:      "partial coverage - front",
			fullRange: rangeset.Range{Start: 1, End: 10},
			rows:      makeRows(1, 5, 1),
			expect:    []rangeset.Range{{Start: 5, End: 10}},
		},
		{
			name:      "partial coverage - middle",
			fullRange: rangeset.Range{Start: 1, End: 10},
			rows:      makeRows(5, 8, 1),
			expect:    []rangeset.Range{{Start: 1, End: 5}, {Start: 8, End: 10}},
		},
		{
			name:      "partial coverage - end",
			fullRange: rangeset.Range{Start: 1, End: 10},
			rows:      makeRows(6, 10, 1),
			expect:    []rangeset.Range{{Start: 1, End: 6}},
		},
		{
			name:      "partial coverage - gaps",
			fullRange: rangeset.Range{Start: 1, End: 10},
			rows:      []Row{{Index: 3}, {Index: 5}, {Index: 7}, {Index: 9}},
			expect: []rangeset.Range{
				{Start: 1, End: 3},
				{Start: 4, End: 5},
				{Start: 6, End: 7},
				{Start: 8, End: 9},
			},
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
	for i := from; i < to; i += inc {
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

// readDataset reads all rows from a RowReader using the given batch size.
func readDataset(br *RowReader, batchSize int) ([]Row, error) {
	return readDatasetWithContext(context.Background(), br, batchSize)
}

// readDatasetWithContext reads all rows from a RowReader using the given batch size and context.
func readDatasetWithContext(ctx context.Context, br *RowReader, batchSize int) ([]Row, error) {
	var (
		all []Row

		batch = make([]Row, batchSize)
	)
	if err := br.Open(ctx); err != nil {
		return nil, err
	}

	for {
		// Clear the batch for each read, to ensure that any memory in Row and
		// Value doesn't get reused. See comment in implmentation of
		// [readBasicReader] for more information.
		clear(batch)

		n, err := br.Read(ctx, batch)
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
		want      rangeset.Set
	}{
		{
			name:      "nil predicate returns full range",
			predicate: nil,
			want:      rangeset.From(rangeset.Range{Start: 0, End: 1000}), // Full dataset range
		},
		{
			name:      "equal predicate in range",
			predicate: EqualPredicate{Column: cols[1], Value: Int64Value(50)},
			want:      rangeset.From(rangeset.Range{Start: 0, End: 250}), // Page 1 of Timestamp column
		},
		{
			name:      "equal predicate not in any range",
			predicate: EqualPredicate{Column: cols[1], Value: Int64Value(1500)},
			want:      rangeset.Set{}, // No ranges should match
		},
		{
			name:      "greater than predicate",
			predicate: GreaterThanPredicate{Column: cols[1], Value: Int64Value(400)},
			want:      rangeset.From(rangeset.Range{Start: 250, End: 1000}), // Pages 2 and 3 of Timestamp column
		},
		{
			name:      "less than predicate",
			predicate: LessThanPredicate{Column: cols[1], Value: Int64Value(300)},
			want:      rangeset.From(rangeset.Range{Start: 0, End: 750}), // Pages 1 and 2 of Timestamp column
		},
		{
			name: "and predicate",
			predicate: AndPredicate{
				Left:  EqualPredicate{Column: cols[0], Value: Int64Value(1)},      // Rows 0 - 299 of stream column
				Right: LessThanPredicate{Column: cols[1], Value: Int64Value(600)}, // Rows 0 - 249, 250 - 749 of timestamp column
			},
			want: rangeset.From(rangeset.Range{Start: 0, End: 300}),
		},
		{
			name: "or predicate",
			predicate: OrPredicate{
				Left:  EqualPredicate{Column: cols[0], Value: Int64Value(1)},         // Rows 0 - 299 of stream column
				Right: GreaterThanPredicate{Column: cols[1], Value: Int64Value(800)}, // Rows 750 - 999 of timestamp column
			},
			want: rangeset.From(rangeset.Range{Start: 0, End: 300}, rangeset.Range{Start: 750, End: 1000}), // [0, 300), [750, 1000)
		},
		{
			name: "InPredicate with values inside and outside page ranges",
			predicate: InPredicate{
				Column: cols[1], // timestamp column
				Values: NewInt64ValueSet([]Value{
					Int64Value(50),
					Int64Value(300),
					Int64Value(150),
					Int64Value(600),
				}), // 2 values in range. ~200 matching rows
			},
			want: rangeset.From(rangeset.Range{Start: 0, End: 750}), // Page 1 + 2
		},
		{
			name: "InPredicate with values all outside page ranges",
			predicate: InPredicate{
				Column: cols[1], // timestamp column
				Values: NewInt64ValueSet([]Value{
					Int64Value(150), // Outside all pages
					Int64Value(600), // Outside all pages
				}),
			},
			want: rangeset.Set{}, // No pages should be included
		},
	}

	ctx := context.Background()
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRowReader(RowReaderOptions{
				Dataset:    ds,
				Columns:    cols,
				Predicates: []Predicate{tc.predicate},
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
			Desc: ColumnDesc{
				Tag:       "stream",
				Type:      ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
				RowsCount: 1000, // 0 - 999
			},
			Pages: []*MemPage{
				{
					Desc: PageDesc{
						RowCount: 300, // 0 - 299
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 1),
							MaxValue: encodeInt64Value(t, 2),
						},
					},
				},
				{
					Desc: PageDesc{
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
			Desc: ColumnDesc{
				Tag:       "timestamp",
				Type:      ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
				RowsCount: 1000, // 0 - 999
			},
			Pages: []*MemPage{
				{
					Desc: PageDesc{
						RowCount: 250, // 0 - 249
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 0),
							MaxValue: encodeInt64Value(t, 100),
						},
					},
				},
				{
					Desc: PageDesc{
						RowCount: 500, // 249 - 749
						Stats: &datasetmd.Statistics{
							MinValue: encodeInt64Value(t, 200),
							MaxValue: encodeInt64Value(t, 500),
						},
					},
				},
				{
					Desc: PageDesc{
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

func BenchmarkReader(b *testing.B) {
	generator := DatasetGenerator{
		RowCount:     1_000_000,
		PageSizeHint: 2 * 1024 * 1024, // 2MB
		Columns: []generatorColumnConfig{
			{
				Tag:               "stream",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 1000,
			},
			{
				Tag:               "timestamp",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "number"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 100_000,
			},
			{
				Tag:               "log",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "string"},
				Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				AvgSize:           1024,
				CardinalityTarget: 100_000,
			},
		},
	}

	readPatterns := []struct {
		name      string
		batchSize int
	}{
		{
			name:      "batch=100",
			batchSize: 100,
		},
		{
			name:      "batch=10k",
			batchSize: 10_000,
		},
	}

	// Generate dataset once per case
	ds, cols := generator.Build(b, rand.Int63())
	opts := RowReaderOptions{
		Dataset: ds,
		Columns: cols,
	}

	for _, rp := range readPatterns {
		b.Run(rp.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			batch := make([]Row, rp.batchSize)
			for b.Loop() {
				reader := NewRowReader(opts)
				require.NoError(b, reader.Open(context.Background()))

				var rowsRead int
				for {
					n, err := reader.Read(context.Background(), batch)
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
					rowsRead += n
				}
				reader.Close()

				b.ReportMetric(float64(rowsRead)/float64(b.N), "rows/op")
			}
		})
	}
}

func BenchmarkPredicateExecution(b *testing.B) {
	// Generate dataset with two columns, one with high cardinality and one with low cardinality
	// higher the cardinality, more selective the predicate
	generator := DatasetGenerator{
		RowCount: 1_000_000,
		// set large page size to not realise benefits from page pruning since the goal
		// of this benchmark is to measure the gains from sequential predicate evaluation alone.
		PageSizeHint: 100 * 1024 * 1024,
		Columns: []generatorColumnConfig{
			{
				Tag:               "more_selective",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "int64"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 500_000,
			},
			{
				Tag:               "less_selective",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "int64"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 100,
			},
		},
	}

	ds, cols := generator.Build(b, rand.Int63())

	var col1Value, col2Value int64
	idx := rand.Intn(generator.RowCount) // Randomly select a row index to use for the predicate values

	currentPos := 0
	batch := make([]Row, 1000)
	// read the dataset once to pick a random row for predicate generation
	reader := NewRowReader(RowReaderOptions{
		Dataset: ds,
		Columns: cols,
	})
	require.NoError(b, reader.Open(context.Background()))

	for {
		n, err := reader.Read(context.Background(), batch)
		if err == io.EOF {
			break
		}
		if err != nil {
			b.Fatal(err)
		}

		// Check if our target index is in this batch
		if idx >= currentPos && idx < currentPos+n {
			selectedRow := batch[idx-currentPos]
			col1Value = selectedRow.Values[0].Int64()
			col2Value = selectedRow.Values[1].Int64()
			break
		}

		currentPos += n
	}
	reader.Close()

	predicatePatterns := []struct {
		name       string
		predicates []Predicate
	}{
		{
			name: "combined",
			predicates: []Predicate{
				AndPredicate{
					Left: EqualPredicate{
						Column: cols[0],
						Value:  Int64Value(col1Value),
					},
					Right: EqualPredicate{
						Column: cols[1],
						Value:  Int64Value(col2Value),
					},
				},
			},
		},
		{
			name: "high",
			predicates: []Predicate{
				EqualPredicate{
					Column: cols[0],
					Value:  Int64Value(col1Value),
				},
				EqualPredicate{
					Column: cols[1],
					Value:  Int64Value(col2Value),
				},
			},
		},
		{
			name: "low",
			predicates: []Predicate{
				EqualPredicate{
					Column: cols[1],
					Value:  Int64Value(col2Value),
				},
				EqualPredicate{
					Column: cols[0],
					Value:  Int64Value(col1Value),
				},
			},
		},
	}

	for _, pp := range predicatePatterns {
		b.Run("selectivity="+pp.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			for b.Loop() {
				reader := NewRowReader(RowReaderOptions{
					Dataset:    ds,
					Columns:    cols,
					Predicates: pp.predicates,
				})
				require.NoError(b, reader.Open(context.Background()))

				batch := make([]Row, 10000)

				for {
					_, err := reader.Read(context.Background(), batch)
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				reader.Close()
			}
		})
	}
}

type generatorColumnConfig struct {
	Tag         string
	Type        ColumnType
	Encoding    datasetmd.EncodingType
	Compression datasetmd.CompressionType

	AvgSize           int64   // Average size in bytes for variable-length types
	CardinalityTarget int64   // Target number of unique values
	SparsityRate      float64 // 0.0-1.0, where 1.0 means all values are null
}

func columnValues(rng *rand.Rand, cfg generatorColumnConfig) iter.Seq[Value] {
	switch cfg.Type.Physical {
	case datasetmd.PHYSICAL_TYPE_INT64, datasetmd.PHYSICAL_TYPE_UINT64:
		return numberValues(rng, cfg)
	case datasetmd.PHYSICAL_TYPE_BINARY:
		return stringValues(rng, cfg)
	default:
		panic(fmt.Sprintf("unsupported type for generation: %v", cfg.Type.Physical))
	}
}

func stringValues(rng *rand.Rand, cfg generatorColumnConfig) iter.Seq[Value] {
	// Pre-generate the set of unique values we'll cycle through
	uniqueValues := make([]Value, cfg.CardinalityTarget)
	for i := range int(cfg.CardinalityTarget) {
		// Generate size between 0.5x and 1.5x of average size
		size := int(float64(cfg.AvgSize) * (0.5 + rng.Float64()))

		// Convert number to string and create padded result
		str := make([]byte, size)
		num := []byte(strconv.Itoa(i))
		copy(str, num)
		for j := len(num); j < size; j++ {
			str[j] = 'x'
		}
		uniqueValues[i] = BinaryValue(str)
	}

	return func(yield func(Value) bool) {
		for {
			if !yield(uniqueValues[rng.Intn(len(uniqueValues))]) {
				return
			}
		}
	}
}

func numberValues(rng *rand.Rand, cfg generatorColumnConfig) iter.Seq[Value] {
	return func(yield func(Value) bool) {
		for {
			v := rng.Int63n(cfg.CardinalityTarget)
			switch cfg.Type.Physical {
			case datasetmd.PHYSICAL_TYPE_INT64:
				if !yield(Int64Value(v)) {
					return
				}
			case datasetmd.PHYSICAL_TYPE_UINT64:
				if !yield(Uint64Value(uint64(v))) {
					return
				}
			}
		}
	}
}

type DatasetGenerator struct {
	RowCount     int
	PageSizeHint int
	Columns      []generatorColumnConfig
}

func (g *DatasetGenerator) Build(t testing.TB, seed int64) (Dataset, []Column) {
	t.Helper()

	memColumns := make([]*MemColumn, 0, len(g.Columns))
	rng := rand.New(rand.NewSource(seed))

	for _, colCfg := range g.Columns {
		next, stop := iter.Pull(columnValues(rng, colCfg))
		defer stop()

		opts := BuilderOptions{
			PageSizeHint: g.PageSizeHint,
			Type:         colCfg.Type,
			Encoding:     colCfg.Encoding,
			Compression:  colCfg.Compression,
			Statistics: StatisticsOptions{
				StoreCardinalityStats: true,
			},
		}

		if colCfg.Type.Physical == datasetmd.PHYSICAL_TYPE_INT64 || colCfg.Type.Physical == datasetmd.PHYSICAL_TYPE_UINT64 {
			opts.Statistics.StoreRangeStats = true
		}

		// Create a builder for this column
		builder, err := NewColumnBuilder(colCfg.Tag, opts)
		require.NoError(t, err)

		// Add values to the builder
		for i := range g.RowCount {
			if rng.Float64() < colCfg.SparsityRate {
				continue
			}

			val, ok := next()
			require.True(t, ok, "generator should yield values")
			require.NoError(t, builder.Append(i, val))
		}

		col, err := builder.Flush()
		require.NoError(t, err)

		memColumns = append(memColumns, col)
	}

	ds := FromMemory(memColumns)
	cols, err := result.Collect(ds.ListColumns(context.Background()))
	require.NoError(t, err)

	return ds, cols
}

// Test_DatasetGenerator is a helper to debug the dataset generation
func Test_DatasetGenerator(t *testing.T) {
	g := DatasetGenerator{
		RowCount:     1_000_000,
		PageSizeHint: 2 * 1024 * 1024, // 2MB
		Columns: []generatorColumnConfig{
			{
				Tag:               "timestamp",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "timestamp"},
				Encoding:          datasetmd.ENCODING_TYPE_DELTA,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				CardinalityTarget: 100_000,
				SparsityRate:      0.0,
			},
			{
				Tag:               "label",
				Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "label"},
				Encoding:          datasetmd.ENCODING_TYPE_PLAIN,
				Compression:       datasetmd.COMPRESSION_TYPE_NONE,
				AvgSize:           32,
				CardinalityTarget: 100,
				SparsityRate:      0.3,
			},
		},
	}

	_, cols := g.Build(t, rand.Int63())
	require.Equal(t, 2, len(cols))
	require.Equal(t, g.RowCount, cols[0].ColumnDesc().RowsCount)
	// TODO: Row count is < expected. Must be a result of null values at the end.
	// Remove this comment once the issue is fixed.
	// require.Equal(t, g.RowCount, cols[1].ColumnInfo().RowsCount)

	require.NotNil(t, cols[0].ColumnDesc().Statistics.CardinalityCount)
	require.NotNil(t, cols[1].ColumnDesc().Statistics.CardinalityCount)

	t.Logf("timestamp column cardinality: %d", cols[0].ColumnDesc().Statistics.CardinalityCount)
	t.Logf("label column cardinality: %d", cols[1].ColumnDesc().Statistics.CardinalityCount)

	require.NotNil(t, cols[0].ColumnDesc().Statistics.MinValue)
	require.NotNil(t, cols[0].ColumnDesc().Statistics.MaxValue)

	var minValue, maxValue Value
	require.NoError(t, minValue.UnmarshalBinary(cols[0].ColumnDesc().Statistics.MinValue))
	require.NoError(t, maxValue.UnmarshalBinary(cols[0].ColumnDesc().Statistics.MaxValue))

	t.Logf("timestamp column min: %d", minValue.Int64())
	t.Logf("timestamp column max: %d", maxValue.Int64())

	t.Logf("timestamp column size: %s", humanize.Bytes(uint64(cols[0].ColumnDesc().UncompressedSize)))
	t.Logf("label column size: %s", humanize.Bytes(uint64(cols[1].ColumnDesc().UncompressedSize)))
}

// Test_Reader_Stats tests that the reader properly tracks statistics via xcap regions.
func Test_Reader_Stats(t *testing.T) {
	dset, columns := buildTestDataset(t)

	r := NewRowReader(RowReaderOptions{
		Dataset: dset,
		Columns: columns,
		Predicates: []Predicate{
			GreaterThanPredicate{
				Column: columns[3], // birth_year
				Value:  Int64Value(1985),
			},
			EqualPredicate{
				Column: columns[0], // first_name
				Value:  BinaryValue([]byte("Alice")),
			},
		},
	})
	defer r.Close()

	ctx, _ := xcap.NewCapture(context.Background(), nil)
	ctx, region := xcap.StartRegion(ctx, "logs.Reader")
	defer region.End()

	_, err := readDatasetWithContext(ctx, r, 3)
	require.NoError(t, err)

	require.NotNil(t, region, "region should be available after reading")

	observations := region.Observations()
	obsMap := make(map[string]int64)
	for _, obs := range observations {
		obsMap[obs.Statistic.Name()] = obs.Value.(int64)
	}

	require.Equal(t, int64(2), obsMap[xcap.StatDatasetReadCalls.Name()])
	require.Equal(t, int64(2), obsMap[xcap.StatDatasetPrimaryColumns.Name()])
	require.Equal(t, int64(2), obsMap[xcap.StatDatasetSecondaryColumns.Name()])
	require.Equal(t, int64(5), obsMap[xcap.StatDatasetPrimaryColumnPages.Name()])
	require.Equal(t, int64(8), obsMap[xcap.StatDatasetSecondaryColumnPages.Name()])

	require.Equal(t, int64(len(basicReaderTestData)), obsMap[xcap.StatDatasetMaxRows.Name()])
	require.Equal(t, int64(3), obsMap[xcap.StatDatasetRowsAfterPruning.Name()])
	require.Equal(t, int64(3), obsMap[xcap.StatDatasetPrimaryRowsRead.Name()])
	require.Equal(t, int64(1), obsMap[xcap.StatDatasetSecondaryRowsRead.Name()])
}
