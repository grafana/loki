package compute

import (
	"strings"
	"testing"

	"github.com/grafana/regexp"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestRegexpMatch(t *testing.T) {
	alloc := memory.NewAllocator(nil)

	singleValueHaystack := columnartest.Array(t, columnar.KindUTF8, alloc, "test")
	nulllValueHaystack := columnartest.Array(t, columnar.KindUTF8, alloc, nil)
	emptyHaystack := columnartest.Array(t, columnar.KindUTF8, alloc)

	emptyRegexp := regexp.MustCompile("")
	nullValueRegexp := (*regexp.Regexp)(nil)
	matchingRegexp := regexp.MustCompile("test")
	nonMatchingRegexp := regexp.MustCompile("NOTtest")

	singleTrue := columnartest.Array(t, columnar.KindBool, alloc, true)
	singleFalse := columnartest.Array(t, columnar.KindBool, alloc, false)
	emptyResult := columnartest.Array(t, columnar.KindBool, alloc)
	nullResult := columnartest.Array(t, columnar.KindBool, alloc, nil)

	cases := []struct {
		name     string
		haystack columnar.Datum
		regexp   *regexp.Regexp
		expected columnar.Datum
	}{
		{
			name:     "empty_haystack",
			haystack: emptyHaystack,
			regexp:   matchingRegexp,
			expected: emptyResult,
		},
		{
			name:     "empty_regexp",
			haystack: singleValueHaystack,
			regexp:   emptyRegexp,
			expected: singleTrue,
		},
		{
			name:     "match",
			haystack: singleValueHaystack,
			regexp:   matchingRegexp,
			expected: singleTrue,
		},
		{
			name:     "not match",
			haystack: singleValueHaystack,
			regexp:   nonMatchingRegexp,
			expected: singleFalse,
		},
		{
			name:     "nil_haystack",
			haystack: nulllValueHaystack,
			regexp:   matchingRegexp,
			expected: nullResult,
		},
		{
			name:     "nil_needle",
			haystack: singleValueHaystack,
			regexp:   nullValueRegexp,
			expected: nullResult,
		},
		{
			name:     "nil_haystack_and_needle",
			haystack: nulllValueHaystack,
			regexp:   nullValueRegexp,
			expected: nullResult,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results, err := RegexpMatch(alloc, c.haystack, c.regexp, memory.Bitmap{})
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, c.expected, results)
		})
	}
}

func BenchmarkRegexpMatch(b *testing.B) {
	alloc := memory.NewAllocator(nil)
	line := strings.Repeat("A", 100) + "target" + strings.Repeat("B", 100)
	haystack := columnar.NewUTF8([]byte(line), []int32{0, int32(len(line))}, memory.NewBitmap(alloc, 1))
	regexp := regexp.MustCompile("A{100}targetB{100}")

	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = RegexpMatch(benchAlloc, haystack, regexp, memory.Bitmap{})
	}
	b.ReportMetric((float64(haystack.Len())*float64(b.N))/b.Elapsed().Seconds(), "values/s")
	b.SetBytes(int64(haystack.Size()))
}

func TestSubstrInsensitive(t *testing.T) {
	alloc := memory.NewAllocator(nil)

	singleValueHaystack := columnartest.Array(t, columnar.KindUTF8, alloc, "test")
	nulllValueHaystack := columnartest.Array(t, columnar.KindUTF8, alloc, nil)
	emptyHaystack := columnartest.Array(t, columnar.KindUTF8, alloc)

	emptyNeedle := columnartest.Scalar(t, columnar.KindUTF8, "")
	nullValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, nil)
	singleValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "test")
	singleUnknownValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "notest")
	singleUpperCaseValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "TEST")

	singleTrue := columnartest.Array(t, columnar.KindBool, alloc, true)
	singleFalse := columnartest.Array(t, columnar.KindBool, alloc, false)
	emptyResult := columnartest.Array(t, columnar.KindBool, alloc)
	nullResult := columnartest.Array(t, columnar.KindBool, alloc, nil)

	cases := []struct {
		name     string
		haystack columnar.Datum
		needle   columnar.Datum
		expected columnar.Datum
	}{
		{
			name:     "empty_haystack",
			haystack: emptyHaystack,
			needle:   singleValueNeedle,
			expected: emptyResult,
		},
		{
			name:     "empty_needle",
			haystack: singleValueHaystack,
			needle:   emptyNeedle,
			expected: singleTrue,
		},
		{
			name:     "match",
			haystack: singleValueHaystack,
			needle:   singleValueNeedle,
			expected: singleTrue,
		},
		{
			name:     "not match",
			haystack: singleValueHaystack,
			needle:   singleUnknownValueNeedle,
			expected: singleFalse,
		},
		{
			name:     "case insensitive match",
			haystack: singleValueHaystack,
			needle:   singleUpperCaseValueNeedle,
			expected: singleTrue,
		},
		{
			name:     "nil_haystack",
			haystack: nulllValueHaystack,
			needle:   singleValueNeedle,
			expected: nullResult,
		},
		{
			name:     "nil_needle",
			haystack: singleValueHaystack,
			needle:   nullValueNeedle,
			expected: nullResult,
		},
		{
			name:     "nil_haystack_and_needle",
			haystack: nulllValueHaystack,
			needle:   nullValueNeedle,
			expected: nullResult,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results, err := SubstrInsensitive(alloc, c.haystack, c.needle, memory.Bitmap{})
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, c.expected, results)
		})
	}
}

