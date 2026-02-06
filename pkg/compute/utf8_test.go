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
			results, err := RegexpMatch(alloc, c.haystack, c.regexp)
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
		_, _ = RegexpMatch(benchAlloc, haystack, regexp)
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
			results, err := SubstrInsensitive(alloc, c.haystack, c.needle)
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
		_, _ = SubstrInsensitive(benchAlloc, haystack, needle)
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
			results, err := Substr(alloc, c.haystack, c.needle)
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
		_, _ = Substr(benchAlloc, haystack, needle)
	}
	b.ReportMetric((float64(haystack.Len())*float64(b.N))/b.Elapsed().Seconds(), "values/s")
	b.SetBytes(int64(haystack.Size()))
}
