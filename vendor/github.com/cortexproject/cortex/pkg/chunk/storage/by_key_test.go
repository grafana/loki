package storage

import (
	"github.com/cortexproject/cortex/pkg/chunk"
)

// ByKey allow you to sort chunks by ID
type ByKey []chunk.Chunk

func (cs ByKey) Len() int           { return len(cs) }
func (cs ByKey) Swap(i, j int)      { cs[i], cs[j] = cs[j], cs[i] }
func (cs ByKey) Less(i, j int) bool { return cs[i].ExternalKey() < cs[j].ExternalKey() }