func BenchmarkSubstrInsensitive(b *testing.B) {
	alloc := memory.NewAllocator(nil)
	line := strings.Repeat("A", 100) + "target" + strings.Repeat("B", 100)
	haystack := columnar.NewUTF8([]byte(line), []int32{0, int32(len(line))}, memory.NewBitmap(alloc, 1))
	needle := columnartest.Scalar(b, columnar.KindUTF8, "target")

	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = SubstrInsensitive(benchAlloc, haystack, needle, memory.Bitmap{})
	}
	b.ReportMetric((float64(haystack.Len())*float64(b.N))/b.Elapsed().Seconds(), "values/s")
	b.SetBytes(int64(haystack.Size()))
}

func TestSubstr(t *testing.T) {
	alloc := memory.NewAllocator(nil)

	singleValueHaystack := columnartest.Array(t, columnar.KindUTF8, alloc, "test")
	nulllValueHaystack := columnartest.Array(t, columnar.KindUTF8, alloc, nil)
	emptyHaystack := columnartest.Array(t, columnar.KindUTF8, alloc)

	emptyNeedle := columnartest.Scalar(t, columnar.KindUTF8, "")
	nullValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, nil)
	singleValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "test")
	singleUnknownValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "notest")
	singleUpperCaseValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "TEST")

	singleTrue := columnartest.Array(t, columnar.KindBool, alloc, true)
	singleFalse := columnartest.Array(t, columnar.KindBool, alloc, false)
	emptyResult := columnartest.Array(t, columnar.KindBool, alloc)
	nullResult := columnartest.Array(t, columnar.KindBool, alloc, nil)

	cases := []struct {
		name     string
		haystack columnar.Datum
		needle   columnar.Datum
		expected columnar.Datum
	}{
		{
			name:     "empty_haystack",
			haystack: emptyHaystack,
			needle:   singleValueNeedle,
			expected: emptyResult,
		},
		{
			name:     "empty_needle",
			haystack: singleValueHaystack,
			needle:   emptyNeedle,
			expected: singleTrue,
		},
		{
			name:     "match",
			haystack: singleValueHaystack,
			needle:   singleValueNeedle,
			expected: singleTrue,
		},
		{
			name:     "not match",
			haystack: singleValueHaystack,
			needle:   singleUnknownValueNeedle,
			expected: singleFalse,
		},
		{
			name:     "case insensitive match",
			haystack: singleValueHaystack,
			needle:   singleUpperCaseValueNeedle,
			expected: singleFalse,
		},
		{
			name:     "nil_haystack",
			haystack: nulllValueHaystack,
			needle:   singleValueNeedle,
			expected: nullResult,
		},
		{
			name:     "nil_needle",
			haystack: singleValueHaystack,
			needle:   nullValueNeedle,
			expected: nullResult,
		},
		{
			name:     "nil_haystack_and_needle",
			haystack: nulllValueHaystack,
			needle:   nullValueNeedle,
			expected: nullResult,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			results, err := Substr(alloc, c.haystack, c.needle, memory.Bitmap{})
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, c.expected, results)
		})
	}
}

func BenchmarkSubstr(b *testing.B) {
	alloc := memory.NewAllocator(nil)
	line := strings.Repeat("A", 100) + "target" + strings.Repeat("B", 100)
	haystack := columnar.NewUTF8([]byte(line), []int32{0, int32(len(line))}, memory.NewBitmap(alloc, 1))
	needle := columnartest.Scalar(b, columnar.KindUTF8, "TaRgEt")

	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = Substr(benchAlloc, haystack, needle, memory.Bitmap{})
	}
	b.ReportMetric((float64(haystack.Len())*float64(b.N))/b.Elapsed().Seconds(), "values/s")
	b.SetBytes(int64(haystack.Size()))
}

