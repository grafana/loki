package consumer

import (
	"context"
	"strconv"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
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
}

func newPartitionOffsetMetrics(ctx context.Context, client *kgo.Client, topic string, partition int32) *partitionOffsetMetrics {
	labels := prometheus.Labels{
		"partition": strconv.Itoa(int(partition)),
		"topic":     topic,
	}

	p := &partitionOffsetMetrics{
		partition: partition,
		topic:     topic,
		client:    client,
		ctx:       ctx,
		flushFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "loki_dataobj_consumer_flush_failures_total",
			Help:        "Total number of flush failures",
			ConstLabels: labels,
		}),
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "loki_dataobj_consumer_commit_failures_total",
			Help:        "Total number of commit failures",
			ConstLabels: labels,
		}),
		appendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name:        "loki_dataobj_consumer_append_failures_total",
			Help:        "Total number of append failures",
			ConstLabels: labels,
		}),
	}

	p.currentOffset = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "loki_dataobj_consumer_current_offset",
			Help:        "The last consumed offset for this partition",
			ConstLabels: labels,
		},
		p.getCurrentOffset,
	)

	p.latestOffset = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name:        "loki_dataobj_consumer_latest_offset",
			Help:        "The latest available offset for this partition",
			ConstLabels: labels,
		},
		p.getLatestOffset,
	)

	return p
}

func (p *partitionOffsetMetrics) getCurrentOffset() float64 {
	return float64(atomic.LoadInt64(&p.lastOffset))
}

func (p *partitionOffsetMetrics) getLatestOffset() float64 {
	req := kmsg.ListOffsetsRequest{
		Topics: []kmsg.ListOffsetsRequestTopic{
			{
				Topic: p.topic,
				Partitions: []kmsg.ListOffsetsRequestTopicPartition{
					{
						Partition: p.partition,
						Timestamp: -1, // Latest offset
					},
				},
			},
		},
	}
	resp, err := p.client.Request(p.ctx, &req)
	if err != nil {
		return 0
	}
	if listResp, ok := resp.(*kmsg.ListOffsetsResponse); ok && len(listResp.Topics) > 0 && len(listResp.Topics[0].Partitions) > 0 {
		return float64(listResp.Topics[0].Partitions[0].Offset)
	}
	return 0
}

func (p *partitionOffsetMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.currentOffset,
		p.latestOffset,
		p.flushFailures,
		p.commitFailures,
		p.appendFailures,
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
