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

func (j Job) String() string {
	return j.tableName + "_" + j.tenantID + "_" + j.seriesFP.String()
}

func (j Job) Tenant() string {
	return j.tenantID
}

func (j Job) Fingerprint() model.Fingerprint {
	return j.seriesFP
}

func (j Job) Chunks() []index.ChunkMeta {
	return j.chunks
}

func (j Job) Labels() labels.Labels {
	return j.seriesLbs
}

func (j Job) IndexPath() string {
	return j.indexPath
}
