package storage

import (
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

// cachedIterator is an iterator that caches iteration to be replayed later on.
type cachedIterator struct {
	cache []*logproto.Entry
	base  iter.EntryIterator

	labels string
	curr   int

	closeErr error
	iterErr  error
}

// newCachedIterator creates an iterator that cache iteration result and can be iterated again
// after closing it without re-using the underlaying iterator `it`.
// The cache iterator should be used for entries that belongs to the same stream only.
func newCachedIterator(it iter.EntryIterator, cap int) *cachedIterator {
	c := &cachedIterator{
		base:  it,
		cache: make([]*logproto.Entry, 0, cap),
		curr:  -1,
	}
	c.load()
	return c
}

func (it *cachedIterator) reset() {
	it.curr = -1
}

func (it *cachedIterator) load() {
	if it.base != nil {
		defer func() {
			it.closeErr = it.base.Close()
			it.iterErr = it.base.Error()
			it.base = nil
			it.reset()
		}()
		// set labels using the first entry
		if !it.base.Next() {
			return
		}
		it.labels = it.base.Labels()

		// add all entries until the base iterator is exhausted
		for {
			e := it.base.Entry()
			it.cache = append(it.cache, &e)
			if !it.base.Next() {
				break
			}
		}

	}
}

func (it *cachedIterator) Next() bool {
	if len(it.cache) == 0 {
		it.cache = nil
		return false
	}
	if it.curr+1 >= len(it.cache) {
		return false
	}
	it.curr++
	return it.curr < len(it.cache)
}

func (it *cachedIterator) Entry() logproto.Entry {
	if len(it.cache) == 0 {
		return logproto.Entry{}
	}
	if it.curr < 0 {
		return *it.cache[0]
	}
	return *it.cache[it.curr]
}

func (it *cachedIterator) Labels() string {
	return it.labels
}

func (it *cachedIterator) Error() error { return it.iterErr }

func (it *cachedIterator) Close() error {
	it.reset()
	return it.closeErr
}

// cachedIterator is an iterator that caches iteration to be replayed later on.
type cachedSampleIterator struct {
	cache []logproto.Sample
	base  iter.SampleIterator

	labels string
	curr   int

	closeErr error
	iterErr  error
}

// newSampleCachedIterator creates an iterator that cache iteration result and can be iterated again
// after closing it without re-using the underlaying iterator `it`.
// The cache iterator should be used for entries that belongs to the same stream only.
func newCachedSampleIterator(it iter.SampleIterator, cap int) *cachedSampleIterator {
	c := &cachedSampleIterator{
		base:  it,
		cache: make([]logproto.Sample, 0, cap),
		curr:  -1,
	}
	c.load()
	return c
}

func (it *cachedSampleIterator) reset() {
	it.curr = -1
}

func (it *cachedSampleIterator) load() {
	if it.base != nil {
		defer func() {
			it.closeErr = it.base.Close()
			it.iterErr = it.base.Error()
			it.base = nil
			it.reset()
		}()
		// set labels using the first entry
		if !it.base.Next() {
			return
		}
		it.labels = it.base.Labels()

		// add all entries until the base iterator is exhausted
		for {
			it.cache = append(it.cache, it.base.Sample())
			if !it.base.Next() {
				break
			}
		}

	}
}

func (it *cachedSampleIterator) Next() bool {
	if len(it.cache) == 0 {
		it.cache = nil
		return false
	}
	if it.curr+1 >= len(it.cache) {
		return false
	}
	it.curr++
	return it.curr < len(it.cache)
}

func (it *cachedSampleIterator) Sample() logproto.Sample {
	if len(it.cache) == 0 {
		return logproto.Sample{}
	}
	if it.curr < 0 {
		return it.cache[0]
	}
	return it.cache[it.curr]
}

func (it *cachedSampleIterator) Labels() string {
	return it.labels
}

func (it *cachedSampleIterator) Error() error { return it.iterErr }

func (it *cachedSampleIterator) Close() error {
	it.reset()
	return it.closeErr
}
