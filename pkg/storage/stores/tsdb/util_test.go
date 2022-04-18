package tsdb

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type LoadableSeries struct {
	Labels labels.Labels
	Chunks index.ChunkMetas
}

func BuildIndex(t *testing.T, dir, tenant string, cases []LoadableSeries) *TSDBFile {
	b := index.NewBuilder()

	for _, s := range cases {
		b.AddSeries(s.Labels, s.Chunks)
	}

	dst, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) (index.Identifier, string) {
		id := index.Identifier{
			Tenant:   tenant,
			From:     from,
			Through:  through,
			Checksum: checksum,
		}
		return id, id.FilePath(dir)
	})
	require.Nil(t, err)
	location := dst.FilePath(dir)

	idx, err := NewShippableTSDBFile(location)
	require.Nil(t, err)
	return idx
}
