package querier

import (
	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/pkg/validation"
)

func DefaultLimitsConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}
