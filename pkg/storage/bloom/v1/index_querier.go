package v1

import (
	"context"
	"sort"

	"github.com/prometheus/common/model"
)

type IndexQuerier interface {
	Series(context.Context) Iterator[*SeriesWithOffset]
}

type SeriesIterator interface {
	Iterator[*SeriesWithOffset]
	Reset()
}

type LazySeriesIter struct {
	b *Block

	// state
	initialized  bool
	err          error
	curPageIndex int
	curPage      *SeriesPageDecoder
}

// Decodes series pages one at a time and iterates through them
func NewLazySeriesIter(b *Block) *LazySeriesIter {
	return &LazySeriesIter{
		b: b,
	}
}

func (it *LazySeriesIter) ensureInit() {
	// TODO(owen-d): better control over when to decode
	if !it.initialized {
		if err := it.b.LoadHeaders(); err != nil {
			it.err = err
		}
		it.initialized = true
	}
}

// Seek returns an iterator over the pages where the first fingerprint is >= fp
func (it *LazySeriesIter) Seek(fp model.Fingerprint) (SeriesIterator, error) {
	it.ensureInit()

	// first potentially relevant page
	desiredPage := sort.Search(len(it.b.index.pageHeaders), func(i int) bool {
		header := it.b.index.pageHeaders[i]
		return header.ThroughFp >= fp
	})

	switch {
	case desiredPage == len(it.b.index.pageHeaders):
		return NewEmptyIter[*SeriesWithOffset](nil), nil

	case desiredPage == it.curPageIndex && it.curPage != nil:
		// desired page is the currently loaded page, can reuse
		it.curPage.Reset()
		return &MinFingerprintIter{
			minFp: fp,
			itr:   it.curPage,
		}, nil

	default:
		// need to load a new page
		var err error
		it.curPage, err = it.b.index.NewSeriesPageDecoder(
			it.b.reader.Index(),
			it.b.index.pageHeaders[it.curPageIndex],
		)
		if err != nil {
			return nil, err
		}

		return &MinFingerprintIter{
			minFp: fp,
			itr:   it.curPage,
		}, nil
	}
}

func (it *LazySeriesIter) Next() bool {
	it.ensureInit()
	if it.err != nil {
		return false
	}

	return it.next()
}

func (it *LazySeriesIter) next() bool {
	for it.curPageIndex < len(it.b.index.pageHeaders) {
		// first access of next page
		if it.curPage == nil {
			var (
				curHeader = it.b.index.pageHeaders[it.curPageIndex]
				err       error
			)
			it.curPage, err = it.b.index.NewSeriesPageDecoder(
				it.b.reader.Index(),
				curHeader,
			)
			if err != nil {
				it.err = err
				return false
			}
			continue
		}

		if !it.curPage.Next() {
			// there was an error
			if it.curPage.Err() != nil {
				return false
			}

			// we've exhausted the current page, progress to next
			it.curPageIndex++
			it.curPage = nil
			continue
		}

		return true
	}

	// finished last page
	return false
}

func (it *LazySeriesIter) At() *SeriesWithOffset {
	return it.curPage.At()
}

func (it *LazySeriesIter) Err() error {
	if it.err != nil {
		return it.err
	}
	if it.curPage != nil {
		return it.curPage.Err()
	}
	return nil
}

type MinFingerprintIter struct {
	minFp model.Fingerprint
	itr   *SeriesPageDecoder
}

func (i *MinFingerprintIter) Next() bool {
	for {
		if ok := i.itr.Next(); !ok {
			return false
		}

		if i.At().Fingerprint >= i.minFp {
			return true
		}
	}
}

func (i *MinFingerprintIter) At() *SeriesWithOffset {
	return i.itr.At()
}

func (i *MinFingerprintIter) Err() error {
	return i.itr.Err()
}

func (i *MinFingerprintIter) Reset() {
	i.itr.Reset()
}
