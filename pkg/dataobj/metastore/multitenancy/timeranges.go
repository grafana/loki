package multitenancy

import (
	"time"
)

// TimeRange represents a time range for a specific tenant.
type TimeRange struct {
	Tenant  TenantID
	MinTime time.Time
	MaxTime time.Time
}

// TenantID wraps a singular tenant ID string.
type TenantID string
