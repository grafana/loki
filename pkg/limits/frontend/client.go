package frontend

import (
	"github.com/grafana/loki/v3/pkg/logproto"
)

type IngestLimitsClient interface {
	// GetLimits returns the ingestion limits for the tenant.
	GetLimits(tenantID string) (*logproto.IngestionLimits, error)

	// HasStream returns true if the stream is known to the limits service,
	// otherwise false. This helps distributors reject new streams when stream
	// limits are exceeded while still allowing push requests for existing streams.
	HasStream(tenantID string, streamHash uint64) (bool, error)

	// HasStreams returns a slice of booleans for each stream and if it is known
	// to the limits service.
	HasStreams(tenantId string, streamHashes []uint64) ([]bool, error)
}

type GRPCIngestLimitsClient struct {
	cfg     Config
	metrics *IngestLimitsClientMetrics
}

func NewGRPCIngestLimitsClient(cfg Config, metrics *IngestLimitsClientMetrics) *GRPCIngestLimitsClient {
	return &GRPCIngestLimitsClient{
		cfg:     cfg,
		metrics: metrics,
	}
}

func (c *GRPCIngestLimitsClient) GetLimits(tenantID string) (*logproto.IngestionLimits, error) {
	return nil, nil
}

func (c *GRPCIngestLimitsClient) HasStream(tenantID string, streamHash uint64) (bool, error) {
	return false, nil
}

func (c *GRPCIngestLimitsClient) HasStreams(tenantID string, streamHashes []uint64) ([]bool, error) {
	b := make([]bool, 0, len(streamHashes))
	return b, nil
}
