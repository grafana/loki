package partition

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kadm"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// Committer defines an interface for committing offsets
type Committer interface {
	Commit(ctx context.Context, offset int64) error
}

// partitionCommitter is responsible for committing offsets for a specific Kafka partition
// to the Kafka broker. It also tracks metrics related to the commit process.
type partitionCommitter struct {
	commitRequestsTotal   prometheus.Counter
	commitRequestsLatency prometheus.Histogram
	commitFailuresTotal   prometheus.Counter
	lastCommittedOffset   prometheus.Gauge

	logger    log.Logger
	admClient *kadm.Client

	kafkaCfg      kafka.Config
	partitionID   int32
	consumerGroup string

	toCommit *atomic.Int64
	wg       sync.WaitGroup
	cancel   context.CancelFunc
}

// newCommitter creates and initializes a new partitionCommitter.
// It sets up the necessary metrics and initializes the committer with the provided configuration.
func newCommitter(kafkaCfg kafka.Config, admClient *kadm.Client, partitionID int32, consumerGroup string, logger log.Logger, reg prometheus.Registerer) *partitionCommitter {
	c := &partitionCommitter{
		logger:        logger,
		kafkaCfg:      kafkaCfg,
		partitionID:   partitionID,
		consumerGroup: consumerGroup,
		admClient:     admClient,
		commitRequestsTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        "loki_ingest_storage_reader_offset_commit_requests_total",
			Help:        "Total number of requests issued to commit the last consumed offset (includes both successful and failed requests).",
			ConstLabels: prometheus.Labels{"partition": strconv.Itoa(int(partitionID))},
		}),
		commitFailuresTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name:        "loki_ingest_storage_reader_offset_commit_failures_total",
			Help:        "Total number of failed requests to commit the last consumed offset.",
			ConstLabels: prometheus.Labels{"partition": strconv.Itoa(int(partitionID))},
		}),
		commitRequestsLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_ingest_storage_reader_offset_commit_request_duration_seconds",
			Help:                            "The duration of requests to commit the last consumed offset.",
			ConstLabels:                     prometheus.Labels{"partition": strconv.Itoa(int(partitionID))},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
			Buckets:                         prometheus.DefBuckets,
		}),
		lastCommittedOffset: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name:        "loki_ingest_storage_reader_last_committed_offset",
			Help:        "The last consumed offset successfully committed by the partition reader. Set to -1 if not offset has been committed yet.",
			ConstLabels: prometheus.Labels{"partition": strconv.Itoa(int(partitionID))},
		}),
		toCommit: atomic.NewInt64(-1),
	}

	// Initialise the last committed offset metric to -1 to signal no offset has been committed yet (0 is a valid offset).
	c.lastCommittedOffset.Set(-1)

	if kafkaCfg.ConsumerGroupOffsetCommitInterval > 0 {
		c.wg.Add(1)
		ctx, cancel := context.WithCancel(context.Background())
		c.cancel = cancel
		go c.autoCommitLoop(ctx)
	}

	return c
}

func (r *partitionCommitter) autoCommitLoop(ctx context.Context) {
	defer r.wg.Done()
	commitTicker := time.NewTicker(r.kafkaCfg.ConsumerGroupOffsetCommitInterval)
	defer commitTicker.Stop()

	previousOffset := r.toCommit.Load()
	for {
		select {
		case <-ctx.Done():
			return
		case <-commitTicker.C:
			currOffset := r.toCommit.Load()
			if currOffset == previousOffset {
				continue
			}

			if err := r.Commit(ctx, currOffset); err == nil {
				previousOffset = currOffset
			}
		}
	}
}

func (r *partitionCommitter) enqueueOffset(o int64) {
	if r.kafkaCfg.ConsumerGroupOffsetCommitInterval > 0 {
		r.toCommit.Store(o)
	}
}

// commit attempts to commit the given offset to Kafka for the partition this committer is responsible for.
// It updates relevant metrics and logs the result of the commit operation.
func (r *partitionCommitter) Commit(ctx context.Context, offset int64) (returnErr error) {
	startTime := time.Now()
	r.commitRequestsTotal.Inc()

	defer func() {
		r.commitRequestsLatency.Observe(time.Since(startTime).Seconds())

		if returnErr != nil {
			level.Error(r.logger).Log("msg", "failed to commit last consumed offset to Kafka", "err", returnErr, "offset", offset)
			r.commitFailuresTotal.Inc()
		}
	}()

	// Commit the last consumed offset.
	toCommit := kadm.Offsets{}
	toCommit.AddOffset(r.kafkaCfg.Topic, r.partitionID, offset, -1)
	committed, err := r.admClient.CommitOffsets(ctx, r.consumerGroup, toCommit)
	if err != nil {
		return err
	} else if !committed.Ok() {
		return committed.Error()
	}

	committedOffset, _ := committed.Lookup(r.kafkaCfg.Topic, r.partitionID)
	level.Debug(r.logger).Log("msg", "last commit offset successfully committed to Kafka", "offset", committedOffset.At)
	r.lastCommittedOffset.Set(float64(committedOffset.At))
	return nil
}

func (r *partitionCommitter) Stop() {
	if r.kafkaCfg.ConsumerGroupOffsetCommitInterval <= 0 {
		return
	}
	r.cancel()
	r.wg.Wait()

	offset := r.toCommit.Load()
	if offset < 0 {
		return
	}
	// Commit has internal timeouts, so this call shouldn't block for too long.
	_ = r.Commit(context.Background(), offset)
}
