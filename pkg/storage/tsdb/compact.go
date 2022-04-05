package tsdb

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"

	"github.com/grafana/loki/pkg/storage/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type Compactor struct {
	// metrics
	// file names (tenant, bounds, checksum?)
	tenant string
}

func NewCompactor(tenant string) *Compactor {
	return &Compactor{
		tenant: tenant,
	}
}

func (c *Compactor) Compact(ctx context.Context, indices ...*TSDBIndex) (res Identifier, err error) {
	// No need to compact a single index file
	if len(indices) == 1 {
		return indices[0].Identifier(c.tenant), nil
	}

	b := index.NewBuilder()
	multi, err := NewMultiIndex(indices...)
	if err != nil {
		return res, err
	}

	lower, upper := inclusiveBounds(multi)

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

	// First write tenant/index-bounds-random.staging
	rng := rand.Int63()
	name := fmt.Sprintf("%s-%d-%d-%x.staging", index.IndexFilename, lower, upper, rng)
	tmpPath := filepath.Join(c.tenant, name)

	if err := b.Build(ctx, tmpPath); err != nil {
		return res, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(tmpPath)
		}
	}()

	// checksum and copy to
	reader, err := index.NewFileReader(tmpPath)
	if err != nil {
		return res, err
	}

	// load the newly compacted index to grab checksum, promptly close
	idx := NewTSDBIndex(reader)
	checksum := idx.Checksum()
	idx.Close()

	dst := Identifier{
		Tenant:   c.tenant,
		From:     lower,
		Through:  upper,
		Checksum: checksum,
	}

	if err := os.Rename(tmpPath, dst.String()); err != nil {
		return res, err
	}

	return dst, nil
}
