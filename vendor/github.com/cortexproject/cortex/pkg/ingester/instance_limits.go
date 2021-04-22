package ingester

import "github.com/pkg/errors"

var (
	// We don't include values in the message to avoid leaking Cortex cluster configuration to users.
	errMaxSamplesPushRateLimitReached = errors.New("cannot push more samples: ingester's samples push rate limit reached")
	errMaxUsersLimitReached           = errors.New("cannot create TSDB: ingesters's max tenants limit reached")
	errMaxSeriesLimitReached          = errors.New("cannot add series: ingesters's max series limit reached")
	errTooManyInflightPushRequests    = errors.New("cannot push: too many inflight push requests in ingester")
)

// InstanceLimits describes limits used by ingester. Reaching any of these will result in Push method to return
// (internal) error.
type InstanceLimits struct {
	MaxIngestionRate        float64 `yaml:"max_ingestion_rate"`
	MaxInMemoryTenants      int64   `yaml:"max_tenants"`
	MaxInMemorySeries       int64   `yaml:"max_series"`
	MaxInflightPushRequests int64   `yaml:"max_inflight_push_requests"`
}

// Sets default limit values for unmarshalling.
var defaultInstanceLimits *InstanceLimits = nil

// UnmarshalYAML implements the yaml.Unmarshaler interface. If give
func (l *InstanceLimits) UnmarshalYAML(unmarshal func(interface{}) error) error {
	if defaultInstanceLimits != nil {
		*l = *defaultInstanceLimits
	}
	type plain InstanceLimits // type indirection to make sure we don't go into recursive loop
	return unmarshal((*plain)(l))
}
