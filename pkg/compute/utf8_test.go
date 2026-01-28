package compute

import (
	"strings"
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/stretchr/testify/require"
)

func TestSubstrInsensitive(t *testing.T) {
	alloc := memory.MakeAllocator(nil)

	singleValueHaystack := columnartest.Array(t, columnar.KindUTF8, alloc, "test")
	emptyHaystack := columnartest.Array(t, columnar.KindUTF8, alloc)

	emptyNeedle := columnartest.Scalar(t, columnar.KindUTF8, "")
	singleValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "test")
	singleUnknownValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "notest")
	singleUpperCaseValueNeedle := columnartest.Scalar(t, columnar.KindUTF8, "TEST")

	singleTrue := columnartest.Array(t, columnar.KindBool, alloc, true)
	singleFalse := columnartest.Array(t, columnar.KindBool, alloc, false)
	emptyResult := columnartest.Array(t, columnar.KindBool, alloc)

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
	alloc := memory.MakeAllocator(nil)
	line := strings.Repeat("A", 100) + "target" + strings.Repeat("B", 100)
	haystack := columnar.MakeUTF8([]byte(line), []int32{0, int32(len(line))}, memory.MakeBitmap(alloc, 1))
	needle := columnartest.Scalar(b, columnar.KindUTF8, "TaRgEt")

	var totalSize int
	for i := range haystack.Len() {
		totalSize += len(haystack.Get(i))
	}

	for b.Loop() {
		alloc.Reclaim()
		SubstrInsensitive(alloc, haystack, needle)
	}
	b.SetBytes(int64(totalSize))
}
