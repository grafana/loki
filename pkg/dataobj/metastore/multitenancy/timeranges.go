package multitenancy

import (
	"time"
)

// TimeRange represents a time range for a specific tenant.
type TimeRange struct {
	Tenant  string
	MinTime time.Time
	MaxTime time.Time
}
