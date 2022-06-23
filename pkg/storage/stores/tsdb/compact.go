package tsdb

import (
	"context"
	"fmt"

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

func (c *Compactor) Compact(ctx context.Context, indices ...*TSDBIndex) (res Identifier, err error) {
	// No need to compact a single index file
	if len(indices) == 1 {
		return newPrefixedIdentifier(
				indices[0].Identifier(c.tenant),
				c.parentDir,
				c.parentDir,
			),
			nil
	}

	ifcs := make([]Index, 0, len(indices))
	for _, idx := range indices {
		ifcs = append(ifcs, idx)
	}

	b := NewBuilder()
	multi, err := NewMultiIndex(ifcs...)
	if err != nil {
		return res, err
	}

	// TODO(owen-d): introduce parallelism
	// Until then,
	// Instead of using the MultiIndex.forIndices helper, we loop over each sub-index manually.
	// The index builder is single threaded, so we avoid races.
	// Additionally, this increases the likelihood we add chunks in order
	// by processing the indices in ascending order.
	for _, idx := range multi.indices {
		casted, ok := idx.(*TSDBIndex)
		if !ok {
			return nil, fmt.Errorf("expected tsdb index to compact, found :%T", idx)
		}
		if err := casted.forSeries(
			ctx,
			nil,
			func(ls labels.Labels, _ model.Fingerprint, chks []index.ChunkMeta) {
				// AddSeries copies chks into it's own slice
				b.AddSeries(ls.Copy(), model.Fingerprint(ls.Hash()), chks)
			},
			labels.MustNewMatcher(labels.MatchEqual, "", ""),
		); err != nil {
			return res, err
		}
	}

	return b.Build(
		ctx,
		c.parentDir,
		func(from, through model.Time, checksum uint32) Identifier {
			id := SingleTenantTSDBIdentifier{
				Tenant:   c.tenant,
				From:     from,
				Through:  through,
				Checksum: checksum,
			}
			return newPrefixedIdentifier(id, c.parentDir, c.parentDir)
		},
	)
}
