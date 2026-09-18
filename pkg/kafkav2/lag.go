package kafkav2

import (
	"context"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
)

var (
	consumptionLag = prometheus.NewDesc(
		"consumption_lag",
		"The difference between the end offset and the last committed offset.",
		[]string{"topic", "partition"}, nil,
	)
)

// LagCollector implements a [prometheus.Collector] that tracks consumption lag
// measured as the difference between the end offset and the last committed offset.
type LagCollector struct {
	topic        string
	partition    int32
	offsetReader *OffsetReader
	logger       log.Logger
}

func NewLagCollector(client *kgo.Client, topic string, partition int32, consumerGroup string, logger log.Logger) *LagCollector {
	return &LagCollector{
		topic:        topic,
		partition:    partition,
		offsetReader: NewOffsetReader(client, topic, consumerGroup, logger),
		logger:       logger,
	}
}

func (c *LagCollector) Collect(ch chan<- prometheus.Metric) {
	// Collect does not currently support [context.Context]. Instead, it is idiomatic
	// to set a short timeout to prevent scrape timeouts. More information can be found
	// in https://github.com/prometheus/client_golang/issues/1538.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The end offset is the offset of the next record to be produced. That means
	// if the end offset is zero no records have been produced for this partition.
	endOffset, err := c.offsetReader.EndOffset(ctx, c.partition)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to get last produced offset", "error", err)
		return
	}

	// If some records have been produced for this partition we need to make sure
	// the consumer has processed and committed all of them otherwise we risk data
	// loss. If no offsets have been committed, the last committed offset is -1.
	lastCommittedOffset, err := c.offsetReader.LastCommittedOffset(ctx, c.partition)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to get last committed offset", "error", err)
		return
	}

	var delta int64
	// If no records have been produced, the delta is zero. If at least one record
	// has been produced, the delta is the difference between the end offset and
	// the last committed offset. We must subtract one because the end offset is
	// the offset of the next record to be produced.
	if endOffset > 0 {
		delta = endOffset - lastCommittedOffset - 1
	}

	ch <- prometheus.MustNewConstMetric(
		consumptionLag,
		prometheus.GaugeValue,
		float64(delta),
		c.topic, strconv.FormatInt(int64(c.partition), 10),
	)
}

func (c *LagCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- consumptionLag
}
