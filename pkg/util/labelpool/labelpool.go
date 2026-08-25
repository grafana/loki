package labelpool

import (
	"sync"

	"github.com/prometheus/prometheus/model/labels"
)

var scratchPool = &sync.Pool{
	New: func() any {
		b := labels.NewScratchBuilder(8)
		return &b
	},
}

// Get returns a [labels.ScratchBuilder] from the pool, or creates one of if
// the pool is empty. The returned builder is always in a fresh state.
//
// [labels.ScratchBuilder.Overwrite] is only valid to use with pooled builders
// if the all references to the overwritten labels end before the builder is
// returned to the pool.
func Get() *labels.ScratchBuilder {
	b := scratchPool.Get().(*labels.ScratchBuilder)
	b.Reset()
	return b
}

// Put returns a [labels.ScratchBuilder] back to the pool.
func Put(b *labels.ScratchBuilder) { scratchPool.Put(b) }
