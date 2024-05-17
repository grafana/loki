package v1

import "github.com/pkg/errors"

type BloomQuerier interface {
	Seek(BloomOffset) (*Bloom, error)
}

type LazyBloomIter struct {
	usePool bool

	b *Block
	m int // max page size in bytes

	// state
	initialized  bool
	err          error
	curPageIndex int
	curPage      *BloomPageDecoder
}

// NewLazyBloomIter returns a new lazy bloom iterator.
// If pool is true, the underlying byte slice of the bloom page
// will be returned to the pool for efficiency.
// This can only safely be used when the underlying bloom
// bytes don't escape the decoder.
func NewLazyBloomIter(b *Block, pool bool, maxSize int) *LazyBloomIter {
	return &LazyBloomIter{
		usePool: pool,
		b:       b,
		m:       maxSize,
	}
}

func (it *LazyBloomIter) ensureInit() {
	// TODO(owen-d): better control over when to decode
	if !it.initialized {
		if err := it.b.LoadHeaders(); err != nil {
			it.err = err
		}
		it.initialized = true
	}
}

// LoadOffset returns whether the bloom page at the given offset should
// be skipped (due to being too large) _and_ there's no error
func (it *LazyBloomIter) LoadOffset(offset BloomOffset) (skip bool) {
	it.ensureInit()

	// if we need a different page or the current page hasn't been loaded,
	// load the desired page
	if it.curPageIndex != offset.Page || it.curPage == nil {

		// drop the current page if it exists and
		// we're using the pool
		if it.curPage != nil && it.usePool {
			it.curPage.Relinquish()
		}

		r, err := it.b.reader.Blooms()
		if err != nil {
			it.err = errors.Wrap(err, "getting blooms reader")
			return false
		}
		decoder, skip, err := it.b.blooms.BloomPageDecoder(r, it.usePool, offset.Page, it.m, it.b.metrics)
		if err != nil {
			it.err = errors.Wrap(err, "loading bloom page")
			return false
		}

		if skip {
			return true
		}

		it.curPageIndex = offset.Page
		it.curPage = decoder

	}

	it.curPage.Seek(offset.ByteOffset)
	return false
}

func (it *LazyBloomIter) Next() bool {
	it.ensureInit()
	if it.err != nil {
		return false
	}
	return it.next()
}

func (it *LazyBloomIter) next() bool {
	if it.err != nil {
		return false
	}

	for it.curPageIndex < len(it.b.blooms.pageHeaders) {
		// first access of next page
		if it.curPage == nil {
			r, err := it.b.reader.Blooms()
			if err != nil {
				it.err = errors.Wrap(err, "getting blooms reader")
				return false
			}

			var skip bool
			it.curPage, skip, err = it.b.blooms.BloomPageDecoder(
				r,
				it.usePool,
				it.curPageIndex,
				it.m,
				it.b.metrics,
			)
			// this page wasn't skipped & produced an error, return
			if err != nil {
				it.err = err
				return false
			}
			if skip {
				// this page was skipped; check the next
				it.curPageIndex++
				continue
			}
		}

		if !it.curPage.Next() {
			// there was an error
			if it.curPage.Err() != nil {
				return false
			}

			// we've exhausted the current page, progress to next
			it.curPageIndex++
			// drop the current page if it exists and
			// we're using the pool
			if it.usePool {
				it.curPage.Relinquish()
			}
			it.curPage = nil
			continue
		}

		return true
	}

	// finished last page
	return false
}

func (it *LazyBloomIter) At() *Bloom {
	return it.curPage.At()
}

func (it *LazyBloomIter) Err() error {
	{
		if it.err != nil {
			return it.err
		}
		if it.curPage != nil {
			return it.curPage.Err()
		}
		return nil
	}
}
