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
	"github.com/twmb/franz-go/pkg/kadm"

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
func NewScheduler(cfg Config, queue *JobQueue, offsetManager partition.OffsetManager, logger log.Logger, r prometheus.Registerer) (*BlockScheduler, error) {
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
		queue:                queue,
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
	lag, err := s.offsetManager.GroupLag(ctx, s.fallbackOffsetMillis)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to get group lag", "err", err)
		return err
	}

	s.publishLagMetrics(lag)

	// TODO(owen-d): parallelize work within a partition
	// TODO(owen-d): skip small jobs unless they're stale,
	//  e.g. a partition which is no longer being written to shouldn't be orphaned
	jobs, err := s.planner.Plan(ctx, 1, 0)
	if err != nil {
		level.Error(s.logger).Log("msg", "failed to plan jobs", "err", err)
	}
	level.Info(s.logger).Log("msg", "planned jobs", "count", len(jobs))

	for _, job := range jobs {
		// TODO: end offset keeps moving each time we plan jobs, maybe we should not use it as part of the job ID

		added, status, err := s.idempotentEnqueue(job)
		level.Info(s.logger).Log(
			"msg", "enqueued job",
			"added", added,
			"status", status.String(),
			"err", err,
			"partition", job.Job.Partition(),
			"num_offsets", job.Offsets().Max-job.Offsets().Min,
		)

		// if we've either added or encountered an error, move on; we're done this cycle
		if added || err != nil {
			continue
		}

		// scheduler is aware of incoming job; handling depends on status
		switch status {
		case types.JobStatusPending:
			level.Debug(s.logger).Log(
				"msg", "job is pending, updating priority",
				"old_priority", job.Priority,
			)
			s.queue.pending.UpdatePriority(job.Job.ID(), job)
		case types.JobStatusInProgress:
			level.Debug(s.logger).Log(
				"msg", "job is in progress, ignoring",
			)
		case types.JobStatusComplete:
			// shouldn't happen
			level.Debug(s.logger).Log(
				"msg", "job is complete, ignoring",
			)
		default:
			level.Error(s.logger).Log(
				"msg", "job has unknown status, ignoring",
				"status", status,
			)
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

func (s *BlockScheduler) HandleGetJob(ctx context.Context) (*types.Job, bool, error) {
	select {
	case <-ctx.Done():
		return nil, false, ctx.Err()
	default:
		job, ok := s.queue.Dequeue()
		return job, ok, nil
	}
}

// if added is true, the job was added to the queue, otherwise status is the current status of the job
func (s *BlockScheduler) idempotentEnqueue(job *JobWithMetadata) (added bool, status types.JobStatus, err error) {
	logger := log.With(
		s.logger,
		"job", job.Job.ID(),
		"priority", job.Priority,
	)

	status, ok := s.queue.Exists(job.Job)

	// scheduler is unaware of incoming job; enqueue
	if !ok {
		level.Debug(logger).Log(
			"msg", "job does not exist, enqueueing",
		)

		// enqueue
		if err := s.queue.Enqueue(job.Job, job.Priority); err != nil {
			level.Error(logger).Log("msg", "failed to enqueue job", "err", err)
			return false, types.JobStatusUnknown, err
		}

		return true, types.JobStatusPending, nil
	}

	return false, status, nil
}

func (s *BlockScheduler) HandleCompleteJob(ctx context.Context, job *types.Job, success bool) (err error) {
	logger := log.With(s.logger, "job", job.ID())

	if success {
		if err = s.offsetManager.Commit(
			ctx,
			job.Partition(),
			job.Offsets().Max-1, // max is exclusive, so commit max-1
		); err == nil {
			s.queue.MarkComplete(job.ID(), types.JobStatusComplete)
			level.Info(logger).Log("msg", "job completed successfully")

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
				_, _, _ = s.idempotentEnqueue(jobs[nextJob])
			}

			return nil
		}

		level.Error(logger).Log("msg", "failed to commit offset", "err", err)
	}

	level.Error(logger).Log("msg", "job failed, re-enqueuing")
	s.queue.MarkComplete(job.ID(), types.JobStatusFailed)
	s.queue.pending.Push(
		NewJobWithMetadata(
			job,
			DefaultPriority,
		),
	)
	return nil
}

func (s *BlockScheduler) HandleSyncJob(_ context.Context, job *types.Job) error {
	s.queue.SyncJob(job.ID(), job)
	return nil
}

func (s *BlockScheduler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	newStatusPageHandler(s.queue, s.offsetManager, s.fallbackOffsetMillis).ServeHTTP(w, req)
}
