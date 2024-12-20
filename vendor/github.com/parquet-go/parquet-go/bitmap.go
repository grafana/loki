package parquet

import "sync"

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
	bitmapPool sync.Pool // *bitmap
)

func acquireBitmap(n int) *bitmap {
	b, _ := bitmapPool.Get().(*bitmap)
	if b == nil {
		b = &bitmap{bits: make([]uint64, n, 2*n)}
	} else {
		b.reset(n)
	}
	return b
}

func releaseBitmap(b *bitmap) {
	if b != nil {
		bitmapPool.Put(b)
	}
}
