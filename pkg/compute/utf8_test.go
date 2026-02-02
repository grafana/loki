package compute

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
)

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

	iterations := 0
	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = SubstrInsensitive(benchAlloc, haystack, needle)
		iterations++
	}
	b.ReportMetric(float64(iterations)/b.Elapsed().Seconds(), "values/s")
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

	var totalSize int
	for i := range haystack.Len() {
		totalSize += len(haystack.Get(i))
	}

	iterations := 0
	benchAlloc := memory.NewAllocator(nil)
	for b.Loop() {
		benchAlloc.Reclaim()
		_, _ = Substr(benchAlloc, haystack, needle)
		iterations++
	}
	b.ReportMetric(float64(iterations)/b.Elapsed().Seconds(), "values/s")
	b.SetBytes(int64(totalSize))
}
