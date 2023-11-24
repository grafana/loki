package bloomcompactor

import (
	"math"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type Job struct {
	tableName, tenantID, indexPath string
	indices                        []Index

	// We compute them lazily. Unset value is 0.
	from, through model.Time
	minFp, maxFp  model.Fingerprint
}

type Index struct {
	seriesFP  model.Fingerprint
	seriesLbs labels.Labels
	chunks    []index.ChunkMeta //TODO rename to chunkRefs
}

// NewJob returns a new compaction Job.
func NewJob(
	tenantID string,
	tableName string,
	indexPath string,
	indices []Index,
) Job {
	return Job{
		tenantID:  tenantID,
		tableName: tableName,
		indexPath: indexPath,
		indices:   indices,
	}
}

func (j *Job) String() string {
	return j.tableName + "_" + j.tenantID + "_"
}

func (j *Job) TableName() string {
	return j.tableName
}

func (j *Job) Tenant() string {
	return j.tenantID
}

func (i *Index) Fingerprint() model.Fingerprint {
	return i.seriesFP
}

func (i *Index) Chunks() []index.ChunkMeta {
	return i.chunks
}

func (i *Index) Labels() labels.Labels {
	return i.seriesLbs
}

func (j *Job) IndexPath() string {
	return j.indexPath
}

func (j *Job) From() model.Time {
	if j.from == 0 {
		j.computeBounds()
	}
	return j.from
}

func (j *Job) Through() model.Time {
	if j.through == 0 {
		j.computeBounds()
	}
	return j.through
}

func (j *Job) computeBounds() {
	if len(j.indices) == 0 {
		return
	}

	minFrom := model.Latest
	maxThrough := model.Earliest

	minFp := model.Fingerprint(math.MaxInt64)
	maxFp := model.Fingerprint(0)

	for _, index := range j.indices {
		// calculate timestamp boundaries
		for _, chunk := range index.chunks {
			from, through := chunk.Bounds()
			if minFrom > from {
				minFrom = from
			}
			if maxThrough < through {
				maxThrough = through
			}
		}

		// calculate fingerprint boundaries
		if minFp > index.seriesFP {
			minFp = index.seriesFP
		}
		if maxFp < index.seriesFP {
			maxFp = index.seriesFP
		}
	}

	j.from = minFrom
	j.through = maxThrough

	j.minFp = minFp
	j.maxFp = maxFp
}
