package ingester

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"

	"github.com/grafana/dskit/backoff"

	"github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/wal"
)

// ObjectStorage defines an interface for object storage operations
type ObjectStorage interface {
	PutObject(ctx context.Context, objectKey string, object io.Reader) error
}

// MetadataStore defines an interface for metadata storage operations
type MetadataStore interface {
	AddBlock(ctx context.Context, in *metastorepb.AddBlockRequest, opts ...grpc.CallOption) (*metastorepb.AddBlockResponse, error)
}

// Committer defines an interface for committing offsets
type Committer interface {
	Commit(ctx context.Context, offset int64) error
}

// consumer represents a Kafka consumer that processes and stores log entries
type consumer struct {
	metastoreClient MetadataStore
	storage         ObjectStorage
	writer          *wal.SegmentWriter
	committer       Committer
	flushInterval   time.Duration
	maxFlushSize    int64
	lastOffset      int64

	flushBuf *bytes.Buffer
	decoder  *kafka.Decoder
	toStore  []*logproto.Entry

	metrics *consumerMetrics
	logger  log.Logger
}

// NewConsumerFactory creates and initializes a new consumer instance
func NewConsumerFactory(
	metastoreClient MetadataStore,
	storage ObjectStorage,
	flushInterval time.Duration,
	maxFlushSize int64,
	logger log.Logger,
	reg prometheus.Registerer,
) ConsumerFactory {
	return func(committer Committer) (Consumer, error) {
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
			flushBuf:        bytes.NewBuffer(make([]byte, 0, 10<<20)), // 10 MB
			decoder:         decoder,
			committer:       committer,
			flushInterval:   flushInterval,
			maxFlushSize:    maxFlushSize,
			lastOffset:      -1,
		}, nil
	}
}

// Start starts the consumer and returns a function to wait for it to finish
// It consumes records from the recordsChan, and flushes them to storage periodically.
func (c *consumer) Start(ctx context.Context, recordsChan <-chan []record) func() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		flushTicker := time.NewTicker(c.flushInterval)
		defer flushTicker.Stop()
		for {
			select {
			case <-flushTicker.C:
				level.Info(c.logger).Log("msg", "flushing block")
				c.Flush()
			case <-ctx.Done():
				level.Info(c.logger).Log("msg", "shutting down consumer")
				c.Flush()
				return
			case records := <-recordsChan:
				if err := c.consume(records); err != nil {
					level.Error(c.logger).Log("msg", "failed to consume records", "error", err)
					return
				}
				if c.writer.InputSize() > c.maxFlushSize {
					level.Info(c.logger).Log("msg", "flushing block due to size limit", "size", humanize.Bytes(uint64(c.writer.InputSize())))
					c.Flush()
				}
			}
		}
	}()
	return wg.Wait
}

// consume processes a batch of Kafka records, decoding and storing them
func (c *consumer) consume(records []record) error {
	if len(records) == 0 {
		return nil
	}
	var (
		minOffset = int64(math.MaxInt64)
		maxOffset = int64(0)
	)
	for _, record := range records {
		minOffset = min(minOffset, record.offset)
		maxOffset = max(maxOffset, record.offset)
	}
	level.Debug(c.logger).Log("msg", "consuming records", "min_offset", minOffset, "max_offset", maxOffset)
	return c.retryWithBackoff(context.Background(), backoff.Config{
		MinBackoff: 250 * time.Millisecond,
		MaxBackoff: 2 * time.Second,
		MaxRetries: 0, // retry forever
	}, func(boff *backoff.Backoff) error {
		consumeStart := time.Now()
		if err := c.appendRecords(records); err != nil {
			level.Error(c.logger).Log(
				"msg", "encountered error while ingesting data from Kafka; should retry",
				"err", err,
				"record_min_offset", minOffset,
				"record_max_offset", maxOffset,
				"num_retries", boff.NumRetries(),
			)
			return err
		}
		c.lastOffset = maxOffset
		c.metrics.currentOffset.Set(float64(c.lastOffset))
		c.metrics.consumeLatency.Observe(time.Since(consumeStart).Seconds())
		return nil
	})
}

