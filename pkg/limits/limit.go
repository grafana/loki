package limits

import (
	"context"
	"strconv"

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
	tenantIngestedBytesTotal *prometheus.CounterVec
	streamDiscardedTotal     *prometheus.CounterVec

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
		streamDiscardedTotal: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_ingest_limits_streams_discarded_total",
			Help: "Total number of times streams were discarded.",
		}, []string{"partition"}),
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
			c.streamDiscardedTotal.WithLabelValues(strconv.Itoa(int(partition))).Inc()
			continue
		}

		streams[valid] = stream
		valid++
	}
	streams = streams[:valid]

	toProduce, accepted, rejected, err := c.store.UpdateCond(req.Tenant, streams, c.clock.Now(), c.limits)
	if err != nil {
		return nil, err
	}

	for _, stream := range toProduce {
		err := c.producer.Produce(context.WithoutCancel(ctx), req.Tenant, stream)
		if err != nil {
			level.Error(c.logger).Log("msg", "failed to send streams", "error", err)
		}
	}

	var ingestedBytes uint64
	for _, stream := range accepted {
		ingestedBytes += stream.TotalSize
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
