package bufpool

import (
	"bytes"
	"sync"
)

var unsizedPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}
