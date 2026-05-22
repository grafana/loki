package distributor

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka"
)

const (
	inmemoryTeeDefaultTopic            = "loki"
	inmemoryTeeDefaultMaxBufferedBytes = 100 << 20 // 100 MB
)

// InMemoryDataObjTee implements the [Tee] interface and sends encoded log
// records to an in-process channel instead of to Kafka. It is used in
// single-binary mode with ingest_mode=inmemory.
type InMemoryDataObjTee struct {
	records          chan *kgo.Record
	topic            string
	maxBufferedBytes int
	pushTimeout      time.Duration
	logger           log.Logger

	streams        prometheus.Counter
	streamFailures *prometheus.CounterVec
}

// NewInMemoryDataObjTee returns a new InMemoryDataObjTee that sends encoded
// records to the given channel. reg and logger may be nil.
func NewInMemoryDataObjTee(records chan *kgo.Record, reg prometheus.Registerer, logger log.Logger, pushTimeout time.Duration) *InMemoryDataObjTee {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &InMemoryDataObjTee{
		records:          records,
		topic:            inmemoryTeeDefaultTopic,
		maxBufferedBytes: inmemoryTeeDefaultMaxBufferedBytes,
		pushTimeout:      pushTimeout,
		logger:           logger,
		streams: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_distributor_inmemory_dataobj_tee_streams_total",
			Help: "Total number of streams duplicated (both successful and failed) to the in-memory dataobj channel.",
		}),
		streamFailures: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_distributor_inmemory_dataobj_tee_stream_failures_total",
			Help: "Total number of streams that could not be duplicated to the in-memory dataobj channel.",
		}, []string{"reason"}),
	}
}

// Register implements [Tee]. It adds all streams to the pushTracker's pending count
// so the distributor waits for them before concluding a push request.
func (t *InMemoryDataObjTee) Register(_ context.Context, _ string, streams []KeyedStream, pushTracker *PushTracker) {
	pushTracker.streamsPending.Add(int32(len(streams)))
}

// Duplicate implements [Tee]. It encodes each stream and sends it to the
// in-process channel, calling pushTracker.doneWithResult for each stream.
func (t *InMemoryDataObjTee) Duplicate(ctx context.Context, tenant string, streams []KeyedStream, pushTracker *PushTracker) {
	go func() {
		for _, s := range streams {
			t.duplicate(ctx, tenant, s, pushTracker)
		}
	}()
}

func (t *InMemoryDataObjTee) duplicate(ctx context.Context, tenant string, stream KeyedStream, pushTracker *PushTracker) {
	t.streams.Inc()

	records, err := kafka.EncodeWithTopic(t.topic, 0, tenant, stream.Stream, t.maxBufferedBytes)
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to encode stream for in-memory tee", "err", err)
		t.streamFailures.WithLabelValues("encode_error").Inc()
		pushTracker.doneWithResult(fmt.Errorf("couldn't process request internally due to inmemory tee error: %d", TeeCouldntEncodeStreamError))
		return
	}

	// Single timer for the whole stream batch. Using time.NewTimer + defer Stop
	// avoids the leak caused by time.After inside a loop (each call creates a timer
	// that lives until it fires, even if the select chose a different case).
	// A nil channel blocks forever, so timeout stays nil when pushTimeout == 0.
	//
	// Note: if the channel send times out mid-batch, earlier records from this
	// stream are already queued. The consumer will process them as a partial
	// stream. This is acceptable in inmemory mode (no durability guarantees).
	var timeout <-chan time.Time
	if t.pushTimeout > 0 {
		timer := time.NewTimer(t.pushTimeout)
		defer timer.Stop()
		timeout = timer.C
	}

	for _, rec := range records {
		select {
		case t.records <- rec:
		case <-timeout:
			level.Error(t.logger).Log("msg", "in-memory dataobj tee channel full, dropping record", "tenant", tenant)
			t.streamFailures.WithLabelValues("channel_full").Inc()
			pushTracker.doneWithResult(fmt.Errorf("couldn't process request internally due to inmemory tee error: %d", TeeCouldntProduceRecordsError))
			return
		case <-ctx.Done():
			t.streamFailures.WithLabelValues("cancellation").Inc()
			pushTracker.doneWithResult(ctx.Err())
			return
		}
	}

	pushTracker.doneWithResult(nil)
}
