package bufpool

import (
	"bufio"
	"io"
	"sync"
)

var bufioPool = sync.Pool{
	New: func() any {
		return bufio.NewReader(nil)
	},
}

// GetReader returns a pooled [bufio.Reader]. The returned reader is reset to
// read from r.
func GetReader(r io.Reader) *bufio.Reader {
	br := bufioPool.Get().(*bufio.Reader)
	br.Reset(r)
	return br
}

// PutReader puts the reader back into the pool. It is not safe to use the
// reader after calling PutReader.
func PutReader(br *bufio.Reader) {
	br.Reset(nil) // Release reference to underlying reader.
	bufioPool.Put(br)
}
