package v1

import (
	"io"

	"github.com/pkg/errors"
)

type BlockReader interface {
	Index() io.ReadSeekCloser
	Bloom(id uint32) io.ReadSeekCloser // id TBD, just placeholder for now
}

type Block struct {
	// schema, series index
	index BlockIndex
	// synthetic header for the entire block
	// built from all the pages in the index
	header SeriesHeader

	reader BlockReader // should this be decoupled from the struct (accepted as method arg instead)?
}

func (b *Block) LoadHeaders(data []byte) error {
	if err := b.index.Decode(data); err != nil {
		return errors.Wrap(err, "decoding index")
	}

	return nil
}
