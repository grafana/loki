package bloomcompactor

import (
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

// Job holds a compaction job, which consists of a group of blocks that should be compacted together.
// Not goroutine safe.
// TODO: A job should probably contain series or chunks
type Job struct {
	tenantID  string
	tableName string
	seriesFP  model.Fingerprint
	chunks    []index.ChunkMeta
}

// NewJob returns a new compaction Job.
func NewJob(tenantID string, tableName string, seriesFP model.Fingerprint, chunks []index.ChunkMeta) Job {
	return Job{
		tenantID:  tenantID,
		tableName: tableName,
		seriesFP:  seriesFP,
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
