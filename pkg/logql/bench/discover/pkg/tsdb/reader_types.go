package tsdb

import "github.com/prometheus/common/model"

type IndexReaderResult struct {
	LocalPath  string
	Bounds     [2]model.Time
	Version    int
	LabelNames []string
}
