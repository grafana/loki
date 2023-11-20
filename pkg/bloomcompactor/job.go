package bloomcompactor

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type Job struct {
	tableName, tenantID, indexPath string
	seriesLbs                      labels.Labels
	seriesFP                       model.Fingerprint
	chunks                         []index.ChunkMeta

	// We compute them lazily. Unset value is 0.
	from, through model.Time
}

// NewJob returns a new compaction Job.
func NewJob(
	tenantID string,
	tableName string,
	indexPath string,
	seriesFP model.Fingerprint,
	seriesLbs labels.Labels,
	chunks []index.ChunkMeta,
) Job {
	return Job{
		tenantID:  tenantID,
		tableName: tableName,
		indexPath: indexPath,
		seriesFP:  seriesFP,
		seriesLbs: seriesLbs,
		chunks:    chunks,
	}
}

func (j *Job) String() string {
	return j.tableName + "_" + j.tenantID + "_" + j.seriesFP.String()
}

func (j *Job) TableName() string {
	return j.tableName
}

func (j *Job) Tenant() string {
	return j.tenantID
}

func (j *Job) Fingerprint() model.Fingerprint {
	return j.seriesFP
}

func (j *Job) Chunks() []index.ChunkMeta {
	return j.chunks
}

func (j *Job) Labels() labels.Labels {
	return j.seriesLbs
}

func (j *Job) IndexPath() string {
	return j.indexPath
}

func (j *Job) From() model.Time {
	if j.from == 0 {
		j.computeFromThrough()
	}
	return j.from
}

func (j *Job) Through() model.Time {
	if j.through == 0 {
		j.computeFromThrough()
	}
	return j.through
}

func (j *Job) computeFromThrough() {
	if len(j.chunks) == 0 {
		return
	}

	minFrom := model.Latest
	maxThrough := model.Earliest

	for _, chunk := range j.chunks {
		from, through := chunk.Bounds()
		if minFrom > from {
			minFrom = from
		}
		if maxThrough < through {
			maxThrough = through
		}
	}

	j.from = minFrom
	j.through = maxThrough
}
