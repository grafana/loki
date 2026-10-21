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
	return BuildShardedIndex(t, dir, cases, index.ShardAnnotation{})
}

// BuildShardedIndex builds the file of one fingerprint-range shard of a table
// the compactor split: only the series of cases that shard matches are
// written, and the file is named for the shard. The zero shard builds an
// unsplit file of every series.
func BuildShardedIndex(t testing.TB, dir string, cases []LoadableSeries, shard index.ShardAnnotation) *TSDBFile {
	b := NewBuilder(index.FormatV3)

	for _, s := range cases {
		fp := model.Fingerprint(labels.StableHash(s.Labels))
		if shard.Of > 1 && !shard.Match(fp) {
			continue
		}
		b.AddSeries(s.Labels, fp, s.Chunks)
	}

	dst, err := b.Build(context.Background(), dir, func(from, through model.Time, checksum uint32) Identifier {
		id := SingleTenantTSDBIdentifier{
			TS:       time.Now(),
			From:     from,
			Through:  through,
			Checksum: checksum,
			Shard:    shard,
		}
		return NewPrefixedIdentifier(id, dir, dir)
	})
	require.Nil(t, err)

	idx, err := NewShippableTSDBFile(dst, index.MmapOptions{})
	require.Nil(t, err)
	return idx
}
