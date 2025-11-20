package push

import (
	"context"
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

// TenantsRetention interface provides retention period for tenants.
type TenantsRetention interface {
	RetentionPeriodFor(userID string, lbs labels.Labels) time.Duration
}

// Limits interface provides OTLP configuration and service name discovery.
type Limits interface {
	OTLPConfig(userID string) OTLPConfig
	DiscoverServiceName(userID string) []string
}

// EmptyLimits is an empty implementation of Limits.
type EmptyLimits struct{}

func (EmptyLimits) OTLPConfig(string) OTLPConfig {
	return DefaultOTLPConfig(GlobalOTLPConfig{})
}

func (EmptyLimits) DiscoverServiceName(string) []string {
	return nil
}

func (EmptyLimits) PolicyFor(_ string, _ labels.Labels) string {
	return ""
}

// StreamResolver is a request-scoped interface that provides retention period and policy for a given stream.
// The values returned by the resolver will not change throughout the handling of the request.
type StreamResolver interface {
	RetentionPeriodFor(lbs labels.Labels) time.Duration
	RetentionHoursFor(lbs labels.Labels) string
	PolicyFor(ctx context.Context, lbs labels.Labels) string
}

// UsageTracker interface for tracking usage metrics.
type UsageTracker interface {
	// ReceivedBytesAdd records ingested bytes by tenant, retention period and labels.
	ReceivedBytesAdd(ctx context.Context, tenant string, retentionPeriod time.Duration, labels labels.Labels, value float64, format string)

	// DiscardedBytesAdd records discarded bytes by tenant and labels.
	DiscardedBytesAdd(ctx context.Context, tenant, reason string, labels labels.Labels, value float64, format string)
}

// PolicyWithRetentionWithBytes maps policy names to retention periods to byte counts.
type PolicyWithRetentionWithBytes map[string]map[time.Duration]int64

// Stats holds statistics about a push request.
// Note: ResourceAndSourceMetadataLabels references push.LabelsAdapter from github.com/grafana/loki/pkg/push
type Stats struct {
	Errs           []error
	PolicyNumLines map[string]int64

	// LogLinesBytes holds the total size of all log lines, per policy per retention. Used in billing.
	LogLinesBytes PolicyWithRetentionWithBytes

	// StructuredMetadataBytes holds the size of the original structured metadata (but after it was enriched by OTLP
	// parser) per policy per retention. Used in billing.
	StructuredMetadataBytes PolicyWithRetentionWithBytes

	// ResourceAndSourceMetadataLabels holds structured metadata that was added by OTLP parser (scope and resource attributes)
	// This uses push.LabelsAdapter from github.com/grafana/loki/pkg/push
	ResourceAndSourceMetadataLabels map[string]map[time.Duration]interface{} // Using interface{} to avoid dependency on push package

	// StreamLabelsSize holds the total size of stream labels after sanitization (empty labels removed and
	// non-meaningful whitespaces removed). Not used in billing.
	StreamLabelsSize int64

	MostRecentEntryTimestamp          time.Time
	MostRecentEntryTimestampPerStream map[string]time.Time

	// StreamSizeBytes holds the total size of log lines and structured metadata. Is used only when logPushRequestStreams is true.
	StreamSizeBytes map[string]int64

	HashOfAllStreams uint64
	ContentType      string
	ContentEncoding  string

	BodySize int64
	// Extra is a place for a wrapped parser to record any interesting stats as key-value pairs to be logged
	Extra []any

	HasInternalStreams bool // True if any of the streams has aggregated metrics or is a pattern stream
}

// NewPushStats creates a new Stats instance with initialized maps.
func NewPushStats() *Stats {
	return &Stats{
		LogLinesBytes:                     map[string]map[time.Duration]int64{},
		StructuredMetadataBytes:           map[string]map[time.Duration]int64{},
		PolicyNumLines:                    map[string]int64{},
		ResourceAndSourceMetadataLabels:   map[string]map[time.Duration]interface{}{},
		MostRecentEntryTimestampPerStream: map[string]time.Time{},
		StreamSizeBytes:                   map[string]int64{},
	}
}
