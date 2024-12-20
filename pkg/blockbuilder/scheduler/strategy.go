package scheduler

import (
	"context"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

const (
	DefaultBufferPeriod = time.Minute * 30
)

// OffsetReader is an interface to list offsets for all partitions of a topic from Kafka.
type OffsetReader interface {
	GroupLag(context.Context, time.Duration) (map[int32]kadm.GroupMemberLag, error)
	FetchPartitionOffset(context.Context, int32, partition.SpecialOffset) (int64, error)
}

type Planner interface {
	Name() string
	Plan(ctx context.Context, maxJobsPerPartition int) ([]*JobWithMetadata, error)
}

const (
	RecordCountStrategy = "record-count"
	TimeSpanStrategy    = "time-span"
)

var validStrategies = []string{
	RecordCountStrategy,
	TimeSpanStrategy,
}

// tries to consume upto targetRecordCount records per partition
type RecordCountPlanner struct {
	targetRecordCount int64
	lookbackPeriod    time.Duration
	offsetReader      OffsetReader
	logger            log.Logger
}

func NewRecordCountPlanner(offsetReader OffsetReader, targetRecordCount int64, lookbackPeriod time.Duration, logger log.Logger) *RecordCountPlanner {
	return &RecordCountPlanner{
		targetRecordCount: targetRecordCount,
		lookbackPeriod:    lookbackPeriod,
		offsetReader:      offsetReader,
		logger:            logger,
	}
}

func (p *RecordCountPlanner) Name() string {
	return RecordCountStrategy
}

func (p *RecordCountPlanner) Plan(ctx context.Context, maxJobsPerPartition int) ([]*JobWithMetadata, error) {
	offsets, err := p.offsetReader.GroupLag(ctx, p.lookbackPeriod)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to get group lag", "err", err)
		return nil, err
	}

	jobs := make([]*JobWithMetadata, 0, len(offsets))
	for _, partitionOffset := range offsets {
		// 1. kadm.GroupMemberLag contains valid Commit.At even when consumer group never committed any offset.
		//    no additional validation is needed here
		// 2. committed offset could be behind start offset if we are falling behind retention period.
		startOffset := max(partitionOffset.Commit.At+1, partitionOffset.Start.Offset)
		endOffset := partitionOffset.End.Offset

		// Skip if there's no lag
		if startOffset >= endOffset {
			continue
		}

		var jobCount int
		currentStart := startOffset
		// Create jobs of size targetRecordCount until we reach endOffset
		for currentStart < endOffset {
			if maxJobsPerPartition > 0 && jobCount >= maxJobsPerPartition {
				break
			}

			currentEnd := min(currentStart+p.targetRecordCount, endOffset)
			job := NewJobWithMetadata(
				types.NewJob(partitionOffset.Partition, types.Offsets{
					Min: currentStart,
					Max: currentEnd,
				}),
				int(endOffset-currentStart), // priority is remaining records to process
			)
			jobs = append(jobs, job)

			currentStart = currentEnd
			jobCount++
		}
	}

	// Sort jobs by partition then priority
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Job.Partition() != jobs[j].Job.Partition() {
			return jobs[i].Job.Partition() < jobs[j].Job.Partition()
		}
		return jobs[i].Priority > jobs[j].Priority
	})

	return jobs, nil
}

type TimeSpanPlanner struct {
	targetPeriod time.Duration
	now          func() time.Time

	lookbackPeriod time.Duration
	offsetReader   OffsetReader

	logger log.Logger
}

func NewTimeSpanPlanner(interval time.Duration, lookbackPeriod time.Duration, offsetReader OffsetReader, now func() time.Time, logger log.Logger) *TimeSpanPlanner {
	return &TimeSpanPlanner{
		targetPeriod:   interval,
		lookbackPeriod: lookbackPeriod,
		offsetReader:   offsetReader,
		now:            now,
		logger:         logger,
	}
}

func (p *TimeSpanPlanner) Name() string {
	return TimeSpanStrategy
}

// create a single job per partition
// do not consume records within buffer time - defaults to 30m
func (p *TimeSpanPlanner) Plan(ctx context.Context, maxJobsPerPartition int) ([]*JobWithMetadata, error) {
	// Add some buffer time to avoid consuming recent logs.
	// truncate to the nearest Interval
	consumeBoundary := p.now().Add(-DefaultBufferPeriod).Truncate(p.targetPeriod).UnixMilli()
	level.Info(p.logger).Log("msg", "start planning", " consumeBoundary", time.UnixMilli(consumeBoundary))

	offsets, err := p.offsetReader.GroupLag(ctx, p.lookbackPeriod)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to get group lag", "err", err)
		return nil, err
	}

	jobs := make([]*JobWithMetadata, 0, len(offsets))
	for _, partitionOffset := range offsets {
		if partitionOffset.End.Offset <= partitionOffset.Start.Offset {
			continue
		}

		// 1. kadm.GroupMemberLag contains valid Commit.At even when consumer group never committed any offset.
		//    no additional validation is needed here
		// 2. committed offset could be behind start offset if we are falling behind retention period.
		startOffset := max(partitionOffset.Commit.At+1, partitionOffset.Start.Offset)

		var lastConsumedRecordTS int64
		if partitionOffset.Commit.Metadata != "" {
			lastConsumedRecordTS, err = partition.UnmarshallCommitMeta(partitionOffset.Commit.Metadata)
			if err != nil {
				level.Error(p.logger).Log("msg", "failed to unmarshal commit metadata", "err", err)
				break
			}
		} else {
			level.Error(p.logger).Log("msg", "no commit metadata found", "partition", partitionOffset.Partition)
			break
		}

		consumeUptoTS := min(lastConsumedRecordTS+p.targetPeriod.Milliseconds(), consumeBoundary)
		consumeUptoOffset, err := p.offsetReader.FetchPartitionOffset(ctx, partitionOffset.Partition, partition.SpecialOffset(consumeUptoTS))
		if err != nil {
			level.Error(p.logger).Log("msg", "failed to list offsets after timestamp", "err", err)
			return nil, err
		}

		// no records >= consumeUptoTS
		if consumeUptoOffset == -1 {
			level.Info(p.logger).Log("msg", "no records to eligible to process", "partition", partitionOffset.Partition,
				"commitOffset", partitionOffset.Commit.At,
				"latestOffset", partitionOffset.End.Offset,
				"consumeUptoTS", time.UnixMilli(consumeUptoTS))
			continue
		}

		endOffset := consumeUptoOffset
		if startOffset >= endOffset {
			level.Info(p.logger).Log("msg", "no records to process", "partition", partitionOffset.Partition,
				"commitOffset", partitionOffset.Commit.At,
				"startOffset", startOffset,
				"endOffset", endOffset)
			continue
		}

		job := NewJobWithMetadata(
			types.NewJob(partitionOffset.Partition, types.Offsets{
				Min: startOffset,
				Max: endOffset,
			}), int(endOffset-startOffset),
		)

		level.Debug(p.logger).Log("msg", "created job", "partition", partitionOffset.Partition, "consumeUptoTS", time.UnixMilli(consumeUptoTS), "min", startOffset, "max", endOffset)
		jobs = append(jobs, job)
	}

	// Sort jobs by partition number to ensure consistent ordering
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].Job.Partition() < jobs[j].Job.Partition()
	})

	return jobs, nil
}
