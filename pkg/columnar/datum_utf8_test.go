package columnar_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

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