func (c *consumer) appendRecords(records []record) error {
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

// Flush writes the accumulated data to storage and updates the metadata store
func (c *consumer) Flush() {
	if c.writer.InputSize() == 0 {
		return
	}
	if c.lastOffset == -1 {
		return
	}
	if err := c.retryWithBackoff(context.Background(), backoff.Config{
		MinBackoff: 250 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 0, // retry forever
	}, func(boff *backoff.Backoff) error {
		start := time.Now()
		c.metrics.flushesTotal.Add(1)
		defer func() { c.metrics.flushDuration.Observe(time.Since(start).Seconds()) }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := c.flush(ctx); err != nil {
			c.metrics.flushFailuresTotal.Inc()
			level.Error(c.logger).Log(
				"msg", "failed to flush block",
				"error", err,
				"num_retries", boff.NumRetries(),
			)
			return err
		}
		c.lastOffset = -1
		return nil
	}); err != nil {
		level.Error(c.logger).Log("msg", "failed to flush block", "error", err)
	}
}

func (c *consumer) retryWithBackoff(ctx context.Context, cfg backoff.Config, fn func(boff *backoff.Backoff) error) error {
	boff := backoff.New(ctx, cfg)
	var err error
	for boff.Ongoing() {
		err = fn(boff)
		if err == nil {
			return nil
		}
		boff.Wait()
	}
	if err != nil {
		return err
	}
	return boff.ErrCause()
}

func (c *consumer) flush(ctx context.Context) error {
	defer c.flushBuf.Reset()
	if _, err := c.writer.WriteTo(c.flushBuf); err != nil {
		return err
	}

	stats := wal.GetSegmentStats(c.writer, time.Now())
	wal.ReportSegmentStats(stats, c.metrics.segmentMetrics)

	id := ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
	if err := c.storage.PutObject(ctx, fmt.Sprintf(wal.Dir+id), c.flushBuf); err != nil {
		return fmt.Errorf("failed to put object to object storage: %w", err)
	}

	if _, err := c.metastoreClient.AddBlock(ctx, &metastorepb.AddBlockRequest{
		Block: c.writer.Meta(id),
	}); err != nil {
		return fmt.Errorf("failed to add block to metastore: %w", err)
	}
	c.writer.Reset()
	if err := c.committer.Commit(ctx, c.lastOffset); err != nil {
		return fmt.Errorf("failed to commit offset: %w", err)
	}

	return nil
}

// consumerMetrics holds various Prometheus metrics for monitoring consumer operations
type consumerMetrics struct {
	flushesTotal       prometheus.Counter
	flushFailuresTotal prometheus.Counter
	flushDuration      prometheus.Histogram
	segmentMetrics     *wal.SegmentMetrics
	consumeLatency     prometheus.Histogram
	currentOffset      prometheus.Gauge
}

// newConsumerMetrics initializes and returns a new consumerMetrics instance
func newConsumerMetrics(reg prometheus.Registerer) *consumerMetrics {
	return &consumerMetrics{
		flushesTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_kafka_ingester_flushes_total",
			Help: "The total number of flushes.",
		}),
		flushFailuresTotal: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_kafka_ingester_flush_failures_total",
			Help: "The total number of failed flushes.",
		}),
		flushDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_kafka_ingester_flush_duration_seconds",
			Help:                        "The flush duration (in seconds).",
			Buckets:                     prometheus.ExponentialBuckets(0.001, 4, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		consumeLatency: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingest_storage_reader_records_batch_process_duration_seconds",
			Help:                        "How long a consumer spent processing a batch of records from Kafka.",
			NativeHistogramBucketFactor: 1.1,
		}),
		segmentMetrics: wal.NewSegmentMetrics(reg),
		currentOffset: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_kafka_ingester_current_offset",
			Help: "The current offset of the Kafka consumer.",
		}),
	}
}
