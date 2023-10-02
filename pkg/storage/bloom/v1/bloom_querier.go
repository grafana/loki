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

func (it *LazyBloomIter) Seek(offset BloomOffset) (*Bloom, error) {
	if it.err != nil {
		return nil, it.err
	}

	// if we need a different page or the current page hasn't been loaded,
	// load the desired page
	if it.curPageIndex != offset.Page || it.curPage == nil {

		// TODO(owen-d): better control over when to decode
		if err := it.b.LoadHeaders(); err != nil {
			return nil, errors.Wrap(err, "loading bloom headers")
		}

		decoder, err := it.b.blooms.BloomPageDecoder(it.b.reader.Blooms(), offset)
		if err != nil {
			return nil, errors.Wrap(err, "loading bloom page")
		}

		it.curPageIndex = offset.Page
		it.curPage = decoder

	}

	it.curPage.Seek(offset.ByteOffset)
	return it.curPage.Next()

}
