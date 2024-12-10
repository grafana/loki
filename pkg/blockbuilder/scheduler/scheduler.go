package scheduler

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

var (
	_ types.SchedulerHandler = &BlockScheduler{}
)

type Config struct {
	ConsumerGroup     string        `yaml:"consumer_group"`
	Interval          time.Duration `yaml:"interval"`
	LookbackPeriod    time.Duration `yaml:"lookback_period"`
	Strategy          string        `yaml:"strategy"`
	planner           Planner       `yaml:"-"` // validated planner
	TargetRecordCount int64         `yaml:"target_record_count"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.Interval, prefix+"interval", 5*time.Minute, "How often the scheduler should plan jobs.")
	f.StringVar(&cfg.ConsumerGroup, prefix+"consumer-group", "block-scheduler", "Consumer group used by block scheduler to track the last consumed offset.")
	f.DurationVar(&cfg.LookbackPeriod, prefix+"lookback-period", 0, "Lookback period used by the scheduler to plan jobs when the consumer group has no commits. 0 consumes from the start of the partition.")
	f.StringVar(
		&cfg.Strategy,
		prefix+"strategy",
		RecordCountStrategy,
		fmt.Sprintf(
			"Strategy used by the planner to plan jobs. One of %s",
			strings.Join(validStrategies, ", "),
		),
	)
	f.Int64Var(
		&cfg.TargetRecordCount,
		prefix+"target-record-count",
		1000,
		fmt.Sprintf(
			"Target record count used by the planner to plan jobs. Only used when strategy is %s",
			RecordCountStrategy,
		),
	)
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("block-scheduler.", f)
}

func (cfg *Config) Validate() error {
	if cfg.Interval <= 0 {
		return errors.New("interval must be a non-zero value")
	}

	if cfg.LookbackPeriod < -2 {
		return errors.New("only -1(latest) and -2(earliest) are valid as negative values for lookback_period")
	}

	switch cfg.Strategy {
	case RecordCountStrategy:
		if cfg.TargetRecordCount <= 0 {
			return errors.New("target record count must be a non-zero value")
		}
		cfg.planner = NewRecordCountPlanner(cfg.TargetRecordCount)
	default:
		return fmt.Errorf("invalid strategy: %s", cfg.Strategy)
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

	offsetManager partition.OffsetManager
	planner       Planner
}

// NewScheduler creates a new scheduler instance
func NewScheduler(cfg Config, queue *JobQueue, offsetManager partition.OffsetManager, logger log.Logger, r prometheus.Registerer) *BlockScheduler {
	s := &BlockScheduler{
		cfg:           cfg,
		planner:       cfg.planner,
		offsetManager: offsetManager,
		logger:        logger,
		metrics:       NewMetrics(r),
		queue:         queue,
	}
	s.Service = services.NewBasicService(nil, s.running, nil)
	return s
}

func (s *BlockScheduler) running(ctx context.Context) error {
	if err := s.runOnce(ctx); err != nil {
		level.Error(s.logger).Log("msg", "failed to schedule jobs", "err", err)
	}

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
	lag, err := s.offsetManager.GroupLag(ctx, s.cfg.LookbackPeriod)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get group lag", "err", err)
		return err
	}

	s.publishLagMetrics(lag)

	jobs, err := s.planner.Plan(ctx)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to plan jobs", "err", err)
	}

	for _, job := range jobs {
		// TODO: end offset keeps moving each time we plan jobs, maybe we should not use it as part of the job ID
		if status, ok := s.queue.Exists(job.Job); ok {
			level.Debug(s.logger).Log("msg", "job already exists", "job", job, "status", status)
			// TODO: update priority
			continue
		}

		if err := s.queue.Enqueue(job.Job, job.Priority); err != nil {
			level.Error(s.logger).Log("msg", "failed to enqueue job", "job", job, "err", err)
		}
	}

	return nil
}

func (s *BlockScheduler) publishLagMetrics(lag map[int32]kadm.GroupMemberLag) {
	for partition, offsets := range lag {
		// useful for scaling builders
		s.metrics.lag.WithLabelValues(strconv.Itoa(int(partition))).Set(float64(offsets.Lag))
		s.metrics.committedOffset.WithLabelValues(strconv.Itoa(int(partition))).Set(float64(offsets.Commit.At))
	}
}

func (s *BlockScheduler) HandleGetJob(ctx context.Context, builderID string) (*types.Job, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
		return s.queue.Dequeue(builderID)
	}
}

func (s *BlockScheduler) HandleCompleteJob(_ context.Context, _ string, job *types.Job, success bool) error {
	logger := log.With(s.logger, "job", job.ID)

	if !success {
		level.Error(logger).Log("msg", "job failed")
		return nil
	}
	s.queue.MarkComplete(job.ID)
	return nil
}

func (s *BlockScheduler) HandleSyncJob(_ context.Context, builderID string, job *types.Job) error {
	s.queue.SyncJob(job.ID, builderID, job)
	return nil
}
