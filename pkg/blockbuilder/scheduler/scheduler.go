package scheduler

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

var (
	_ types.SchedulerHandler = &BlockScheduler{}
)

type Config struct {
	Interval          time.Duration  `yaml:"interval"`
	LookbackPeriod    time.Duration  `yaml:"lookback_period"`
	Strategy          string         `yaml:"strategy"`
	TargetRecordCount int64          `yaml:"target_record_count"`
	JobQueueConfig    JobQueueConfig `yaml:"job_queue"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.Interval, prefix+"interval", 15*time.Minute, "How often the scheduler should plan jobs.")
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
	cfg.JobQueueConfig.RegisterFlags(f)
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

	fallbackOffsetMillis int64
	offsetManager        partition.OffsetManager
	planner              Planner
}

// NewScheduler creates a new scheduler instance
func NewScheduler(cfg Config, offsetManager partition.OffsetManager, logger log.Logger, r prometheus.Registerer) (*BlockScheduler, error) {
	// pin the fallback offset at the time of scheduler creation to ensure planner uses the same fallback offset on subsequent runs
	// without this, planner would create jobs that are unaligned when the partition has no commits so far.
	fallbackOffsetMillis := int64(partition.KafkaStartOffset)
	if cfg.LookbackPeriod > 0 {
		fallbackOffsetMillis = time.Now().UnixMilli() - cfg.LookbackPeriod.Milliseconds()
	}

	var planner Planner
	switch cfg.Strategy {
	case RecordCountStrategy:
		planner = NewRecordCountPlanner(offsetManager, cfg.TargetRecordCount, fallbackOffsetMillis, logger)
	default:
		return nil, fmt.Errorf("invalid strategy: %s", cfg.Strategy)
	}

	s := &BlockScheduler{
		cfg:                  cfg,
		planner:              planner,
		offsetManager:        offsetManager,
		logger:               logger,
		metrics:              NewMetrics(r),
		queue:                NewJobQueue(cfg.JobQueueConfig, logger, r),
		fallbackOffsetMillis: fallbackOffsetMillis,
	}

	s.Service = services.NewBasicService(nil, s.running, nil)
	return s, nil
}

func (s *BlockScheduler) running(ctx context.Context) error {
	if err := s.runOnce(ctx); err != nil {
		level.Error(s.logger).Log("msg", "failed to schedule jobs", "err", err)
	}

	go s.queue.RunLeaseExpiryChecker(ctx)
	go s.publishLagLoop(ctx)

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
	// TODO(owen-d): parallelize work within a partition
	// TODO(owen-d): skip small jobs unless they're stale,
	//  e.g. a partition which is no longer being written to shouldn't be orphaned
	jobs, err := s.planner.Plan(ctx, 1, 0)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to plan jobs", "err", err)
	}
	level.Info(s.logger).Log("msg", "planned jobs", "count", len(jobs))

	// TODO: end offset keeps moving each time we plan jobs, maybe we should not use it as part of the job ID

	for _, job := range jobs {
		if err := s.handlePlannedJob(job); err != nil {
			level.Error(s.logger).Log("msg", "failed to handle planned job", "err", err)
		}
	}

	return nil
}

func (s *BlockScheduler) publishLagLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lag, err := s.offsetManager.GroupLag(ctx, s.fallbackOffsetMillis)
			if err != nil {
				level.Error(s.logger).Log("msg", "failed to get group lag for metric publishing", "err", err)
			}
			s.publishLagMetrics(lag)
		case <-ctx.Done():
			return
		}
	}
}

func (s *BlockScheduler) handlePlannedJob(job *JobWithMetadata) error {
	logger := log.With(
		s.logger,
		"job", job.Job.ID(),
		"partition", job.Job.Partition(),
		"num_offsets", job.Offsets().Max-job.Offsets().Min,
	)

	status, exists := s.queue.Exists(job.Job.ID())
	if !exists {
		// New job, enqueue it
		_, _, err := s.queue.TransitionAny(job.Job.ID(), types.JobStatusPending, func() (*JobWithMetadata, error) {
			return job, nil
		})
		if err != nil {
			level.Error(logger).Log("msg", "failed to enqueue new job", "err", err)
			return err
		}
		level.Info(logger).Log("msg", "enqueued new job")
		return nil
	}

	// Job exists, handle based on current status
	switch status {
	case types.JobStatusComplete:
		// Job already completed successfully, no need to replan
		level.Debug(logger).Log("msg", "job already completed successfully, skipping")

	case types.JobStatusPending:
		// Update priority of pending job
		if updated := s.queue.UpdatePriority(job.Job.ID(), job.Priority); !updated {
			// Job is no longer pending, skip it for this iteration
			level.Debug(logger).Log("msg", "job no longer pending, skipping priority update")
			return nil
		}
		level.Debug(logger).Log("msg", "updated priority of pending job", "new_priority", job.Priority)

	case types.JobStatusFailed, types.JobStatusExpired:
		// Re-enqueue failed or expired jobs
		_, _, err := s.queue.TransitionAny(job.Job.ID(), types.JobStatusPending, func() (*JobWithMetadata, error) {
			return job, nil
		})
		if err != nil {
			level.Error(logger).Log("msg", "failed to re-enqueue failed/expired job", "err", err)
			return err
		}
		level.Info(logger).Log("msg", "re-enqueued failed/expired job", "status", status)

	case types.JobStatusInProgress:
		// Job is being worked on, ignore it
		level.Debug(logger).Log("msg", "job is in progress, ignoring")

	default:
		level.Error(logger).Log("msg", "job has unknown status, ignoring", "status", status)
	}

	return nil
}

func (s *BlockScheduler) publishLagMetrics(lag map[int32]partition.Lag) {
	for partition, l := range lag {
		// useful for scaling builders
		s.metrics.lag.WithLabelValues(strconv.Itoa(int(partition))).Set(float64(l.Lag()))
		s.metrics.committedOffset.WithLabelValues(strconv.Itoa(int(partition))).Set(float64(l.LastCommittedOffset()))
	}
}

func (s *BlockScheduler) HandleGetJob(ctx context.Context) (*types.Job, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
		job, ok := s.queue.Dequeue()
		return job, ok, nil
	}
}

func (s *BlockScheduler) HandleCompleteJob(ctx context.Context, job *types.Job, success bool) (err error) {
	logger := log.With(s.logger, "job", job.ID())

	if success {
		if err = s.offsetManager.Commit(
			ctx,
			job.Partition(),
			job.Offsets().Max-1, // max is exclusive, so commit max-1
		); err == nil {
			level.Info(logger).Log("msg", "job completed successfully")
			if _, _, transitionErr := s.queue.TransitionAny(job.ID(), types.JobStatusComplete, func() (*JobWithMetadata, error) {
				return NewJobWithMetadata(job, DefaultPriority), nil
			}); transitionErr != nil {
				level.Warn(logger).Log("msg", "failed to mark successful job as complete", "err", transitionErr)
			}

			// TODO(owen-d): cleaner way to enqueue next job for this partition,
			// don't make it part of the response cycle to job completion, etc.
			// NB(owen-d): only immediately enqueue another job for this partition if]
			// the job is full. Otherwise, we'd repeatedly enqueue tiny jobs with a few records.
			jobs, err := s.planner.Plan(ctx, 1, int(s.cfg.TargetRecordCount))
			if err != nil {
				level.Error(logger).Log("msg", "failed to plan subsequent jobs", "err", err)
			}

			// find first job for this partition
			nextJob := sort.Search(len(jobs), func(i int) bool {
				return jobs[i].Job.Partition() >= job.Partition()
			})

			if nextJob < len(jobs) && jobs[nextJob].Job.Partition() == job.Partition() {
				if err := s.handlePlannedJob(jobs[nextJob]); err != nil {
					level.Error(logger).Log("msg", "failed to handle subsequent job", "err", err)
				}
			}
			return nil
		}

		level.Error(logger).Log("msg", "failed to commit offset", "err", err)
	}

	// mark as failed
	prev, found, err := s.queue.TransitionAny(job.ID(), types.JobStatusFailed, func() (*JobWithMetadata, error) {
		return NewJobWithMetadata(job, DefaultPriority), nil
	})
	if err != nil {
		level.Error(logger).Log("msg", "failed to mark job failure", "prev", prev, "found", found, "err", err)
	} else {
		level.Error(logger).Log("msg", "marked job failure", "prev", prev, "found", found)
	}

	cpy := *job
	if err := s.handlePlannedJob(NewJobWithMetadata(&cpy, DefaultPriority)); err != nil {
		level.Error(logger).Log("msg", "failed to handle subsequent job", "err", err)
	}
	return nil
}

func (s *BlockScheduler) HandleSyncJob(_ context.Context, job *types.Job) error {
	_, _, err := s.queue.TransitionAny(
		job.ID(),
		types.JobStatusInProgress,
		func() (*JobWithMetadata, error) {
			return NewJobWithMetadata(job, DefaultPriority), nil
		},
	)

	// Update last-updated timestamp
	_ = s.queue.Ping(job.ID())

	if err != nil {
		level.Error(s.logger).Log("msg", "failed to sync job", "job", job.ID(), "err", err)
	}
	return err
}

func (s *BlockScheduler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	newStatusPageHandler(s.queue, s.offsetManager, s.fallbackOffsetMillis).ServeHTTP(w, req)
}
