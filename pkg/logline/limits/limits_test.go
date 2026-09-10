package limits_test

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logline/limits"
)

// stubLimits is a test helper that satisfies limits.Limits.
type stubLimits struct {
	retentionPeriod time.Duration
}

func (s stubLimits) RetentionPeriod() time.Duration { return s.retentionPeriod }

// Compile-time assertion: stubLimits satisfies limits.Limits.
var _ limits.Limits = stubLimits{}
