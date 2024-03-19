package v1

import "github.com/pkg/errors"

type BloomQuerier interface {
	Seek(BloomOffset) (*Bloom, error)
}

type LazyBloomIter struct {
	b *Block

	// state
	initialized  bool
	err          error
	curPageIndex int
	curPage      *BloomPageDecoder
}

func NewLazyBloomIter(b *Block) *LazyBloomIter {
	return &LazyBloomIter{
		b: b,
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

func (it *LazyBloomIter) setPage(dec *BloomPageDecoder) {
	if it.curPage != nil {
		it.curPage.Close()
	}
	it.curPage = dec
}

func (it *LazyBloomIter) Seek(offset BloomOffset) {
	it.ensureInit()

	// if we need a different page or the current page hasn't been loaded,
	// load the desired page
	if it.curPageIndex != offset.Page || it.curPage == nil {

		r, err := it.b.reader.Blooms()
		if err != nil {
			it.err = errors.Wrap(err, "getting blooms reader")
			it.setPage(nil)
			return
		}
		decoder, err := it.b.blooms.BloomPageDecoder(r, offset.Page)
		if err != nil {
			it.err = errors.Wrap(err, "loading bloom page")
			it.setPage(nil)
			return
		}

		it.curPageIndex = offset.Page
		it.setPage(decoder)
	}

	it.curPage.Seek(offset.ByteOffset)
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

			decoder, err := it.b.blooms.BloomPageDecoder(r, it.curPageIndex)
			if err != nil {
				it.err = err
				it.setPage(nil)
				return false
			}
			it.setPage(decoder)
			continue
		}

		if !it.curPage.Next() {
			// there was an error
			if it.curPage.Err() != nil {
				it.err = it.curPage.Err()
				it.setPage(nil)
				return false
			}
			// we've exhausted the current page, progress to next
			it.curPageIndex++
			it.setPage(nil)
			continue
		}

		return true
	}

	// finished last page
	it.setPage(nil)
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
