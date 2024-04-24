package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func BenchmarkExternalKey(b *testing.B) {
	b.ReportAllocs()
	var cfg config.SchemaConfig
	require.Nil(b, yaml.Unmarshal([]byte(`
configs:
  - index:
      period: 24h
      prefix: loki_dev_004_index_
    object_store: gcs
    schema: v12
    store: boltdb-shipper
`), &cfg))
	require.Nil(b, cfg.Validate())
	key := "fake/57f628c7f6d57aad/162c699f000:162c69a07eb:eb242d99"
	chunk, err := chunk.ParseExternalKey("fake", key)
	require.Nil(b, err)

	for i := 0; i < b.N; i++ {
		_ = cfg.ExternalKey(chunk.ChunkRef)
	}
}
