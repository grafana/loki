package tsdb

import (
	"context"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

type LoadableSeries struct {
	Labels labels.Labels
	Chunks index.ChunkMetas
}

func BuildIndex(t *testing.T, cases []LoadableSeries) *TSDBIndex {
	dir := t.TempDir()
	b := index.NewBuilder()

	for _, s := range cases {
		b.AddSeries(s.Labels, s.Chunks)
	}

	require.Nil(t, b.Build(context.Background(), dir))

	reader, err := index.NewFileReader(dir)
	require.Nil(t, err)

	return NewTSDBIndex(reader)
}
