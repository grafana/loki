package bloomcompactor

import (
	"math"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

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

	// We compute them lazily. Unset value is 0.
	from, through model.Time
	minFp, maxFp  model.Fingerprint
}

// NewJob returns a new compaction Job.
func NewJob(
	tenantID string,
	tableName string,
	indexPath string,
	seriesMetas []seriesMeta,
) Job {
	j := Job{
		tenantID:    tenantID,
		tableName:   tableName,
		indexPath:   indexPath,
		seriesMetas: seriesMetas,
	}
	j.computeBounds()
	return j
}

func (j *Job) String() string {
	return j.tableName + "_" + j.tenantID + "_"
}

func (j *Job) computeBounds() {
	if len(j.seriesMetas) == 0 {
		return
	}

	minFrom := model.Latest
	maxThrough := model.Earliest

	minFp := model.Fingerprint(math.MaxInt64)
	maxFp := model.Fingerprint(0)

	for _, seriesMeta := range j.seriesMetas {
		// calculate timestamp boundaries
		for _, chunkRef := range seriesMeta.chunkRefs {
			from, through := chunkRef.Bounds()
			if minFrom > from {
				minFrom = from
			}
			if maxThrough < through {
				maxThrough = through
			}
		}

		// calculate fingerprint boundaries
		if minFp > seriesMeta.seriesFP {
			minFp = seriesMeta.seriesFP
		}
		if maxFp < seriesMeta.seriesFP {
			maxFp = seriesMeta.seriesFP
		}
	}

	j.from = minFrom
	j.through = maxThrough

	j.minFp = minFp
	j.maxFp = maxFp
}
