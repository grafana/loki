package iter

import (
	"github.com/grafana/loki/pkg/logproto"
)

type CacheEntryIterator interface {
	EntryIterator
	Base() EntryIterator
	Reset()
}

// cachedIterator is an iterator that caches iteration to be replayed later on.
type cachedIterator struct {
	cache []entryWithLabels
	base  EntryIterator // once set to nil it means we have to use the cache.

	curr int

	closeErr error
	iterErr  error
}

// NewCachedIterator creates an iterator that cache iteration result and can be iterated again
// after closing it without re-using the underlaying iterator `it`.
func NewCachedIterator(it EntryIterator, cap int) CacheEntryIterator {
	c := &cachedIterator{
		base:  it,
		cache: make([]entryWithLabels, 0, cap),
		curr:  -1,
	}
	return c
}

func (it *cachedIterator) Reset() {
	it.curr = -1
}

func (it *cachedIterator) Base() EntryIterator {
	return it.base
}

func (it *cachedIterator) consumeNext() bool {
	if it.base == nil {
		return false
	}
	ok := it.base.Next()
	// we're done with the base iterator.
	if !ok {
		it.closeErr = it.base.Close()
		it.iterErr = it.base.Error()
		it.base = nil
		return false
	}
	// we're caching entries
	it.cache = append(it.cache, entryWithLabels{entry: it.base.Entry(), labels: it.base.Labels()})
	it.curr++
	return true
}

func (it *cachedIterator) Next() bool {
	if len(it.cache) == 0 && it.base == nil {
		return false
	}
	if it.curr+1 >= len(it.cache) {
		if it.base != nil {
			return it.consumeNext()
		}
		return false
	}
	it.curr++
	return true
}

func (it *cachedIterator) Entry() logproto.Entry {
	if len(it.cache) == 0 || it.curr < 0 || it.curr >= len(it.cache) {
		return logproto.Entry{}
	}

	return it.cache[it.curr].entry
}

func (it *cachedIterator) Labels() string {
	if len(it.cache) == 0 || it.curr < 0 || it.curr >= len(it.cache) {
		return ""
	}
	return it.cache[it.curr].labels
}

func (it *cachedIterator) Error() error { return it.iterErr }

func (it *cachedIterator) Close() error {
	it.Reset()
	return it.closeErr
}

type CacheSampleIterator interface {
	SampleIterator
	Base() SampleIterator
	Reset()
}

// cachedIterator is an iterator that caches iteration to be replayed later on.
type cachedSampleIterator struct {
	cache []sampleWithLabels
	base  SampleIterator

	curr int

	closeErr error
	iterErr  error
}

// newSampleCachedIterator creates an iterator that cache iteration result and can be iterated again
// after closing it without re-using the underlaying iterator `it`.
func NewCachedSampleIterator(it SampleIterator, cap int) CacheSampleIterator {
	c := &cachedSampleIterator{
		base:  it,
		cache: make([]sampleWithLabels, 0, cap),
		curr:  -1,
	}
	return c
}

func (it *cachedSampleIterator) Base() SampleIterator {
	return it.base
}

func (it *cachedSampleIterator) Reset() {
	it.curr = -1
}

func (it *cachedSampleIterator) consumeNext() bool {
	if it.base == nil {
		return false
	}
	ok := it.base.Next()
	// we're done with the base iterator.
	if !ok {
		it.closeErr = it.base.Close()
		it.iterErr = it.base.Error()
		it.base = nil
		return false
	}
	// we're caching entries
	it.cache = append(it.cache, sampleWithLabels{Sample: it.base.Sample(), labels: it.base.Labels()})
	it.curr++
	return true
}

func (it *cachedSampleIterator) Next() bool {
	if len(it.cache) == 0 && it.base == nil {
		return false
	}
	if it.curr+1 >= len(it.cache) {
		if it.base != nil {
			return it.consumeNext()
		}
		return false
	}
	it.curr++
	return true
}

func (it *cachedSampleIterator) Sample() logproto.Sample {
	if len(it.cache) == 0 || it.curr < 0 || it.curr >= len(it.cache) {
		return logproto.Sample{}
	}
	return it.cache[it.curr].Sample
}

func (it *cachedSampleIterator) Labels() string {
	if len(it.cache) == 0 || it.curr < 0 || it.curr >= len(it.cache) {
		return ""
	}
	return it.cache[it.curr].labels
}

func (it *cachedSampleIterator) Error() error { return it.iterErr }

func (it *cachedSampleIterator) Close() error {
	it.Reset()
	return it.closeErr
}
