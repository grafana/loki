package querier

import (
	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/runtime"
)

func DefaultLimitsConfig() runtime.Limits {
	limits := runtime.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}
