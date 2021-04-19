package retention

import (
	"bytes"
	"sync"
)

type componentRef struct {
	components [][]byte
}

var (
	componentPools = sync.Pool{
		New: func() interface{} {
			return &componentRef{
				components: make([][]byte, 0, 5),
			}
		},
	}
	keyPool = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 512))
		},
	}
)

func getComponents() *componentRef {
	ref := componentPools.Get().(*componentRef)
	ref.components = ref.components[:0]
	return ref
}

func putComponents(ref *componentRef) {
	componentPools.Put(ref)
}

func getKeyBuffer(key []byte) (*bytes.Buffer, error) {
	buf := keyPool.Get().(*bytes.Buffer)
	if _, err := buf.Write(key); err != nil {
		putKeyBuffer(buf)
		return nil, err
	}
	return buf, nil
}

func putKeyBuffer(buf *bytes.Buffer) {
	buf.Reset()
	keyPool.Put(buf)
}
