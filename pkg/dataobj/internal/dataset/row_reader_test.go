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

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
	"github.com/grafana/loki/v3/pkg/xcap"
)

func Test_RowReader_Open_Prefetch(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, PrefetchAllOnOpen: true})
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

func Test_RowReader_LazyDownloadTarget(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, PrefetchAllOnOpen: false})
	defer r.Close()

	require.NoError(t, r.Open(t.Context()))
	require.Equal(t, defaultTargetDownloadedBytes, r.dl.targetCompressedBytes)

	// Force the target below a single page so only P1 pages for the current
	// read range are downloaded.
	r.dl.targetCompressedBytes = 1

	n, err := r.Read(t.Context(), make([]Row, 1))
	require.NoError(t, err)
	require.Equal(t, 1, n)

	var pagesCached, totalPages int
	for _, col := range r.dl.allColumns {
		rc := col.(*readerColumn)
		require.NotEmpty(t, rc.pages)
		for _, page := range rc.pages {
			totalPages++
			if page.data != nil {
				pagesCached++
			}
		}
	}

	require.Greater(t, totalPages, len(columns), "expected more than one page per column")
	require.Greater(t, pagesCached, 0, "expected the current read range to download pages")
	require.Less(t, pagesCached, totalPages, "expected the download target to leave later pages uncached")
}

func Test_RowReader_ReadAll(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
	defer r.Close()

	actualRows, err := readDataset(r, 3)
	require.NoError(t, err)
	require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
}

func Test_RowReader_ReadBeforeOpen(t *testing.T) {
	dset, columns := buildTestDataset(t)
	r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
	defer r.Close()

	_, err := r.Read(context.Background(), make([]Row, 1))
	require.ErrorIs(t, err, errRowReaderNotOpen)
}

// Test_RowReader_ReadWithNegatedConstantPredicate covers a read whose only predicate negates a
// constant. A translation layer produces one whenever it reduces a negated leaf whose column
// the data does not hold, and the whole predicate then names no column, which the reader
// handles differently from a predicate that names one.
func Test_RowReader_ReadWithNegatedConstantPredicate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		inner Predicate
	}{
		{"not true", TruePredicate{}},
		{"not nil", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dset, columns := buildTestDataset(t)

			r := NewRowReader(RowReaderOptions{
				Dataset:          dset,
				ProjectedColumns: columns,
				Predicates:       []Predicate{NotPredicate{Inner: tc.inner}},
			})
			defer r.Close()

			rows, err := readDataset(r, 3)
			require.NoError(t, err)
			require.Empty(t, rows)
		})
	}
}

