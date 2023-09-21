package v1

import "io"

type BlockReader interface {
	Index() io.ReadSeekCloser
	Bloom(id uint32) io.ReadSeekCloser // id TBD, just placeholder for now
}

type Block struct {
	schema Schema
	header SeriesHeader // header for the entire block
	index  BlockIndex

	reader BlockReader // should this be decoupled from the struct (accepted as method arg instead)?
}
