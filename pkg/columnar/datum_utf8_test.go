package columnar_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestUTF8_SlicedSize(t *testing.T) {
	alloc := memory.NewAllocator(nil)
	defer alloc.Free()

	// Build a UTF8 array: ["aa", "bbbb", "cccccc", "dd"]
	// Data lengths:          2      4        6        2   = 14 total
	b := columnar.NewUTF8Builder(alloc)
	b.AppendValue([]byte("aa"))
	b.AppendValue([]byte("bbbb"))
	b.AppendValue([]byte("cccccc"))
	b.AppendValue([]byte("dd"))
	arr := b.Build()

	fullSize := arr.Size()

	// Slice [1, 3) = ["bbbb", "cccccc"], data = 10 bytes.
	sliced := arr.Slice(1, 3)
	slicedSize := sliced.Size()

	require.Less(t, slicedSize, fullSize, "sliced Size() must be less than full Size()")

	// Expected: DataLen=10, offsets=3*4=12, validity=0 (no nulls).
	require.Equal(t, 22, slicedSize)
}

func TestUTF8Builder_AppendManyValues(t *testing.T) {
	// Regression test: UTF8Builder.needGrow only checked offsets capacity, not
	// the validity bitmap. Because offsets (4 bytes each) and the bitmap (1 bit
	// each) grow at different rates, the bitmap could fill up first, causing a
	// panic in AppendUnsafe.
	alloc := memory.NewAllocator(nil)
	defer alloc.Free()

	b := columnar.NewUTF8Builder(alloc)
	require.NotPanics(t, func() {
		for i := range 1000 {
			b.AppendValue([]byte("x"))
			_ = i
		}
	})

	arr := b.Build()
	require.Equal(t, 1000, arr.Len())
}
