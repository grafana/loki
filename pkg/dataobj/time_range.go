package dataobj

import (
	"time"
)

// TimeRange is the time range of a tenant's data in an object, together with
// the sizes recorded for it in the metastore.
type TimeRange struct {
	Tenant               string
	MinTime              time.Time
	MaxTime              time.Time
	FileSize             uint64
	UncompressedLogsSize uint64
}
