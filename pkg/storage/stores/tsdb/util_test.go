package tsdb

import (
	"context"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type LoadableSeries struct {
	Labels labels.Labels
	Chunks index.ChunkMetas
}

func BuildIndex(t *testing.T, dir, tenant string, cases []LoadableSeries) *TSDBIndex {
	b := index.NewBuilder()

	for _, s := range cases {
		b.AddSeries(s.Labels, s.Chunks)
	}

	dst, err := b.Build(context.Background(), dir, tenant)
	require.Nil(t, err)
	location := dst.FilePath(dir)

	idx, err := LoadTSDB(location)
	require.Nil(t, err)
	return idx
}