// TestRowReader_ReadWithPageFiltering tests that a RowReader can filter rows based
// on a predicate that has filtered pages out.
func TestRowReader_ReadWithPageFiltering(t *testing.T) {
	dset, columns := buildTestDataset(t)

	r := NewRowReader(RowReaderOptions{
		Dataset:          dset,
		ProjectedColumns: columns,

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
		Dataset:          dset,
		ProjectedColumns: cols,

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

func Test_RowReader_ReadWithPredicate_NoSecondary(t *testing.T) {
	dset, columns := buildTestDataset(t)

	// Create a predicate that only returns people born after 1985
	r := NewRowReader(RowReaderOptions{
		Dataset:          dset,
		ProjectedColumns: []Column{columns[3]},
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

func Test_RowReader_ReadWithPredicate(t *testing.T) {
	t.Run("greater than keeps a row larger than the value", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				GreaterThanPredicate{Column: columns[3], Value: Int64Value(1985)},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var expected []testPerson
		for _, p := range basicReaderTestData {
			if p.birthYear > 1985 {
				expected = append(expected, p)
			}
		}
		require.NotEmpty(t, expected)
		require.Equal(t, expected, convertToTestPersons(actualRows))
	})

	t.Run("or keeps a row that matches either side", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				OrPredicate{
					Left:  LessThanPredicate{Column: columns[3], Value: Int64Value(1980)},
					Right: GreaterThanPredicate{Column: columns[3], Value: Int64Value(1988)},
				},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var expected []testPerson
		for _, p := range basicReaderTestData {
			if p.birthYear < 1980 || p.birthYear > 1988 {
				expected = append(expected, p)
			}
		}
		require.NotEmpty(t, expected)
		require.Equal(t, expected, convertToTestPersons(actualRows))
	})

	t.Run("in keeps a row whose value is one of the set", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		wanted := []int64{1980, 1990, 1975}

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				InPredicate{Column: columns[3], Values: NewInt64ValueSetOf(wanted...)},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var expected []testPerson
		for _, p := range basicReaderTestData {
			if slices.Contains(wanted, p.birthYear) {
				expected = append(expected, p)
			}
		}
		require.NotEmpty(t, expected)
		require.Equal(t, expected, convertToTestPersons(actualRows))
	})

	t.Run("less than keeps a row smaller than the value", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				LessThanPredicate{Column: columns[3], Value: Int64Value(1980)},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var expected []testPerson
		for _, p := range basicReaderTestData {
			if p.birthYear < 1980 {
				expected = append(expected, p)
			}
		}
		require.NotEmpty(t, expected)
		require.Equal(t, expected, convertToTestPersons(actualRows))
	})

	t.Run("func keeps a row its Keep function accepts", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				FuncPredicate{
					Column: columns[0], // first_name column
					Keep: func(_ Column, value Value) bool {
						return len(value.Binary()) > 4
					},
				},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var expected []testPerson
		for _, p := range basicReaderTestData {
			if len(p.firstName) > 4 {
				expected = append(expected, p)
			}
		}
		require.NotEmpty(t, expected)
		require.Equal(t, expected, convertToTestPersons(actualRows))
	})

	t.Run("keeps a row that satisfies two sequential predicates on different columns", func(t *testing.T) {
		// Each predicate targets a different column, so a bug that swaps r.compiledPredicates'
		// entries (instead of leaving them aligned with r.opts.Predicates by index) would evaluate
		// a column against the wrong predicate and change the result, unlike two predicates on the
		// same column, which would still agree after a swap.
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				GreaterThanPredicate{Column: columns[3], Value: Int64Value(1979)}, // birth_year
				FuncPredicate{
					Column: columns[0], // first_name
					Keep: func(_ Column, value Value) bool {
						return len(value.Binary()) > 4
					},
				},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var expected []testPerson
		for _, p := range basicReaderTestData {
			if p.birthYear > 1979 && len(p.firstName) > 4 {
				expected = append(expected, p)
			}
		}
		require.NotEmpty(t, expected)
		require.Equal(t, expected, convertToTestPersons(actualRows))
	})

	t.Run("keeps a row that satisfies a single and predicate over two columns", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				AndPredicate{
					Left:  GreaterThanPredicate{Column: columns[3], Value: Int64Value(1979)},
					Right: LessThanPredicate{Column: columns[3], Value: Int64Value(1988)},
				},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var expected []testPerson
		for _, p := range basicReaderTestData {
			if p.birthYear > 1979 && p.birthYear < 1988 {
				expected = append(expected, p)
			}
		}
		require.NotEmpty(t, expected)
		require.Equal(t, expected, convertToTestPersons(actualRows))
	})

	t.Run("keeps a row a not predicate over a real leaf admits", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				NotPredicate{Inner: GreaterThanPredicate{Column: columns[3], Value: Int64Value(1985)}},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var expected []testPerson
		for _, p := range basicReaderTestData {
			if !(p.birthYear > 1985) {
				expected = append(expected, p)
			}
		}
		require.NotEmpty(t, expected)
		require.Equal(t, expected, convertToTestPersons(actualRows))
	})

	t.Run("excludes a row that fails the first predicate and passes the second", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				yearAfter(columns[3], 1985),
				nameLongerThan(columns[0], 4),
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		actualRows, err := readDatasetWithContext(t.Context(), r, 10)
		require.NoError(t, err)
		require.Equal(t, peopleNamed("Alice", "David", "Grace"), convertToTestPersons(actualRows))
	})

	t.Run("evaluates each predicate once for every row that reaches it", func(t *testing.T) {
		for _, batchSize := range []int{10, 3} {
			t.Run(fmt.Sprintf("with a batch size of %d", batchSize), func(t *testing.T) {
				dset, columns := buildTestDataset(t)

				calls := make([]int, 3)
				counting := func(i int, c Column, keep func(Value) bool) Predicate {
					return FuncPredicate{Column: c, Keep: func(_ Column, v Value) bool {
						calls[i]++
						return keep(v)
					}}
				}

				r := NewRowReader(RowReaderOptions{
					Dataset:          dset,
					ProjectedColumns: columns,
					Predicates: []Predicate{
						counting(0, columns[0], func(v Value) bool { return len(v.Binary()) > 4 }),
						counting(1, columns[3], func(v Value) bool { return v.Int64() > 1982 }),
						counting(2, columns[3], func(v Value) bool { return v.Int64() < 1990 }),
					},
				})
				t.Cleanup(func() { require.NoError(t, r.Close()) })

				rows, err := readDatasetWithContext(t.Context(), r, batchSize)
				require.NoError(t, err)
				require.Equal(t, peopleNamed("David", "Frank", "Grace"), convertToTestPersons(rows))
				require.Equal(t, []int{10, 6, 4}, calls)
			})
		}
	})

	t.Run("does not fill the column of a later predicate for a row that an earlier predicate rejected", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates:       []Predicate{nameLongerThan(columns[0], 4), yearAfter(columns[3], 1982)},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		buf := make([]Row, 10)
		n, err := r.Read(t.Context(), buf)
		require.NoError(t, err)
		require.Equal(t, 4, n)

		var withoutYear []string
		for _, row := range buf {
			if row.Values[3].IsNil() {
				withoutYear = append(withoutYear, basicReaderTestData[row.Index].firstName)
			}
		}
		require.ElementsMatch(t, []string{"John", "Jane", "Bob", "Eve"}, withoutYear)
	})

	t.Run("does not share a Values slice between rows after filtering", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				nameLongerThan(columns[0], 4),
				yearAfter(columns[3], 1982),
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		buf := make([]Row, 10)
		n, err := r.Read(t.Context(), buf)
		require.NoError(t, err)
		require.Equal(t, 4, n)

		seen := make(map[*Value]int)
		for i, row := range buf {
			require.NotEmpty(t, row.Values, "row %d", i)
			prev, shared := seen[&row.Values[0]]
			require.False(t, shared, "rows %d and %d share a Values slice", prev, i)
			seen[&row.Values[0]] = i
		}
	})

	t.Run("returns only the rows the predicate column holds, then io.EOF", func(t *testing.T) {
		for _, batchSize := range []int{3, 7, 10} {
			t.Run(fmt.Sprintf("with a batch size of %d", batchSize), func(t *testing.T) {
				dset, columns := buildShortColumnDataset(t)

				r := NewRowReader(RowReaderOptions{
					Dataset:          dset,
					ProjectedColumns: columns,
					Predicates: []Predicate{
						// A null passes, so a row past the end of the column would appear in the result.
						FuncPredicate{Column: columns[1], Keep: func(_ Column, v Value) bool { return v.IsNil() || v.Int64()%2 == 0 }},
					},
				})
				t.Cleanup(func() { require.NoError(t, r.Close()) })

				ctx, _ := xcap.NewCapture(t.Context(), nil)
				ctx, region := xcap.StartRegion(ctx, "logs.Reader")
				defer region.End()
				require.NoError(t, r.Open(ctx))

				indexes := readIndexes(ctx, t, r, func() []Row { return make([]Row, batchSize) })
				require.Equal(t, []int{0, 2, 4, 6}, indexes)
				require.Equal(t, int64(7), statsOf(region)[dataobj.StatDatasetPrimaryRowsRead.Name()])
			})
		}
	})

	t.Run("returns the error of reading a column", func(t *testing.T) {
		for _, tc := range []struct {
			name       string
			predicates func(columns []Column) []Predicate
		}{
			{
				name:       "when the first predicate column fails to read",
				predicates: func(c []Column) []Predicate { return []Predicate{yearAfter(c[3], 1982)} },
			},
			{
				name: "when a later predicate column fails to read",
				predicates: func(c []Column) []Predicate {
					return []Predicate{nameLongerThan(c[0], 4), yearAfter(c[3], 1982)}
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dset, columns := buildTestDataset(t)
				for _, page := range dset.(memDataset)[3].Pages {
					page.Data = PageData("not a page")
				}

				r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: tc.predicates(columns)})
				t.Cleanup(func() { require.NoError(t, r.Close()) })
				require.NoError(t, r.Open(t.Context()))

				n, err := r.Read(t.Context(), make([]Row, 10))
				require.Zero(t, n)
				require.Error(t, err)
				require.NotErrorIs(t, err, io.EOF)
				require.ErrorContains(t, err, "reading column")
			})
		}
	})

	t.Run("returns an error when a later predicate column holds fewer rows than the rows read", func(t *testing.T) {
		dset, columns := buildShortColumnDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				FuncPredicate{Column: columns[0], Keep: func(Column, Value) bool { return true }},
				FuncPredicate{Column: columns[1], Keep: func(Column, Value) bool { return true }},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		n, err := r.Read(t.Context(), make([]Row, 10))
		require.Zero(t, n)
		require.ErrorContains(t, err, "failed to fill rows: expected 10, got 7")
	})

	t.Run("returns only the projected columns when a predicate reads other columns", func(t *testing.T) {
		nonEmptyMiddleName := func(_ Column, v Value) bool { return len(v.Binary()) > 0 }

		for _, tc := range []struct {
			name       string
			projected  []int
			predicates func(c []Column) []Predicate
			keep       func(p testPerson) bool
		}{
			{
				name:      "when the first predicate reads one",
				projected: []int{2, 0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)}}
				},
				keep: func(p testPerson) bool { return p.birthYear > 1982 },
			},
			{
				name:      "when a later predicate reads one",
				projected: []int{2, 0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{
						FuncPredicate{Column: c[0], Keep: func(_ Column, v Value) bool { return len(v.Binary()) > 3 }},
						GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)},
					}
				},
				keep: func(p testPerson) bool { return len(p.firstName) > 3 && p.birthYear > 1982 },
			},
			{
				name:      "when each of two predicates reads its own",
				projected: []int{0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{
						GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)},
						FuncPredicate{Column: c[1], Keep: nonEmptyMiddleName},
					}
				},
				keep: func(p testPerson) bool { return p.birthYear > 1982 && p.middleName != "" },
			},
			{
				name:      "when an and predicate reads two",
				projected: []int{0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{AndPredicate{
						Left:  GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)},
						Right: FuncPredicate{Column: c[1], Keep: nonEmptyMiddleName},
					}}
				},
				keep: func(p testPerson) bool { return p.birthYear > 1982 && p.middleName != "" },
			},
			{
				name:      "when an or predicate reads two",
				projected: []int{0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{OrPredicate{
						Left:  LessThanPredicate{Column: c[3], Value: Int64Value(1976)},
						Right: EqualPredicate{Column: c[1], Value: BinaryValue([]byte("Grace"))},
					}}
				},
				keep: func(p testPerson) bool { return p.birthYear < 1976 || p.middleName == "Grace" },
			},
			{
				name:      "when a not predicate reads two",
				projected: []int{0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{NotPredicate{Inner: OrPredicate{
						Left:  GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)},
						Right: FuncPredicate{Column: c[1], Keep: nonEmptyMiddleName},
					}}}
				},
				keep: func(p testPerson) bool { return !(p.birthYear > 1982 || p.middleName != "") },
			},
			{
				name:      "when the projected column is also read and no secondary column is left",
				projected: []int{3},
				predicates: func(c []Column) []Predicate {
					return []Predicate{AndPredicate{
						Left:  GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)},
						Right: FuncPredicate{Column: c[1], Keep: nonEmptyMiddleName},
					}}
				},
				keep: func(p testPerson) bool { return p.birthYear > 1982 && p.middleName != "" },
			},
			{
				name:      "when two projected columns and two non-projected columns are read",
				projected: []int{2, 0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{
						GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)},
						FuncPredicate{Column: c[1], Keep: nonEmptyMiddleName},
					}
				},
				keep: func(p testPerson) bool { return p.birthYear > 1982 && p.middleName != "" },
			},
			{
				name:      "when the projection lists a column twice",
				projected: []int{0, 0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)}}
				},
				keep: func(p testPerson) bool { return p.birthYear > 1982 },
			},
			{
				name:      "when the projection is empty",
				projected: nil,
				predicates: func(c []Column) []Predicate {
					return []Predicate{GreaterThanPredicate{Column: c[3], Value: Int64Value(1982)}}
				},
				keep: func(p testPerson) bool { return p.birthYear > 1982 },
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				for _, batchSize := range []int{1, 2, 3, 20} {
					t.Run(fmt.Sprintf("with a batch size of %d", batchSize), func(t *testing.T) {
						dset, columns := buildTestDataset(t)

						projected := make([]Column, 0, len(tc.projected))
						for _, i := range tc.projected {
							projected = append(projected, columns[i])
						}
						r := NewRowReader(RowReaderOptions{
							Dataset:          dset,
							ProjectedColumns: projected,
							Predicates:       tc.predicates(columns),
						})
						t.Cleanup(func() { require.NoError(t, r.Close()) })

						var want [][]string
						for _, p := range basicReaderTestData {
							if !tc.keep(p) {
								continue
							}
							all := []string{p.firstName, p.middleName, p.lastName, strconv.FormatInt(p.birthYear, 10)}
							row := make([]string, 0, len(tc.projected))
							for _, i := range tc.projected {
								row = append(row, all[i])
							}
							want = append(want, row)
						}
						require.NotEmpty(t, want)

						require.Equal(t, want, readValuesWithReusedBuffer(t, r, batchSize))
					})
				}
			})
		}
	})

	t.Run("returns the rows read before a failed fill with only the projected columns", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		// The last page of the first names holds row 9 only. Rows 0 and 9 are the only ones born in
		// 1980 or 1981, so the fill of the first names reads row 0 and then fails on row 9.
		dset.(memDataset)[0].Pages[3].Data = PageData("not a page")

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: []Column{columns[0]},
			Predicates: []Predicate{
				FuncPredicate{Column: columns[3], Keep: func(_ Column, v Value) bool { return v.Int64() == 1980 || v.Int64() == 1981 }},
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		buf := make([]Row, 10)
		n, err := r.Read(t.Context(), buf)
		require.Error(t, err)
		require.NotErrorIs(t, err, io.EOF)
		require.Equal(t, 1, n)
		require.Len(t, buf[0].Values, 1)
	})

	t.Run("returns the rows read before a short fill with only the projected columns", func(t *testing.T) {
		firstNames := buildStringColumn(t, "first_name")
		years := buildInt64Column(t, "birth_year")
		for i, p := range basicReaderTestData {
			require.NoError(t, years.Append(i, Int64Value(p.birthYear)))
			if i < 5 {
				require.NoError(t, firstNames.Append(i, BinaryValue([]byte(p.firstName))))
			}
		}

		var memColumns []*MemColumn
		for _, builder := range []*ColumnBuilder{firstNames, years} {
			col, err := builder.Flush()
			require.NoError(t, err)
			memColumns = append(memColumns, col)
		}
		dset := FromMemory(memColumns)
		columns, err := result.Collect(dset.ListColumns(t.Context()))
		require.NoError(t, err)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: []Column{columns[0]},
			Predicates:       []Predicate{GreaterThanPredicate{Column: columns[1], Value: Int64Value(0)}},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })
		require.NoError(t, r.Open(t.Context()))

		buf := make([]Row, 10)
		n, err := r.Read(t.Context(), buf)
		require.ErrorContains(t, err, "failed to fill rows: expected 10, got 5")
		require.Equal(t, 5, n)
		for _, row := range buf[:n] {
			require.Len(t, row.Values, 1)
		}
	})

	t.Run("counts the columns that predicates read as primary columns", func(t *testing.T) {
		nonEmptyMiddleName := func(_ Column, v Value) bool { return len(v.Binary()) > 0 }

		for _, tc := range []struct {
			name          string
			projected     []int
			predicates    func(c []Column) []Predicate
			wantPrimary   int64
			wantSecondary int64
		}{
			{
				name:      "for a column that only one predicate reads",
				projected: []int{0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{GreaterThanPredicate{Column: c[3], Value: Int64Value(1980)}}
				},
				wantPrimary:   1,
				wantSecondary: 1,
			},
			{
				name:      "for a column that two predicates read",
				projected: []int{0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{
						GreaterThanPredicate{Column: c[3], Value: Int64Value(1980)},
						LessThanPredicate{Column: c[3], Value: Int64Value(1990)},
					}
				},
				wantPrimary:   1,
				wantSecondary: 1,
			},
			{
				name:      "for a column that one predicate reads twice",
				projected: []int{0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{AndPredicate{
						Left:  GreaterThanPredicate{Column: c[3], Value: Int64Value(1980)},
						Right: LessThanPredicate{Column: c[3], Value: Int64Value(1990)},
					}}
				},
				wantPrimary:   1,
				wantSecondary: 1,
			},
			{
				name:      "for a projected column that a predicate reads",
				projected: []int{3, 0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{GreaterThanPredicate{Column: c[3], Value: Int64Value(1980)}}
				},
				wantPrimary:   1,
				wantSecondary: 1,
			},
			{
				name:      "for two columns that only predicates read",
				projected: []int{0},
				predicates: func(c []Column) []Predicate {
					return []Predicate{
						GreaterThanPredicate{Column: c[3], Value: Int64Value(1980)},
						FuncPredicate{Column: c[1], Keep: nonEmptyMiddleName},
					}
				},
				wantPrimary:   2,
				wantSecondary: 1,
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dset, columns := buildTestDataset(t)

				projected := make([]Column, 0, len(tc.projected))
				for _, i := range tc.projected {
					projected = append(projected, columns[i])
				}
				r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: projected, Predicates: tc.predicates(columns)})
				t.Cleanup(func() { require.NoError(t, r.Close()) })

				stats := collectStats(t, r, 3)
				require.Equal(t, tc.wantPrimary, stats[dataobj.StatDatasetPrimaryColumns.Name()])
				require.Equal(t, tc.wantSecondary, stats[dataobj.StatDatasetSecondaryColumns.Name()])
			})
		}
	})

	t.Run("counts the bytes of a column that only a predicate reads as primary bytes", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: []Column{columns[0]},
			Predicates:       []Predicate{GreaterThanPredicate{Column: columns[3], Value: Int64Value(1982)}},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		stats := collectStats(t, r, 3)

		var wantSecondaryBytes int64
		for _, p := range basicReaderTestData {
			if p.birthYear > 1982 {
				wantSecondaryBytes += int64(len(p.firstName))
			}
		}
		const int64Bytes = 8
		require.Equal(t, wantSecondaryBytes, stats[dataobj.StatDatasetSecondaryRowBytes.Name()])
		require.Equal(t, int64Bytes*stats[dataobj.StatDatasetPrimaryRowsRead.Name()], stats[dataobj.StatDatasetPrimaryRowBytes.Name()])
	})

	t.Run("prunes the pages of a column that only a predicate reads", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: []Column{columns[2]},
			Predicates:       []Predicate{EqualPredicate{Column: columns[0], Value: BinaryValue([]byte("Henry"))}},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		stats := collectStats(t, r, 3)
		require.Positive(t, stats[dataobj.StatDatasetPagesPruned.Name()])
		require.Less(t, stats[dataobj.StatDatasetRowsAfterPruning.Name()], stats[dataobj.StatDatasetMaxRows.Name()])
	})
}

