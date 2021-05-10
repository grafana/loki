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
			return &keyPair{
				key:   bytes.NewBuffer(make([]byte, 0, 8)),
				value: bytes.NewBuffer(make([]byte, 0, 512)),
			}
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

type keyPair struct {
	key   *bytes.Buffer
	value *bytes.Buffer
}

func getKeyPairBuffer(key, value []byte) (*keyPair, error) {
	keyBuf := keyPool.Get().(*keyPair)
	if _, err := keyBuf.key.Write(key); err != nil {
		putKeyBuffer(keyBuf)
		return nil, err
	}
	if _, err := keyBuf.value.Write(value); err != nil {
		putKeyBuffer(keyBuf)
		return nil, err
	}
	return keyBuf, nil
}

func putKeyBuffer(pair *keyPair) {
	pair.key.Reset()
	pair.value.Reset()
	keyPool.Put(pair)
}
