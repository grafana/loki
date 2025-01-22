package encoding

import (
	"bufio"
	"bytes"
	"io"
	"sync"

	"github.com/gogo/protobuf/proto"
)

// bytesBufferPool is a pool of bytes.Buffer used during encoding.
// bytesBufferPool should only be used when the size of the buffer is unknown;
// all elements in the pool will eventually grow to be the same capacity.
//
// If the size of the buffer is known, use the bufpool package instead.
var bytesBufferPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

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