func Test_RowReader_Reset(t *testing.T) {
	t.Run("keeps reading the same data after a reset with unchanged options", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		_, err := readDataset(r, 3)
		require.NoError(t, err)

		r.Reset(RowReaderOptions{Dataset: dset, ProjectedColumns: columns})

		actualRows, err := readDataset(r, 3)
		require.NoError(t, err)
		require.Equal(t, basicReaderTestData, convertToTestPersons(actualRows))
	})

	t.Run("recompiles predicates after a reset with a different predicate set", func(t *testing.T) {
		// The two predicate sets target different columns and match different, non-empty sets of
		// rows, so a Reset that failed to clear r.compiledPredicates would leave the first set's
		// compiled predicate at index 0 ahead of the second set's, and the second read would keep
		// applying the first set instead of the second.
		dset, columns := buildTestDataset(t)
		r := NewRowReader(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				GreaterThanPredicate{Column: columns[3], Value: Int64Value(1985)}, // birth_year
			},
		})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		firstRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var firstExpected []testPerson
		for _, p := range basicReaderTestData {
			if p.birthYear > 1985 {
				firstExpected = append(firstExpected, p)
			}
		}
		require.NotEmpty(t, firstExpected)
		require.Equal(t, firstExpected, convertToTestPersons(firstRows))

		r.Reset(RowReaderOptions{
			Dataset:          dset,
			ProjectedColumns: columns,
			Predicates: []Predicate{
				FuncPredicate{
					Column: columns[0], // first_name
					Keep: func(_ Column, value Value) bool {
						return len(value.Binary()) > 4
					},
				},
			},
		})

		secondRows, err := readDataset(r, 3)
		require.NoError(t, err)

		var secondExpected []testPerson
		for _, p := range basicReaderTestData {
			if len(p.firstName) > 4 {
				secondExpected = append(secondExpected, p)
			}
		}
		require.NotEmpty(t, secondExpected)
		require.NotEqual(t, firstExpected, secondExpected)
		require.Equal(t, secondExpected, convertToTestPersons(secondRows))
	})

	t.Run("returns the columns of the new projection after a reset", func(t *testing.T) {
		dset, columns := buildTestDataset(t)
		predicates := []Predicate{GreaterThanPredicate{Column: columns[3], Value: Int64Value(1982)}}

		r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{columns[0]}, Predicates: predicates})
		t.Cleanup(func() { require.NoError(t, r.Close()) })

		rows, err := readDataset(r, 3)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		for _, row := range rows {
			require.Len(t, row.Values, 1)
		}

		r.Reset(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: predicates})

		rows, err = readDataset(r, 3)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		for _, row := range rows {
			require.Len(t, row.Values, len(columns))
		}

		r.Reset(RowReaderOptions{Dataset: dset, ProjectedColumns: []Column{columns[2], columns[0]}, Predicates: predicates})

		rows, err = readDataset(r, 3)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		for _, row := range rows {
			require.Len(t, row.Values, 2)
		}
	})
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
				Column: cols[1],                               // timestamp column
				Values: NewInt64ValueSetOf(50, 300, 150, 600), // 2 values in range. ~200 matching rows
			},
			want: rangeset.From(rangeset.Range{Start: 0, End: 750}), // Page 1 + 2
		},
		{
			name: "InPredicate with values all outside page ranges",
			predicate: InPredicate{
				Column: cols[1],                      // timestamp column
				Values: NewInt64ValueSetOf(150, 600), // Both outside all pages
			},
			want: rangeset.Set{}, // No pages should be included
		},
		{
			// A constant inner predicate reaches here whenever a translation layer reduces a
			// negated leaf whose column the data does not hold.
			name:      "not of a true predicate matches no row",
			predicate: NotPredicate{Inner: TruePredicate{}},
			want:      rangeset.Set{},
		},
		{
			// A nil predicate keeps every row, so negating one keeps none.
			name:      "not of a nil predicate matches no row",
			predicate: NotPredicate{Inner: nil},
			want:      rangeset.Set{},
		},
		{
			name:      "not of a false predicate matches every row",
			predicate: NotPredicate{Inner: FalsePredicate{}},
			want:      rangeset.From(rangeset.Range{Start: 0, End: 1000}),
		},
	}

	ctx := context.Background()
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRowReader(RowReaderOptions{
				Dataset:          ds,
				ProjectedColumns: cols,
				Predicates:       []Predicate{tc.predicate},
			})
			defer r.Close()

			require.NoError(t, r.Open(ctx))

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
		Dataset:          ds,
		ProjectedColumns: cols,
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
	// Generate a dataset with two columns, one with high cardinality and one with low cardinality.
	// The higher the cardinality, the more selective an equality predicate on the column is.
	// Columns that repeat each value in a run follow, for the IN patterns.
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

	// A logs section keeps the rows of one stream together, so its stream ID column repeats each
	// value in a run. Run lengths above 1 show where a lookup cache starts to pay off. A run
	// length of 1 repeats nothing, so it is the worst case for the cache.
	const streamCardinality = 200
	runLengths := []int64{1, 8, 64, 1000}
	for _, runLength := range runLengths {
		generator.Columns = append(generator.Columns, generatorColumnConfig{
			Tag:               fmt.Sprintf("stream_run_%d", runLength),
			Type:              ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "int64"},
			Encoding:          datasetmd.ENCODING_TYPE_DELTA,
			Compression:       datasetmd.COMPRESSION_TYPE_NONE,
			CardinalityTarget: streamCardinality,
			RunLength:         runLength,
		})
	}

	ds, cols := generator.Build(b, rand.Int63())

	var col1Value, col2Value int64
	idx := rand.Intn(generator.RowCount) // Randomly select a row index to use for the predicate values

	currentPos := 0
	batch := make([]Row, 1000)
	// read the dataset once to pick a random row for predicate generation
	reader := NewRowReader(RowReaderOptions{
		Dataset:          ds,
		ProjectedColumns: cols,
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

	type predicatePattern struct {
		name             string
		projectedColumns []Column
		predicates       []Predicate
	}

	predicatePatterns := []predicatePattern{
		{
			name:             "selectivity=combined",
			projectedColumns: cols[:2],
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
			name:             "selectivity=high",
			projectedColumns: cols[:2],
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
			name:             "selectivity=low",
			projectedColumns: cols[:2],
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

	// The IN set holds the lower half of the values, so about half of the rows match and a
	// lookup of an absent value is as common as one of a present value.
	inMembers := make([]int64, 0, streamCardinality/2)
	for i := range streamCardinality / 2 {
		inMembers = append(inMembers, int64(i))
	}
	for i, runLength := range runLengths {
		column := cols[2+i]
		for _, set := range []struct {
			name   string
			values ValueSet
		}{
			{"plain", NewInt64ValueSetOf(inMembers...)},
			{"memoized", NewMemoizedInt64ValueSetOf(inMembers...)},
		} {
			predicatePatterns = append(predicatePatterns, predicatePattern{
				name:             fmt.Sprintf("in=%s/run=%d", set.name, runLength),
				projectedColumns: []Column{column},
				predicates:       []Predicate{InPredicate{Column: column, Values: set.values}},
			})
		}
	}

	for _, pp := range predicatePatterns {
		b.Run(pp.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()

			var rowsRead int
			for b.Loop() {
				reader := NewRowReader(RowReaderOptions{
					Dataset:          ds,
					ProjectedColumns: pp.projectedColumns,
					Predicates:       pp.predicates,
				})
				require.NoError(b, reader.Open(context.Background()))

				batch := make([]Row, 10000)

				for {
					n, err := reader.Read(context.Background(), batch)
					rowsRead += n
					if err == io.EOF {
						break
					}
					if err != nil {
						b.Fatal(err)
					}
				}
				reader.Close()
			}
			b.ReportMetric(float64(rowsRead)/float64(b.N), "rows/op")
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

	// RunLength is how many consecutive generated values repeat each drawn value, as in a
	// column sorted by stream ID. Zero or one draws a new value every time. It applies to
	// number columns only.
	RunLength int64
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
			for range max(cfg.RunLength, 1) {
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

// Test_RowReader_Stats tests that the reader properly tracks statistics via xcap regions.
func Test_RowReader_Stats(t *testing.T) {
	dset, columns := buildTestDataset(t)

	r := NewRowReader(RowReaderOptions{
		Dataset:          dset,
		ProjectedColumns: columns,
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

	require.Equal(t, int64(2), obsMap[dataobj.StatDatasetReadCalls.Name()])
	require.Equal(t, int64(2), obsMap[dataobj.StatDatasetPrimaryColumns.Name()])
	require.Equal(t, int64(2), obsMap[dataobj.StatDatasetSecondaryColumns.Name()])
	require.Equal(t, int64(5), obsMap[dataobj.StatDatasetPrimaryColumnPages.Name()])
	require.Equal(t, int64(8), obsMap[dataobj.StatDatasetSecondaryColumnPages.Name()])

	require.Equal(t, int64(len(basicReaderTestData)), obsMap[dataobj.StatDatasetMaxRows.Name()])
	require.Equal(t, int64(3), obsMap[dataobj.StatDatasetRowsAfterPruning.Name()])
	require.Equal(t, int64(3), obsMap[dataobj.StatDatasetPrimaryRowsRead.Name()])
	require.Equal(t, int64(1), obsMap[dataobj.StatDatasetSecondaryRowsRead.Name()])
}

func Test_RowReader_PrimaryStats(t *testing.T) {
	const int64Bytes = 8

	var allNameBytes int64
	for _, p := range basicReaderTestData {
		allNameBytes += int64(len(p.firstName))
	}

	for _, tc := range []struct {
		name       string
		predicates func(c []Column) []Predicate
		wantRows   []testPerson
		wantBytes  int64
	}{
		{
			name:       "counts the bytes of the only predicate column for every row read",
			predicates: func(c []Column) []Predicate { return []Predicate{nameLongerThan(c[0], 4)} },
			wantRows:   peopleNamed("Alice", "Carol", "David", "Frank", "Grace", "Henry"),
			wantBytes:  allNameBytes,
		},
		{
			name: "counts a later column only for the rows that passed the earlier predicate",
			predicates: func(c []Column) []Predicate {
				return []Predicate{nameLongerThan(c[0], 4), yearAfter(c[3], 1982)}
			},
			wantRows:  peopleNamed("Alice", "David", "Frank", "Grace"),
			wantBytes: allNameBytes + 6*int64Bytes,
		},
		{
			name: "adds no bytes when a later predicate reuses a column an earlier one filled",
			predicates: func(c []Column) []Predicate {
				return []Predicate{nameLongerThan(c[0], 4), nameIsNot(c[0], "Alice")}
			},
			wantRows:  peopleNamed("Carol", "David", "Frank", "Grace", "Henry"),
			wantBytes: allNameBytes,
		},
		{
			name: "adds no bytes when a third predicate reuses a column the second one filled",
			predicates: func(c []Column) []Predicate {
				return []Predicate{nameLongerThan(c[0], 4), yearAfter(c[3], 1982), yearBefore(c[3], 1990)}
			},
			wantRows:  peopleNamed("David", "Frank", "Grace"),
			wantBytes: allNameBytes + 6*int64Bytes,
		},
		{
			name: "counts only the new column of a later predicate that also uses a filled one",
			predicates: func(c []Column) []Predicate {
				return []Predicate{
					nameLongerThan(c[0], 4),
					AndPredicate{Left: nameIsNot(c[0], "Alice"), Right: yearAfter(c[3], 1982)},
				}
			},
			wantRows:  peopleNamed("David", "Frank", "Grace"),
			wantBytes: allNameBytes + 6*int64Bytes,
		},
		{
			name: "counts every column of the first predicate for every row read",
			predicates: func(c []Column) []Predicate {
				return []Predicate{AndPredicate{Left: nameLongerThan(c[0], 4), Right: yearAfter(c[3], 1982)}}
			},
			wantRows:  peopleNamed("Alice", "David", "Frank", "Grace"),
			wantBytes: allNameBytes + 10*int64Bytes,
		},
		{
			name: "counts only the columns of the first predicate when it rejects every row",
			predicates: func(c []Column) []Predicate {
				return []Predicate{nameLongerThan(c[0], 100), yearAfter(c[3], 0)}
			},
			wantRows:  []testPerson{},
			wantBytes: allNameBytes,
		},
		{
			name: "counts no column of a predicate after the one that rejects every row",
			predicates: func(c []Column) []Predicate {
				return []Predicate{nameLongerThan(c[0], 4), yearAfter(c[3], 3000), nameIsNot(c[2], "x")}
			},
			wantRows:  []testPerson{},
			wantBytes: allNameBytes + 6*int64Bytes,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, batchSize := range []int{10, 3} {
				t.Run(fmt.Sprintf("with a batch size of %d", batchSize), func(t *testing.T) {
					dset, columns := buildTestDataset(t)
					r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: tc.predicates(columns)})
					t.Cleanup(func() { require.NoError(t, r.Close()) })

					rows, stats := readWithStats(t, r, batchSize)

					require.Equal(t, tc.wantRows, convertToTestPersons(rows))
					require.Equal(t, int64(len(basicReaderTestData)), stats[dataobj.StatDatasetPrimaryRowsRead.Name()])
					require.Equal(t, tc.wantBytes, stats[dataobj.StatDatasetPrimaryRowBytes.Name()])
				})
			}
		})
	}

	t.Run("counts only the rows read when the buffer is reused", func(t *testing.T) {
		for _, tc := range []struct {
			name       string
			predicates func(c []Column) []Predicate
			wantRows   []int
			wantBytes  int64
		}{
			{
				name: "when the first predicate column ends before the batch does",
				predicates: func(c []Column) []Predicate {
					return []Predicate{FuncPredicate{Column: c[1], Keep: func(Column, Value) bool { return true }}}
				},
				wantRows:  []int{0, 1, 2, 3, 4, 5, 6},
				wantBytes: 7 * int64Bytes,
			},
			{
				name: "when a later predicate uses a longer column",
				predicates: func(c []Column) []Predicate {
					return []Predicate{
						FuncPredicate{Column: c[1], Keep: func(_ Column, v Value) bool { return v.Int64()%2 == 0 }},
						FuncPredicate{Column: c[0], Keep: func(Column, Value) bool { return true }},
					}
				},
				wantRows:  []int{0, 2, 4, 6},
				wantBytes: 7*int64Bytes + 4*int64(len("a0")),
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dset, columns := buildShortColumnDataset(t)
				r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: tc.predicates(columns)})
				t.Cleanup(func() { require.NoError(t, r.Close()) })

				ctx, _ := xcap.NewCapture(t.Context(), nil)
				ctx, region := xcap.StartRegion(ctx, "logs.Reader")
				defer region.End()
				require.NoError(t, r.Open(ctx))

				// Reuse the buffer without clearing it. Rows past the last row read keep the values of
				// the previous batch.
				buf := make([]Row, 5)
				indexes := readIndexes(ctx, t, r, func() []Row { return buf })

				stats := statsOf(region)
				require.Equal(t, tc.wantRows, indexes)
				require.Equal(t, int64(7), stats[dataobj.StatDatasetPrimaryRowsRead.Name()])
				require.Equal(t, tc.wantBytes, stats[dataobj.StatDatasetPrimaryRowBytes.Name()])
			})
		}
	})
}

func Test_RowReader_StatStreamRelevantRows(t *testing.T) {
	keepMessagesWithOddNumber := func(c Column) Predicate {
		return FuncPredicate{Column: c, Keep: func(_ Column, v Value) bool { return v.Binary()[1]%2 == 1 }}
	}

	for _, tc := range []struct {
		name       string
		predicates func(c []Column) []Predicate
		wantStat   int64
		wantRecord bool
	}{
		{
			name: "counts the rows that pass the first predicate when it is a stream ID predicate",
			predicates: func(c []Column) []Predicate {
				return []Predicate{InPredicate{Column: c[0], Values: NewInt64ValueSetOf(10, 20)}}
			},
			wantStat:   6,
			wantRecord: true,
		},
		{
			name: "does not count the rows that a later predicate drops",
			predicates: func(c []Column) []Predicate {
				return []Predicate{InPredicate{Column: c[0], Values: NewInt64ValueSetOf(10, 20)}, keepMessagesWithOddNumber(c[2])}
			},
			wantStat:   6,
			wantRecord: true,
		},
		{
			name: "records zero when the first predicate is a stream ID predicate that matches no row",
			predicates: func(c []Column) []Predicate {
				return []Predicate{InPredicate{Column: c[0], Values: NewInt64ValueSetOf(15)}}
			},
			wantStat:   0,
			wantRecord: true,
		},
		{
			name: "records nothing when the stream ID predicate is not the first",
			predicates: func(c []Column) []Predicate {
				return []Predicate{keepMessagesWithOddNumber(c[2]), InPredicate{Column: c[0], Values: NewInt64ValueSetOf(10, 20)}}
			},
		},
		{
			name: "records nothing when the first predicate is an in predicate on another column",
			predicates: func(c []Column) []Predicate {
				return []Predicate{InPredicate{Column: c[1], Values: NewInt64ValueSetOf(10, 20)}}
			},
		},
		{
			name: "records nothing when the first predicate is not an in predicate",
			predicates: func(c []Column) []Predicate {
				return []Predicate{GreaterThanPredicate{Column: c[0], Value: Int64Value(5)}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dset, columns := buildStreamDataset(t)
			r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: tc.predicates(columns)})
			t.Cleanup(func() { require.NoError(t, r.Close()) })

			_, stats := readWithStats(t, r, 8)

			got, recorded := stats[dataobj.StatStreamRelevantRows.Name()]
			require.Equal(t, tc.wantRecord, recorded)
			require.Equal(t, tc.wantStat, got)
		})
	}
}

// readWithStats reads every row of r in batches. It returns the rows and a map from stat name to
// value.
func readWithStats(t *testing.T, r *RowReader, batchSize int) ([]Row, map[string]int64) {
	t.Helper()

	ctx, _ := xcap.NewCapture(t.Context(), nil)
	ctx, region := xcap.StartRegion(ctx, "logs.Reader")
	defer region.End()

	rows, err := readDatasetWithContext(ctx, r, batchSize)
	require.NoError(t, err)
	return rows, statsOf(region)
}

// readIndexes reads the open reader r until io.EOF. It returns the Index of every row it read.
// nextBuffer returns the buffer for each Read.
func readIndexes(ctx context.Context, t *testing.T, r *RowReader, nextBuffer func() []Row) []int {
	t.Helper()

	// maxReadAttempts bounds the loop, so a Read that never returns io.EOF fails the test instead
	// of hanging it. It is far above what any caller needs.
	const maxReadAttempts = 100

	var indexes []int
	for range maxReadAttempts {
		buf := nextBuffer()
		n, err := r.Read(ctx, buf)
		for _, row := range buf[:n] {
			indexes = append(indexes, row.Index)
		}

		if errors.Is(err, io.EOF) {
			require.Zero(t, n)
			return indexes
		}
		require.NoError(t, err)
	}
	t.Fatal("Read did not return io.EOF")
	return nil
}

func statsOf(region *xcap.Region) map[string]int64 {
	stats := make(map[string]int64)
	for _, obs := range region.Observations() {
		stats[obs.Statistic.Name()] = obs.Value.(int64)
	}
	return stats
}

func nameLongerThan(c Column, n int) Predicate {
	return FuncPredicate{Column: c, Keep: func(_ Column, v Value) bool { return len(v.Binary()) > n }}
}

func nameIsNot(c Column, name string) Predicate {
	return FuncPredicate{Column: c, Keep: func(_ Column, v Value) bool { return string(v.Binary()) != name }}
}

func yearAfter(c Column, year int64) Predicate {
	return FuncPredicate{Column: c, Keep: func(_ Column, v Value) bool { return v.Int64() > year }}
}

func yearBefore(c Column, year int64) Predicate {
	return FuncPredicate{Column: c, Keep: func(_ Column, v Value) bool { return v.Int64() < year }}
}

// peopleNamed returns the people of basicReaderTestData with the given first names, in dataset
// order.
func peopleNamed(names ...string) []testPerson {
	var out []testPerson
	for _, p := range basicReaderTestData {
		if slices.Contains(names, p.firstName) {
			out = append(out, p)
		}
	}
	return out
}

// buildShortColumnDataset returns a dataset with two columns, in this order: a string column "a"
// that holds ten rows and an int64 column "b" that holds seven rows.
func buildShortColumnDataset(t *testing.T) (Dataset, []Column) {
	t.Helper()

	a := buildStringColumn(t, "a")
	b := buildInt64Column(t, "b")
	for i := range 10 {
		require.NoError(t, a.Append(i, BinaryValue([]byte(fmt.Sprintf("a%d", i)))))
	}
	for i := range 7 {
		require.NoError(t, b.Append(i, Int64Value(int64(i))))
	}

	return memDatasetOf(t, a, b)
}

// buildStreamDataset returns a dataset with three columns, in this order: a stream ID column, an
// int64 column with the same values, and a message column. The stream IDs of its eight rows are 10,
// 20, 10, 30, 20, 10, 30 and 20. The message of row i is "m<i>".
func buildStreamDataset(t *testing.T) (Dataset, []Column) {
	t.Helper()

	streamID, err := NewColumnBuilder("stream_id", BuilderOptions{
		PageSizeHint: 4096,
		Type:         ColumnType{Physical: datasetmd.PHYSICAL_TYPE_INT64, Logical: "stream_id"},
		Compression:  datasetmd.COMPRESSION_TYPE_NONE,
		Encoding:     datasetmd.ENCODING_TYPE_DELTA,
		Statistics:   StatisticsOptions{StoreRangeStats: true},
	})
	require.NoError(t, err)
	other := buildInt64Column(t, "other")
	message := buildStringColumn(t, "message")

	for i, id := range []int64{10, 20, 10, 30, 20, 10, 30, 20} {
		require.NoError(t, streamID.Append(i, Int64Value(id)))
		require.NoError(t, other.Append(i, Int64Value(id)))
		require.NoError(t, message.Append(i, BinaryValue([]byte(fmt.Sprintf("m%d", i)))))
	}

	return memDatasetOf(t, streamID, other, message)
}

// memDatasetOf flushes the builders into a dataset. It returns the dataset and its columns, in the
// order of the builders.
func memDatasetOf(t *testing.T, builders ...*ColumnBuilder) (Dataset, []Column) {
	t.Helper()

	var memColumns []*MemColumn
	for _, builder := range builders {
		col, err := builder.Flush()
		require.NoError(t, err)
		memColumns = append(memColumns, col)
	}

	dset := FromMemory(memColumns)
	columns, err := result.Collect(dset.ListColumns(t.Context()))
	require.NoError(t, err)
	return dset, columns
}

func Test_RowReader_Open(t *testing.T) {
	t.Run("returns only the projected columns when Open runs again after it failed", func(t *testing.T) {
		dset, columns := buildTestDataset(t)

		r := NewRowReader(RowReaderOptions{
			Dataset:           &failingOnceDataset{Dataset: dset},
			ProjectedColumns:  []Column{columns[0]},
			Predicates:        []Predicate{GreaterThanPredicate{Column: columns[3], Value: Int64Value(1982)}},
			PrefetchAllOnOpen: true,
		})
		t.Cleanup(func() { _ = r.Close() })

		require.Error(t, r.Open(t.Context()))

		var want [][]string
		for _, p := range basicReaderTestData {
			if p.birthYear > 1982 {
				want = append(want, []string{p.firstName})
			}
		}
		require.NotEmpty(t, want)
		require.Equal(t, want, readValuesWithReusedBuffer(t, r, 2))
	})

	t.Run("returns an error when a predicate reads a nil column", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			predicate Predicate
		}{
			{"for a greater than predicate", GreaterThanPredicate{Value: Int64Value(1)}},
			{"for an in predicate", InPredicate{Values: NewInt64ValueSetOf(1)}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				dset, columns := buildTestDataset(t)

				r := NewRowReader(RowReaderOptions{Dataset: dset, ProjectedColumns: columns, Predicates: []Predicate{tc.predicate}})
				t.Cleanup(func() { require.NoError(t, r.Close()) })

				require.ErrorContains(t, r.Open(t.Context()), "nil column")
			})
		}
	})
}

