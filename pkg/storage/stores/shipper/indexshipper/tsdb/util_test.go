package tsdb

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type LoadableSeries struct {
	Labels labels.Labels
	Chunks index.ChunkMetas
}

func BuildIndex(t testing.TB, dir string, cases []LoadableSeries) *TSDBFile {
	b := NewBuilder(index.FormatV3)

	for _, s := range cases {
		b.AddSeries(s.Labels, model.Fingerprint(s.Labels.Hash()), s.Chunks)
	}

	dst, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		id := SingleTenantTSDBIdentifier{
			TS:       time.Now(),
			From:     from,
			Through:  through,
			Checksum: checksum,
		}
		return NewPrefixedIdentifier(id, dir, dir)
	})
	require.Nil(t, err)

	idx, err := NewShippableTSDBFile(dst)
	require.Nil(t, err)
	return idx
}
