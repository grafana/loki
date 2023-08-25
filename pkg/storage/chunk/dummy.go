package chunk

import (
	"io"

	"github.com/grafana/loki/pkg/util/filter"
	"github.com/prometheus/common/model"
)

func newDummyChunk() *dummyChunk {
	return &dummyChunk{}
}

// dummyChunk implements chunk.Data
// It is a placeholder chunk with Encoding(0)
// It can be used in tests where the content of a chunk is irrelevant.
type dummyChunk struct{}

func (chk *dummyChunk) Add(sample model.SamplePair) (Data, error) {
	return nil, nil
}

func (chk *dummyChunk) Marshal(io.Writer) error {
	return nil
}

func (chk *dummyChunk) UnmarshalFromBuf([]byte) error {
	return nil
}

func (chk *dummyChunk) Encoding() Encoding {
	return Dummy
}

func (chk *dummyChunk) Rebound(start, end model.Time, filter filter.Func) (Data, error) {
	return nil, nil
}

func (chk *dummyChunk) Size() int {
	return 0
}

func (chk *dummyChunk) UncompressedSize() int {
	return 0
}

func (chk *dummyChunk) Entries() int {
	return 0
}

func (chk *dummyChunk) Utilization() float64 {
	return 0
}
