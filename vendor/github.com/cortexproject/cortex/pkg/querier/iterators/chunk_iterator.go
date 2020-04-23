package iterators

import (
	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/chunk"
	promchunk "github.com/cortexproject/cortex/pkg/chunk/encoding"
)

type chunkIterator struct {
	chunk.Chunk
	it promchunk.Iterator

	// At() is called often in the heap code, so caching its result seems like
	// a good idea.
	cacheValid  bool
	cachedTime  int64
	cachedValue float64
}

// Seek advances the iterator forward to the value at or after
// the given timestamp.
func (i *chunkIterator) Seek(t int64) bool {
	i.cacheValid = false

	// We assume seeks only care about a specific window; if this chunk doesn't
	// contain samples in that window, we can shortcut.
	if int64(i.Through) < t {
		return false
	}

	return i.it.FindAtOrAfter(model.Time(t))
}

func (i *chunkIterator) AtTime() int64 {
	if i.cacheValid {
		return i.cachedTime
	}

	v := i.it.Value()
	i.cachedTime, i.cachedValue = int64(v.Timestamp), float64(v.Value)
	i.cacheValid = true
	return i.cachedTime
}

func (i *chunkIterator) At() (int64, float64) {
	if i.cacheValid {
		return i.cachedTime, i.cachedValue
	}

	v := i.it.Value()
	i.cachedTime, i.cachedValue = int64(v.Timestamp), float64(v.Value)
	i.cacheValid = true
	return i.cachedTime, i.cachedValue
}

func (i *chunkIterator) Next() bool {
	i.cacheValid = false
	return i.it.Scan()
}

func (i *chunkIterator) Err() error {
	return i.it.Err()
}
