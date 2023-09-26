package v1

import (
	"context"

	"github.com/grafana/loki/pkg/util/encoding"
)

type IndexQuerier interface {
	Series(context.Context) Iterator[SeriesWithOffset]
}

type LazySeriesIter struct {
	b    *Block
	data []byte

	// state
	initialized bool
	err         error
	pageIndex   int
	curPage     *SeriesPageDecoder
}

// Decodes series pages one at a time and iterates through them
func NewLazySeriesIter(b *Block) *LazySeriesIter {
	return &LazySeriesIter{
		b: b,

		pageIndex: 0,
	}
}

func (it *LazySeriesIter) Next() bool {
	if it.err != nil {
		return false
	}

	if !it.initialized {
		// TODO(owen-d): better control over when to decode
		if err := it.b.LoadHeaders(); err != nil {
			it.err = err
			return false
		}
		it.data, _ = it.b.LoadIndex()
	}

	return it.next()
}

func (it *LazySeriesIter) next() bool {
	for it.pageIndex < len(it.b.index.pageHeaders) {
		// first access of next page
		if it.curPage == nil {
			var (
				p   SeriesPage
				err error
			)
			decbuf := encoding.DecWith(it.data[it.b.index.pageHeaders[it.pageIndex].Offset : it.b.index.pageHeaders[it.pageIndex].Offset+it.b.index.pageHeaders[it.pageIndex].Len])
			it.curPage, err = p.DecodeLazy(&decbuf, it.b.index.schema.DecompressorPool(), it.b.index.pageHeaders[it.pageIndex].DecompressedLen)
			if err != nil {
				it.err = err
				return false
			}
		}

		if !it.curPage.Next() {
			it.pageIndex++
			it.curPage = nil
			continue
		}

		return true
	}

	return false
}

func (it *LazySeriesIter) At() (res SeriesWithOffset) {
	res, it.err = it.curPage.At()
	return res
}

func (it *LazySeriesIter) Err() error { return it.err }
