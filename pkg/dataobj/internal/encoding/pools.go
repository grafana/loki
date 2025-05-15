package encoding

import (
	"sync"

	"github.com/gogo/protobuf/proto"
)

var protoBufferPool = sync.Pool{
	New: func() any {
		return new(proto.Buffer)
	},
}
