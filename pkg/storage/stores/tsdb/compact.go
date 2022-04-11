package tsdb

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type Compactor struct {
	// metrics
	// file names (tenant, bounds, checksum?)
	tenant    string
	parentDir string
}

func NewCompactor(tenant, parentDir string) *Compactor {
	return &Compactor{
		tenant:    tenant,
		parentDir: parentDir,
	}
}

func (c *Compactor) Compact(ctx context.Context, indices ...*TSDBIndex) (res index.Identifier, err error) {
	// No need to compact a single index file
	if len(indices) == 1 {
		return indices[0].Identifier(c.tenant), nil
	}

	b := index.NewBuilder()
	multi, err := NewMultiIndex(indices...)
	if err != nil {
		return res, err
	}

	// Instead of using the MultiIndex.forIndices helper, we loop over each sub-index manually.
	// The index builder is single threaded, so we avoid races.
	// Additionally, this increases the likelihood we add chunks in order
	// by processing the indices in ascending order.
	for _, idx := range multi.indices {
		if err := idx.forSeries(
			nil,
			func(ls labels.Labels, _ model.Fingerprint, chks []index.ChunkMeta) {
				// AddSeries copies chks into it's own slice
				b.AddSeries(ls.Copy(), chks)
			},
			labels.MustNewMatcher(labels.MatchEqual, "", ""),
		); err != nil {
			return res, err
		}
	}

	return b.Build(ctx, c.parentDir, c.tenant)
}
