package tests

import (
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// ByKey allow you to sort chunks by ID
type ByKey struct {
	chunks []chunk.Chunk
	scfg   config.SchemaConfig
}

func (a ByKey) Len() int      { return len(a.chunks) }
func (a ByKey) Swap(i, j int) { a.chunks[i], a.chunks[j] = a.chunks[j], a.chunks[i] }
func (a ByKey) Less(i, j int) bool {
	return a.scfg.ExternalKey(a.chunks[i].ChunkRef) < a.scfg.ExternalKey(a.chunks[j].ChunkRef)
}
