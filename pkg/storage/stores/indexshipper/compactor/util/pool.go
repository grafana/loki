package util

import (
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
)

func getComponents() *componentRef {
	ref := componentPools.Get().(*componentRef)
	ref.components = ref.components[:0]
	return ref
}

func putComponents(ref *componentRef) {
	componentPools.Put(ref)
}
