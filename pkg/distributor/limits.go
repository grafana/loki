package distributor

import (
	"time"

	"github.com/grafana/loki/pkg/compactor/retention"
	"github.com/grafana/loki/pkg/distributor/shardstreams"
	"github.com/grafana/loki/pkg/loghttp/push"
)

// Limits is an interface for distributor limits/related configs
type Limits interface {
	retention.Limits
	MaxLineSize(userID string) int
	MaxLineSizeTruncate(userID string) bool
	MaxLabelNamesPerSeries(userID string) int
	MaxLabelNameLength(userID string) int
	MaxLabelValueLength(userID string) int

	CreationGracePeriod(userID string) time.Duration
	RejectOldSamples(userID string) bool
	RejectOldSamplesMaxAge(userID string) time.Duration

	IncrementDuplicateTimestamps(userID string) bool
	DiscoverServiceName(userID string) []string

	ShardStreams(userID string) *shardstreams.Config
	IngestionRateStrategy() string
	IngestionRateBytes(userID string) float64
	IngestionBurstSizeBytes(userID string) int
	AllowStructuredMetadata(userID string) bool
	MaxStructuredMetadataSize(userID string) int
	MaxStructuredMetadataCount(userID string) int
	OTLPConfig(userID string) push.OTLPConfig
}
