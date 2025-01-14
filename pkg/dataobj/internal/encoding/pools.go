package encoding

import (
	"bytes"
	"sync"

	"github.com/gogo/protobuf/proto"
)

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
