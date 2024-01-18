package bloomcompactor

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type seriesMeta struct {
	seriesFP  model.Fingerprint
	seriesLbs labels.Labels
	chunkRefs []index.ChunkMeta
}

type Job struct {
	tableName, tenantID, indexPath string
	seriesMetas                    []seriesMeta
	v1.SeriesBounds
}

// NewJob returns a new compaction Job.
func NewJob(
	tenantID string,
	tableName string,
	indexPath string,
	seriesMetas []seriesMeta,
) Job {
	bounds := v1.NewUpdatableBounds()
	for _, series := range seriesMetas {
		bounds.Update(v1.Series{
			Fingerprint: series.seriesFP,
			Chunks:      makeChunkRefsFromChunkMetas(series.chunkRefs),
		})
	}

	return Job{
		tenantID:     tenantID,
		tableName:    tableName,
		indexPath:    indexPath,
		seriesMetas:  seriesMetas,
		SeriesBounds: v1.SeriesBounds(bounds),
	}
}

func (j *Job) String() string {
	return j.tableName + "_" + j.tenantID + "_"
}
