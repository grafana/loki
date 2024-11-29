package blockscheduler

import (
	"context"
	"errors"
	"flag"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/grafana/dskit/services"
)

type Config struct {
	Topic string

	ConsumerGroup string
	Interval      time.Duration

	TargetRecordConsumptionPeriod time.Duration
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.Interval, prefix+"interval", time.Minute, "How often to run scheduler top compute jobs.")
	f.DurationVar(&cfg.TargetRecordConsumptionPeriod, prefix+"target-records-spanning-period", time.Hour, "Period used by the planner to calculate the start and end offset such that each job consumes records spanning the target period.")
	f.StringVar(&cfg.ConsumerGroup, prefix+"consumer-group", "block-scheduler", "Consumer group used by block scheduler to track the last consumed offset.")
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("block-scheduler.", f)
}

func (cfg *Config) Validate() error {
	if cfg.Interval.Seconds() <= 0 {
		return errors.New("interval must be a non-zero value.")
	}

	return nil
}

type BlockScheduler struct {
	services.Service

	cfg     Config
	metrics *Metrics
	logger  log.Logger

	offsetReader OffsetReader
	planner      Planner
}

func New(cfg Config, admClient *kadm.Client, logger log.Logger, r prometheus.Registerer) (*BlockScheduler, error) {
	offsetReader := NewOffsetReader(cfg.Topic, cfg.ConsumerGroup, admClient)
	planner := NewTimeRangePlanner(cfg.TargetRecordConsumptionPeriod, offsetReader, func() time.Time { return time.Now().UTC() }, logger)
	s := &BlockScheduler{
		cfg:          cfg,
		planner:      planner,
		offsetReader: offsetReader,
		logger:       logger,
		metrics:      NewMetrics(r),
	}

	s.Service = services.NewBasicService(nil, s.running, nil)
	return s, nil
}

func (s *BlockScheduler) running(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.Interval)
	for {
		select {
		case <-ticker.C:
			if err := s.runOnce(ctx); err != nil {
				// TODO: add metrics
				level.Error(s.logger).Log("msg", "failed to schedule jobs", "err", err)
			}
		case <-ctx.Done():
			return nil
		}
	}
}

func (s *BlockScheduler) runOnce(ctx context.Context) error {
	lag, err := s.offsetReader.GroupLag(ctx)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get group lag", "err", err)
		return err
	}

	if err := s.publishLagMetrics(lag); err != nil {
		level.Error(s.logger).Log("msg", "failed to publish lag metrics", "err", err)
	}

	return nil
}

func (s *BlockScheduler) publishLagMetrics(lag map[int32]kadm.GroupMemberLag) error {
	for partition, offsets := range lag {
		s.metrics.lag.WithLabelValues(strconv.Itoa(int(partition))).Set(float64(offsets.Lag))
		s.metrics.committedOffset.WithLabelValues(strconv.Itoa(int(partition))).Set(float64(offsets.Commit.At))
	}

	return nil
}

type offsetReader struct {
	topic         string
	consumerGroup string
	adminClient   *kadm.Client
}

func NewOffsetReader(topic, consumerGroup string, adminClient *kadm.Client) OffsetReader {
	return &offsetReader{
		topic:         topic,
		consumerGroup: consumerGroup,
		adminClient:   adminClient,
	}
}

func (r *offsetReader) GroupLag(ctx context.Context) (map[int32]kadm.GroupMemberLag, error) {
	lag, err := GetGroupLag(ctx, r.adminClient, r.topic, r.consumerGroup, -1)
	if err != nil {
		return nil, err
	}

	offsets, ok := lag[r.topic]
	if !ok {
		return nil, errors.New("no lag found for the topic")
	}

	return offsets, nil
}

func (r *offsetReader) ListOffsetsAfterMilli(ctx context.Context, ts int64) (map[int32]kadm.ListedOffset, error) {
	offsets, err := r.adminClient.ListOffsetsAfterMilli(ctx, ts, r.topic)
	if err != nil {
		return nil, err
	}

	resp, ok := offsets[r.topic]
	if !ok {
		return nil, errors.New("no offsets found for the topic")
	}

	return resp, nil
}