// TestRegexpMatchWithSelection tests RegexpMatch with various selection patterns
func TestRegexpMatchWithSelection(t *testing.T) {
	alloc := memory.NewAllocator(nil)

	// Create a 5-element haystack array
	haystack := columnartest.Array(t, columnar.KindUTF8, alloc, "foo", "bar", "baz", "qux", "test")
	matchingRegexp := regexp.MustCompile("ba.")

	testCases := []struct {
		name      string
		haystack  columnar.Datum
		regexp    *regexp.Regexp
		selection memory.Bitmap
		expected  columnar.Datum
	}{
		{
			name:      "zero_bitmap_all_rows_selected",
			haystack:  haystack,
			regexp:    matchingRegexp,
			selection: memory.Bitmap{}, // all rows selected
			expected:  columnartest.Array(t, columnar.KindBool, alloc, false, true, true, false, false),
		},
		{
			name:     "full_selection_all_true",
			haystack: haystack,
			regexp:   matchingRegexp,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.AppendCount(true, 5)
				return bm
			}(),
			expected: columnartest.Array(t, columnar.KindBool, alloc, false, true, true, false, false),
		},
		{
			name:     "partial_selection_first_three",
			haystack: haystack,
			regexp:   matchingRegexp,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(true)
				bm.Append(true)
				bm.Append(true)
				bm.Append(false)
				bm.Append(false)
				return bm
			}(),
			// Unselected rows (indices 3, 4) should be null
			expected: columnartest.Array(t, columnar.KindBool, alloc, false, true, true, nil, nil),
		},
		{
			name:     "partial_selection_middle",
			haystack: haystack,
			regexp:   matchingRegexp,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(false)
				bm.Append(true)
				bm.Append(true)
				bm.Append(true)
				bm.Append(false)
				return bm
			}(),
			// Unselected rows (indices 0, 4) should be null
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, true, true, false, nil),
		},
		{
			name:     "all_false_selection",
			haystack: haystack,
			regexp:   matchingRegexp,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.AppendCount(false, 5)
				return bm
			}(),
			// All rows unselected, should all be null
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, nil, nil, nil, nil),
		},
		{
			name:     "single_row_selected",
			haystack: haystack,
			regexp:   matchingRegexp,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(false)
				bm.Append(false)
				bm.Append(true) // Only select the "baz" row
				bm.Append(false)
				bm.Append(false)
				return bm
			}(),
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, nil, true, nil, nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := RegexpMatch(alloc, tc.haystack, tc.regexp, tc.selection)
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tc.expected, result)
		})
	}
}

// TestSubstrInsensitiveWithSelection tests SubstrInsensitive with various selection patterns
func TestSubstrInsensitiveWithSelection(t *testing.T) {
	alloc := memory.NewAllocator(nil)

	// Create a 5-element haystack array
	haystack := columnartest.Array(t, columnar.KindUTF8, alloc, "FOO", "BAR", "BAZ", "QUX", "TEST")
	needle := columnartest.Scalar(t, columnar.KindUTF8, "ba")

	testCases := []struct {
		name      string
		haystack  columnar.Datum
		needle    columnar.Datum
		selection memory.Bitmap
		expected  columnar.Datum
	}{
		{
			name:      "zero_bitmap_selection_all_rows_selected",
			haystack:  haystack,
			needle:    needle,
			selection: memory.Bitmap{}, // all rows selected
			expected:  columnartest.Array(t, columnar.KindBool, alloc, false, true, true, false, false),
		},
		{
			name:     "full_selection_all_true",
			haystack: haystack,
			needle:   needle,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.AppendCount(true, 5)
				return bm
			}(),
			expected: columnartest.Array(t, columnar.KindBool, alloc, false, true, true, false, false),
		},
		{
			name:     "partial_selection_last_three",
			haystack: haystack,
			needle:   needle,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(false)
				bm.Append(false)
				bm.Append(true)
				bm.Append(true)
				bm.Append(true)
				return bm
			}(),
			// Unselected rows (indices 0, 1) should be null
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, nil, true, false, false),
		},
		{
			name:     "all_false_selection",
			haystack: haystack,
			needle:   needle,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.AppendCount(false, 5)
				return bm
			}(),
			// All rows unselected, should all be null
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, nil, nil, nil, nil),
		},
		{
			name:     "single_row_selected",
			haystack: haystack,
			needle:   needle,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(false)
				bm.Append(true) // Only select the "BAR" row
				bm.Append(false)
				bm.Append(false)
				bm.Append(false)
				return bm
			}(),
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, true, nil, nil, nil),
		},
		{
			name:     "empty_needle_with_selection",
			haystack: haystack,
			needle:   columnartest.Scalar(t, columnar.KindUTF8, ""),
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(true)
				bm.Append(false)
				bm.Append(true)
				bm.Append(false)
				bm.Append(true)
				return bm
			}(),
			// Empty needle matches all selected rows
			expected: columnartest.Array(t, columnar.KindBool, alloc, true, nil, true, nil, true),
		},
		{
			name:     "null_needle_with_selection",
			haystack: haystack,
			needle:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(true)
				bm.Append(true)
				bm.Append(false)
				bm.Append(false)
				bm.Append(true)
				return bm
			}(),
			// Null needle results in all nulls regardless of selection
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, nil, nil, nil, nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SubstrInsensitive(alloc, tc.haystack, tc.needle, tc.selection)
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tc.expected, result)
		})
	}
}

