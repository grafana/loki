package parquet

import "github.com/parquet-go/parquet-go/internal/memory"

type bitmap struct {
	bits []uint64
}

func (m *bitmap) reset(size int) {
	size = (size + 63) / 64
	if cap(m.bits) < size {
		m.bits = make([]uint64, size, 2*size)
	} else {
		m.bits = m.bits[:size]
		m.clear()
	}
}

func (m *bitmap) clear() {
	for i := range m.bits {
		m.bits[i] = 0
	}
}

var (
	bitmapPool memory.Pool[bitmap]
)

func acquireBitmap(n int) *bitmap {
	return bitmapPool.Get(
		func() *bitmap { return &bitmap{bits: make([]uint64, n, 2*n)} },
		func(b *bitmap) { b.reset(n) },
	)
}

func releaseBitmap(b *bitmap) {
	bitmapPool.Put(b)
}
