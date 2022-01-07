package storage

import (
	"github.com/grafana/loki/pkg/storage/chunk"
)

// ByKey allow you to sort chunks by ID
type ByKey struct {
	chunks []chunk.Chunk
	scfg   chunk.SchemaConfig
}

func (a ByKey) Len() int      { return len(a.chunks) }
func (a ByKey) Swap(i, j int) { a.chunks[i], a.chunks[j] = a.chunks[j], a.chunks[i] }
func (a ByKey) Less(i, j int) bool {
	return a.scfg.ExternalKey(a.chunks[i]) < a.scfg.ExternalKey(a.chunks[j])
}
