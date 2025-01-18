package consumer

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

type partitionOffsetMetrics struct {
	currentOffset prometheus.GaugeFunc
	latestOffset  prometheus.GaugeFunc
	partition     int32
	topic         string
	client        *kgo.Client
	ctx           context.Context
	lastOffset    int64

	// Error counters
	flushFailures  prometheus.Counter
	commitFailures prometheus.Counter
	appendFailures prometheus.Counter

	// Processing delay histogram
	processingDelay prometheus.Histogram

	logger log.Logger
}

func newPartitionOffsetMetrics(ctx context.Context, client *kgo.Client, topic string, partition int32, logger log.Logger) *partitionOffsetMetrics {
	p := &partitionOffsetMetrics{
		partition: partition,
		topic:     topic,
		client:    client,
		ctx:       ctx,
		flushFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_flush_failures_total",
			Help: "Total number of flush failures",
		}),
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures",
		}),
		appendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_append_failures_total",
			Help: "Total number of append failures",
		}),
		processingDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_processing_delay_seconds",
			Help:                            "Time difference between record timestamp and processing time in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		logger: logger,
	}

	p.currentOffset = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_current_offset",
			Help: "The last consumed offset for this partition",
		},
		p.getCurrentOffset,
	)

	p.latestOffset = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_latest_offset",
			Help: "The latest available offset for this partition",
		},
		p.getLatestOffset,
	)

	return p
}

func (p *partitionOffsetMetrics) getCurrentOffset() float64 {
	return float64(atomic.LoadInt64(&p.lastOffset))
}

func (p *partitionOffsetMetrics) getLatestOffset() float64 {
	adm := kadm.NewClient(p.client)
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()
	resp, err := adm.ListEndOffsets(ctx, p.topic)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to list end offsets", "topic", p.topic, "partition", p.partition, "err", err)
		return 0
	}
	offset, ok := resp.Lookup(p.topic, p.partition)
	if !ok {
		level.Debug(p.logger).Log("msg", "partition not found in response", "topic", p.topic, "partition", p.partition)
		return 0
	}
	return float64(offset.Offset)
}

func (p *partitionOffsetMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.currentOffset,
		p.latestOffset,
		p.flushFailures,
		p.commitFailures,
		p.appendFailures,
		p.processingDelay,
	}

	for _, collector := range collectors {
		if err := reg.Register(collector); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				return err
			}
		}
	}
	return nil
}

func (p *partitionOffsetMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		p.currentOffset,
		p.latestOffset,
		p.flushFailures,
		p.commitFailures,
		p.appendFailures,
		p.processingDelay,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *partitionOffsetMetrics) updateOffset(offset int64) {
	atomic.StoreInt64(&p.lastOffset, offset)
}

func (p *partitionOffsetMetrics) incFlushFailures() {
	p.flushFailures.Inc()
}

func (p *partitionOffsetMetrics) incCommitFailures() {
	p.commitFailures.Inc()
}

func (p *partitionOffsetMetrics) incAppendFailures() {
	p.appendFailures.Inc()
}

func (p *partitionOffsetMetrics) observeProcessingDelay(recordTimestamp time.Time) {
	// Convert milliseconds to seconds and calculate delay
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.processingDelay.Observe(time.Since(recordTimestamp).Seconds())
	}
}
