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
	b := NewBuilder()

	for _, s := range cases {
		b.AddSeries(s.Labels, model.Fingerprint(s.Labels.Hash()), s.Chunks)
	}

	dst, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		id := SingleTenantTSDBIdentifier{
			Tenant:   tenant,
			From:     from,
			Through:  through,
			Checksum: checksum,
		}
		return newPrefixedIdentifier(id, dir, dir)
	})
	require.Nil(t, err)

	idx, err := NewShippableTSDBFile(dst, false)
	require.Nil(t, err)
	return idx
}
