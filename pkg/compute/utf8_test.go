package compute

import (
	"strings"
	"testing"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
	"github.com/stretchr/testify/require"
)

func TestSubstrInsensitive(t *testing.T) {
	alloc := memory.MakeAllocator(nil)
	validity1 := memory.MakeBitmap(alloc, 1)
	validity1.Append(true)

	trueBitmap := memory.MakeBitmap(alloc, 1)
	trueBitmap.Append(true)
	true1 := columnar.MakeBool(trueBitmap, validity1)
	falseBitmap := memory.MakeBitmap(alloc, 1)
	falseBitmap.Append(false)
	false1 := columnar.MakeBool(falseBitmap, validity1)

	cases := []struct {
		name     string
		haystack *columnar.UTF8
		needle   string
		expected *columnar.Bool
	}{
		{name: "empty_haystack", haystack: columnar.MakeUTF8([]byte{}, []int32{}, memory.MakeBitmap(alloc, 0)), needle: "", expected: columnar.MakeBool(memory.MakeBitmap(alloc, 0), memory.MakeBitmap(alloc, 0))},
		{name: "empty_needle", haystack: columnar.MakeUTF8([]byte("test"), []int32{0, 4}, validity1), needle: "", expected: true1},
		{name: "match", haystack: columnar.MakeUTF8([]byte("test"), []int32{0, 4}, validity1), needle: "test", expected: true1},
		{name: "not match", haystack: columnar.MakeUTF8([]byte("test"), []int32{0, 4}, validity1), needle: "notest", expected: false1},
		{name: "case insensitive match", haystack: columnar.MakeUTF8([]byte("test"), []int32{0, 4}, validity1), needle: "Test", expected: true1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			alloc.Reclaim()
			results, err := SubstrInsensitiveAS(alloc, c.haystack, columnar.UTF8Scalar{Value: []byte(c.needle), Null: false})
			require.NoError(t, err)
			require.Equal(t, results.Len(), c.haystack.Len())
			if results.Len() > 0 {
				require.Equal(t, c.expected.Get(0), results.Get(0))
			}
		})
	}
}

func BenchmarkSubstrInsensitive(b *testing.B) {
	alloc := memory.MakeAllocator(nil)
	line := strings.Repeat("A", 100) + "target" + strings.Repeat("B", 100)
	haystack := columnar.MakeUTF8([]byte(line), []int32{0, int32(len(line))}, memory.MakeBitmap(alloc, 1))
	needle := columnar.UTF8Scalar{Value: []byte("TaRgEt"), Null: false}

	var totalSize int
	for i := range haystack.Len() {
		totalSize += len(haystack.Get(i))
	}

	for b.Loop() {
		alloc.Reclaim()
		SubstrInsensitiveAS(alloc, haystack, needle)
	}
	b.SetBytes(int64(totalSize) * int64(b.N))
}
