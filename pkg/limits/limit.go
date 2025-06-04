package limits

import (
	"context"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// Limits contains all limits enforced by the limits frontend.
type Limits interface {
	IngestionRateBytes(userID string) float64
	IngestionBurstSizeBytes(userID string) int
	MaxGlobalStreamsPerUser(userID string) int
}

type limitsChecker struct {
	limits           Limits
	store            *usageStore
	producer         *producer
	partitionManager *partitionManager
	numPartitions    int
	logger           log.Logger

	// Metrics.
	tenantIngestedBytesTotal       *prometheus.CounterVec
	streamInvalidPartitionAssigned *prometheus.CounterVec

	// Used in tests.
	clock quartz.Clock
}

func newLimitsChecker(limits Limits, store *usageStore, producer *producer, partitionManager *partitionManager, numPartitions int, logger log.Logger, reg prometheus.Registerer) *limitsChecker {
	return &limitsChecker{
		limits:           limits,
		store:            store,
		producer:         producer,
		partitionManager: partitionManager,
		numPartitions:    numPartitions,
		logger:           logger,
		tenantIngestedBytesTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingest_limits_tenant_ingested_bytes_total",
			Help: "Total number of bytes ingested per tenant within the active window. This is not a global total, as tenants can be sharded over multiple pods.",
		}, []string{"tenant"}),
		streamInvalidPartitionAssigned: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingest_limits_stream_invalid_partition_assigned_total",
			Help: "Total number of times a stream was assigned to a partition that is not owned by the instance.",
		}, []string{"tenant"}),
		clock: quartz.NewReal(),
	}
}

func (c *limitsChecker) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	streams := req.Streams
	valid := 0
	for _, stream := range streams {
		partition := int32(stream.StreamHash % uint64(c.numPartitions))

		// TODO(periklis): Do we need to report this as an error to the frontend?
		if assigned := c.partitionManager.Has(partition); !assigned {
			c.streamInvalidPartitionAssigned.WithLabelValues(req.Tenant).Inc()
			continue
		}

		streams[valid] = stream
		valid++
	}
	streams = streams[:valid]

	accepted, rejected, err := c.store.UpdateCond(req.Tenant, streams, c.clock.Now(), c.limits)
	if err != nil {
		return nil, err
	}

	var ingestedBytes uint64
	for _, stream := range accepted {
		ingestedBytes += stream.TotalSize

		err := c.producer.Produce(context.WithoutCancel(ctx), req.Tenant, stream)
		if err != nil {
			level.Error(c.logger).Log("msg", "failed to send streams", "error", err)
		}
	}

	c.tenantIngestedBytesTotal.WithLabelValues(req.Tenant).Add(float64(ingestedBytes))

	results := make([]*proto.ExceedsLimitsResult, 0, len(rejected))
	for _, stream := range rejected {
		results = append(results, &proto.ExceedsLimitsResult{
			StreamHash: stream.StreamHash,
			Reason:     uint32(ReasonExceedsMaxStreams),
		})
	}

	return &proto.ExceedsLimitsResponse{Results: results}, nil
}
