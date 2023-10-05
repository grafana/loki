package bloomshipper

import (
	"github.com/prometheus/common/model"
	"io"
)

// todo remove once bloom library is merged
type BlockReader interface {
	Index() (io.ReadSeeker, error)
	Blooms() (io.ReadSeeker, error)
}

type BlockQuerier interface {
	Seek(fp model.Fingerprint) error
	Next() bool
	At() *SeriesWithBloom
	Err() error
}

type SeriesWithBloom struct {
}