// TestSubstrWithSelection tests Substr with various selection patterns
func TestSubstrWithSelection(t *testing.T) {
	alloc := memory.NewAllocator(nil)

	// Create a 5-element haystack array
	haystack := columnartest.Array(t, columnar.KindUTF8, alloc, "foo", "bar", "baz", "qux", "test")
	needle := columnartest.Scalar(t, columnar.KindUTF8, "ba")

	testCases := []struct {
		name      string
		haystack  columnar.Datum
		needle    columnar.Datum
		selection memory.Bitmap
		expected  columnar.Datum
	}{
		{
			name:      "zero_bitmap_selection_all_rows_selected",
			haystack:  haystack,
			needle:    needle,
			selection: memory.Bitmap{}, // all rows selected
			expected:  columnartest.Array(t, columnar.KindBool, alloc, false, true, true, false, false),
		},
		{
			name:     "full_selection_all_true",
			haystack: haystack,
			needle:   needle,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.AppendCount(true, 5)
				return bm
			}(),
			expected: columnartest.Array(t, columnar.KindBool, alloc, false, true, true, false, false),
		},
		{
			name:     "partial_selection_alternating",
			haystack: haystack,
			needle:   needle,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(true)
				bm.Append(false)
				bm.Append(true)
				bm.Append(false)
				bm.Append(true)
				return bm
			}(),
			// Unselected rows (indices 1, 3) should be null
			expected: columnartest.Array(t, columnar.KindBool, alloc, false, nil, true, nil, false),
		},
		{
			name:     "all_false_selection",
			haystack: haystack,
			needle:   needle,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.AppendCount(false, 5)
				return bm
			}(),
			// All rows unselected, should all be null
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, nil, nil, nil, nil),
		},
		{
			name:     "single_row_selected",
			haystack: haystack,
			needle:   needle,
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(false)
				bm.Append(true) // Only select the "bar" row
				bm.Append(false)
				bm.Append(false)
				bm.Append(false)
				return bm
			}(),
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, true, nil, nil, nil),
		},
		{
			name:     "case_sensitive_no_match_with_selection",
			haystack: columnartest.Array(t, columnar.KindUTF8, alloc, "FOO", "BAR", "BAZ", "qux", "test"),
			needle:   needle, // "ba" in lowercase
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(true)
				bm.Append(true)
				bm.Append(true)
				bm.Append(false)
				bm.Append(false)
				return bm
			}(),
			// Case-sensitive match should fail for "BAR" and "BAZ"
			expected: columnartest.Array(t, columnar.KindBool, alloc, false, false, false, nil, nil),
		},
		{
			name:     "empty_needle_with_selection",
			haystack: haystack,
			needle:   columnartest.Scalar(t, columnar.KindUTF8, ""),
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(false)
				bm.Append(true)
				bm.Append(false)
				bm.Append(true)
				bm.Append(false)
				return bm
			}(),
			// Empty needle matches all selected rows
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, true, nil, true, nil),
		},
		{
			name:     "null_needle_with_selection",
			haystack: haystack,
			needle:   columnartest.Scalar(t, columnar.KindUTF8, nil),
			selection: func() memory.Bitmap {
				bm := memory.NewBitmap(alloc, 5)
				bm.Append(true)
				bm.Append(false)
				bm.Append(true)
				bm.Append(false)
				bm.Append(true)
				return bm
			}(),
			// Null needle results in all nulls regardless of selection
			expected: columnartest.Array(t, columnar.KindBool, alloc, nil, nil, nil, nil, nil),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Substr(alloc, tc.haystack, tc.needle, tc.selection)
			require.NoError(t, err)
			columnartest.RequireDatumsEqual(t, tc.expected, result)
		})
	}
}
