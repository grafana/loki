package dataobj

import (
	"time"
)

// TimeRange is the time range of a tenant's data in an object.
type TimeRange struct {
	Tenant  string
	MinTime time.Time
	MaxTime time.Time
}
