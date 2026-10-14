// Package streamcount contains a service that replays the stream metadata
// records of the current UTC day from Kafka and exports the number of unique
// streams per tenant as Prometheus metrics. The counts reset when the UTC day
// changes. It never commits offsets.
package streamcount

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

var streamsDesc = prometheus.NewDesc(
	"loki_24h_streams",
	"The number of unique streams seen per tenant since the start of the current UTC day.",
	[]string{"tenant"},
	nil,
)

// kafkaConsumer allows mocking of [kgo.Client] in tests.
type kafkaConsumer interface {
	PollFetches(context.Context) kgo.Fetches
}

// Service consumes stream metadata records and counts unique streams per
// tenant. It does not use a consumer group and so consumes all partitions and
// never commits offsets.
type Service struct {
	services.Service

	cfg    Config
	client kafkaConsumer
	closer func()
	logger log.Logger

	mtx     sync.Mutex
	streams map[string]map[uint64]struct{}
	// day is the start of the UTC day being counted. Records before it are
	// ignored.
	day time.Time

	// Used for tests.
	clock quartz.Clock

	recordsFetched prometheus.Counter
	recordsInvalid prometheus.Counter
	recordsOld     prometheus.Counter
}

// New returns a new Service.
func New(cfg Config, logger log.Logger, reg prometheus.Registerer) (*Service, error) {
	kCfg := cfg.KafkaConfig
	kCfg.Topic = cfg.Topic
	c, err := client.NewReaderClient("stream-count-reader", kCfg, logger, reg,
		kgo.ConsumerGroup("stream-count"),
		kgo.ConsumeTopics(cfg.Topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AfterMilli(startOfDay(time.Now()).UnixMilli())),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}
	s := newService(cfg, c, logger, reg, quartz.NewReal())
	s.closer = c.Close
	return s, nil
}

func newService(cfg Config, c kafkaConsumer, logger log.Logger, reg prometheus.Registerer, clock quartz.Clock) *Service {
	s := &Service{
		cfg:     cfg,
		client:  c,
		logger:  logger,
		streams: make(map[string]map[uint64]struct{}),
		clock:   clock,
		recordsFetched: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "stream_count_records_fetched_total",
			Help:      "The total number of records fetched.",
		}),
		recordsInvalid: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "stream_count_records_invalid_total",
			Help:      "The total number of invalid records.",
		}),
		recordsOld: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "stream_count_records_old_total",
			Help:      "The total number of records ignored because they are from before the current UTC day.",
		}),
	}
	s.day = startOfDay(s.clock.Now())
	if reg != nil {
		reg.MustRegister(s)
	}
	s.Service = services.NewBasicService(nil, s.running, s.stopping)
	return s
}

// Describe implements [prometheus.Collector].
func (s *Service) Describe(ch chan<- *prometheus.Desc) { ch <- streamsDesc }

// Collect implements [prometheus.Collector].
func (s *Service) Collect(ch chan<- prometheus.Metric) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.rotate()
	for tenant, streams := range s.streams {
		ch <- prometheus.MustNewConstMetric(streamsDesc, prometheus.GaugeValue, float64(len(streams)), tenant)
	}
}

// Count returns the number of unique streams for the tenant.
func (s *Service) Count(tenant string) int {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.rotate()
	return len(s.streams[tenant])
}

func (s *Service) running(ctx context.Context) error {
	b := backoff.New(ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: time.Second,
	})
	for b.Ongoing() {
		if err := s.poll(ctx); err != nil {
			if errors.Is(err, kgo.ErrClientClosed) || errors.Is(err, context.Canceled) {
				return nil
			}
			level.Error(s.logger).Log("msg", "failed to poll fetches", "err", err.Error())
			b.Wait()
			continue
		}
		b.Reset()
	}
	return nil
}

func (s *Service) poll(ctx context.Context) error {
	fetches := s.client.PollFetches(ctx)
	if err := fetches.Err(); err != nil {
		return err
	}
	// Acquire the lock once per batch rather than once per record.
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.rotate()
	fetches.EachRecord(func(r *kgo.Record) {
		s.recordsFetched.Inc()
		if err := s.processRecord(r); err != nil {
			level.Error(s.logger).Log("msg", "failed to process record", "err", err.Error())
		}
	})
	return nil
}

// rotate resets the counts if the UTC day has changed. It must be called with
// s.mtx held.
func (s *Service) rotate() {
	if day := startOfDay(s.clock.Now()); day.After(s.day) {
		s.day = day
		s.streams = make(map[string]map[uint64]struct{})
	}
}

// startOfDay returns midnight UTC of the day containing t.
func startOfDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// processRecord must be called with s.mtx held for writing.
func (s *Service) processRecord(r *kgo.Record) error {
	if r.Timestamp.Before(s.day) {
		s.recordsOld.Inc()
		return nil
	}
	var rec proto.StreamMetadataRecord
	if err := rec.Unmarshal(r.Value); err != nil {
		s.recordsInvalid.Inc()
		return fmt.Errorf("corrupted record: %w", err)
	}
	if rec.Metadata == nil {
		s.recordsInvalid.Inc()
		return errors.New("record has no metadata")
	}
	m, ok := s.streams[rec.Tenant]
	if !ok {
		m = make(map[uint64]struct{})
		s.streams[rec.Tenant] = m
	}
	m[rec.Metadata.StreamHash] = struct{}{}
	return nil
}

func (s *Service) stopping(_ error) error {
	if s.closer != nil {
		s.closer()
	}
	return nil
}
