package tsdb

import (
	"time"

	"github.com/prometheus/common/model"
)

type DownloadConfig struct {
	Tenant      string
	From        time.Time
	To          time.Time
	TmpDir      string
	TablePrefix string // Overrides default "index_" prefix for table name generation (e.g. "loki_dev_005_tsdb_index_").
}

type DownloadedIndexFile struct {
	Table      string
	ObjectName string
	LocalPath  string
	From       model.Time
	Through    model.Time
}

type DownloadResult struct {
	Files []DownloadedIndexFile
}
