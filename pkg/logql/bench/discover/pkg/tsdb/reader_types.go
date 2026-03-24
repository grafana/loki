package tsdb

import (
	"github.com/prometheus/common/model"

	lokitsdb "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type IndexReaderResult struct {
	LocalPath  string
	Bounds     [2]model.Time
	Version    int
	LabelNames []string
	// Index is the opened TSDB index handle for structural discovery (ForSeries
	// traversal). The caller is responsible for closing it when no longer needed.
	Index lokitsdb.Index
}
