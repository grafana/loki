package ingester

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/wal"
)

type ObjectStorage interface {
	PutObject(ctx context.Context, objectKey string, object io.Reader) error
}

type MetadataStore interface {
	AddBlock(ctx context.Context, in *metastorepb.AddBlockRequest, opts ...grpc.CallOption) (*metastorepb.AddBlockResponse, error)
}

type consumer struct {
	logger          log.Logger
	metastoreClient MetadataStore
	storage         ObjectStorage
	writer          *wal.SegmentWriter
	buf             *bytes.Buffer
	decoder         *kafka.Decoder
	toStore         []*logproto.Entry

	metrics *consumerMetrics
}

func newConsumer(
	metastoreClient MetadataStore,
	storage ObjectStorage,
	logger log.Logger,
	reg prometheus.Registerer,
) (*consumer, error) {
	writer, err := wal.NewWalSegmentWriter()
	if err != nil {
		return nil, err
	}
	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, err
	}
	return &consumer{
		logger:          logger,
		metastoreClient: metastoreClient,
		storage:         storage,
		writer:          writer,
		metrics:         newConsumerMetrics(reg),
		buf:             bytes.NewBuffer(make([]byte, 0, 10<<20)), // 10 MB
		decoder:         decoder,
	}, nil
}

func (c *consumer) Consume(ctx context.Context, partitionID int32, records []record) error {
	for _, record := range records {
		stream, labels, err := c.decoder.Decode(record.content)
		if err != nil {
			return fmt.Errorf("failed to decode record: %w", err)
		}
		if len(stream.Entries) == 0 {
			continue
		}
		if len(c.toStore) == 0 {
			c.toStore = make([]*logproto.Entry, 0, len(stream.Entries))
		}
		c.toStore = c.toStore[:0]
		for _, entry := range stream.Entries {
			c.toStore = append(c.toStore, &logproto.Entry{
				Timestamp:          entry.Timestamp,
				Line:               entry.Line,
				StructuredMetadata: entry.StructuredMetadata,
				Parsed:             entry.Parsed,
			})
		}
		c.writer.Append(record.tenantID, stream.Labels, labels, c.toStore, time.Now())
	}
	return nil
}

func (c *consumer) Flush(ctx context.Context) error {
	start := time.Now()
	c.metrics.flushesTotal.Add(1)
	defer func() { c.metrics.flushDuration.Observe(time.Since(start).Seconds()) }()

	defer c.buf.Reset()
	if _, err := c.writer.WriteTo(c.buf); err != nil {
		c.metrics.flushFailuresTotal.Inc()
		return err
	}

	stats := wal.GetSegmentStats(c.writer, time.Now())
	wal.ReportSegmentStats(stats, c.metrics.segmentMetrics)

	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	if err := c.storage.PutObject(ctx, fmt.Sprintf(wal.Dir+id), c.buf); err != nil {
		c.metrics.flushFailuresTotal.Inc()
		return fmt.Errorf("failed to put object: %w", err)
	}

	if _, err := c.metastoreClient.AddBlock(ctx, &metastorepb.AddBlockRequest{
		Block: c.writer.Meta(id),
	}); err != nil {
		c.metrics.flushFailuresTotal.Inc()
		return fmt.Errorf("failed to update metastore: %w", err)
	}
	c.writer.Reset()
	return nil
}

type consumerMetrics struct {
	flushesTotal       prometheus.Counter
	flushFailuresTotal prometheus.Counter
	flushDuration      prometheus.Histogram
	segmentMetrics     *wal.SegmentMetrics
}

func newConsumerMetrics(reg prometheus.Registerer) *consumerMetrics {
	return &consumerMetrics{
		flushesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_kafka_ingester_flushes_total",
			Help: "The total number of flushes.",
		}),
		flushFailuresTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_ingester_rf1_flush_failures_total",
			Help: "The total number of failed flushes.",
		}),
		flushDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_flush_duration_seconds",
			Help:                        "The flush duration (in seconds).",
			Buckets:                     prometheus.ExponentialBuckets(0.001, 4, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		segmentMetrics: wal.NewSegmentMetrics(reg),
	}
}