// failingOnceDataset is a Dataset whose first ReadPages call fails.
type failingOnceDataset struct {
	Dataset
	failed bool
}

func (d *failingOnceDataset) ReadPages(ctx context.Context, pages []Page) result.Seq[PageData] {
	if !d.failed {
		d.failed = true
		return result.Iter(func(yield func(PageData) bool) error { return errors.New("failed to read pages") })
	}
	return d.Dataset.ReadPages(ctx, pages)
}

// readValuesWithReusedBuffer opens r and reads it until io.EOF, with one buffer of batchSize rows
// that it never clears. It returns the values of every row, as strings.
func readValuesWithReusedBuffer(t *testing.T, r *RowReader, batchSize int) [][]string {
	t.Helper()

	require.NoError(t, r.Open(t.Context()))

	// maxReadAttempts bounds the loop, so a Read that never returns io.EOF fails the test instead
	// of hanging it. It is far above what any caller needs.
	const maxReadAttempts = 100

	var values [][]string
	buf := make([]Row, batchSize)
	for range maxReadAttempts {
		n, err := r.Read(t.Context(), buf)
		for _, row := range buf[:n] {
			strs := make([]string, 0, len(row.Values))
			for _, v := range row.Values {
				if v.Type() == datasetmd.PHYSICAL_TYPE_INT64 {
					strs = append(strs, strconv.FormatInt(v.Int64(), 10))
				} else {
					strs = append(strs, string(v.Binary()))
				}
			}
			values = append(values, strs)
		}

		if errors.Is(err, io.EOF) {
			return values
		}
		require.NoError(t, err)
	}
	t.Fatal("Read did not return io.EOF")
	return nil
}

// collectStats reads every row of r in batches and returns the stats that the reader recorded, by
// name.
func collectStats(t *testing.T, r *RowReader, batchSize int) map[string]int64 {
	t.Helper()

	ctx, _ := xcap.NewCapture(t.Context(), nil)
	ctx, region := xcap.StartRegion(ctx, "logs.Reader")
	defer region.End()

	_, err := readDatasetWithContext(ctx, r, batchSize)
	require.NoError(t, err)

	stats := make(map[string]int64)
	for _, obs := range region.Observations() {
		stats[obs.Statistic.Name()] = obs.Value.(int64)
	}
	return stats
}
