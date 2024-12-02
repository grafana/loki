package scheduler

import (
	"context"
	"errors"
	"flag"
	"strconv"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
)

var (
	_ types.Scheduler = unimplementedScheduler{}
	_ types.Scheduler = &BlockScheduler{}
)

type Config struct {
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

// BlockScheduler implements the Scheduler interface
type BlockScheduler struct {
	services.Service

	cfg     Config
	logger  log.Logger
	queue   *JobQueue
	metrics *Metrics

	offsetReader OffsetReader
	planner      Planner
}

// NewScheduler creates a new scheduler instance
func NewScheduler(cfg Config, queue *JobQueue, offsetReader OffsetReader, logger log.Logger, r prometheus.Registerer) *BlockScheduler {
	planner := NewTimeRangePlanner(cfg.TargetRecordConsumptionPeriod, offsetReader, func() time.Time { return time.Now().UTC() }, logger)
	s := &BlockScheduler{
		cfg:          cfg,
		planner:      planner,
		offsetReader: offsetReader,
		logger:       logger,
		metrics:      NewMetrics(r),
		queue:        queue,
	}
	s.Service = services.NewBasicService(nil, s.running, nil)
	return s
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

func (s *BlockScheduler) HandleGetJob(ctx context.Context, builderID string) (*types.Job, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
		return s.queue.Dequeue(builderID)
	}
}

func (s *BlockScheduler) HandleCompleteJob(_ context.Context, builderID string, job *types.Job) error {
	return s.queue.MarkComplete(job.ID, builderID)
}

func (s *BlockScheduler) HandleSyncJob(_ context.Context, builderID string, job *types.Job) error {
	return s.queue.SyncJob(job.ID, builderID, job)
}

// unimplementedScheduler provides default implementations that panic.
type unimplementedScheduler struct{}

func (s unimplementedScheduler) HandleGetJob(_ context.Context, _ string) (*types.Job, bool, error) {
	panic("unimplemented")
}

func (s unimplementedScheduler) HandleCompleteJob(_ context.Context, _ string, _ *types.Job) error {
	panic("unimplemented")
}

func (s unimplementedScheduler) HandleSyncJob(_ context.Context, _ string, _ *types.Job) error {
	panic("unimplemented")
}
