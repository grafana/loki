package encoding

import (
	"bufio"
	"io"
	"sync"

	"github.com/gogo/protobuf/proto"
)

var protoBufferPool = sync.Pool{
	New: func() any {
		return new(proto.Buffer)
	},
}

var bufioPool = sync.Pool{
	New: func() any {
		return bufio.NewReader(nil)
	},
}

func getBufioReader(r io.Reader) (rd *bufio.Reader, release func()) {
	br := bufioPool.Get().(*bufio.Reader)
	br.Reset(r)
	return br, func() { bufioPool.Put(br) }
}
