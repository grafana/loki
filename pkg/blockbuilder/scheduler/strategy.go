package scheduler

import (
	"context"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/twmb/franz-go/pkg/kadm"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
)

// OffsetReader is an interface to list offsets for all partitions of a topic from Kafka.
type OffsetReader interface {
	ListOffsetsAfterMilli(context.Context, int64) (map[int32]kadm.ListedOffset, error)
	GroupLag(context.Context) (map[int32]kadm.GroupMemberLag, error)
}

type Planner interface {
	Name() string
	Plan(ctx context.Context) ([]*JobWithPriority[int], error)
}

const (
	RecordCountStrategy = "record_count"
	TimeRangeStrategy   = "time_range"
)

// tries to consume upto targetRecordCount records per partition
type RecordCountPlanner struct {
	targetRecordCount int64
	offsetReader      OffsetReader
	logger            log.Logger
}

func NewRecordCountPlanner(targetRecordCount int64) *RecordCountPlanner {
	return &RecordCountPlanner{
		targetRecordCount: targetRecordCount,
	}
}

func (p *RecordCountPlanner) Name() string {
	return RecordCountStrategy
}

func (p *RecordCountPlanner) Plan(ctx context.Context) ([]*JobWithPriority[int], error) {
	offsets, err := p.offsetReader.GroupLag(ctx)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to get group lag", "err", err)
		return nil, err
	}

	var jobs []*JobWithPriority[int]
	for _, partitionOffset := range offsets {
		// kadm.GroupMemberLag contains valid Commit.At even when consumer group never committed any offset.
		// no additional validation is needed here
		startOffset := partitionOffset.Commit.At + 1
		endOffset := min(startOffset+p.targetRecordCount, partitionOffset.End.Offset)

		job := NewJobWithPriority(
			types.NewJob(int(partitionOffset.Partition), types.Offsets{
				Min: startOffset,
				Max: endOffset,
			}), int(partitionOffset.End.Offset-startOffset),
		)

		jobs = append(jobs, job)
	}

	// Sort jobs by partition number to ensure consistent ordering
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].Job.Partition < jobs[j].Job.Partition
	})

	return jobs, nil
}

// Targets consuming records spanning a configured period.
// This is a stateless planner, it is upto the caller to deduplicate or update jobs that are already in queue or progress.
type TimeRangePlanner struct {
	offsetReader OffsetReader

	buffer       time.Duration
	targetPeriod time.Duration
	now          func() time.Time

	logger log.Logger
}

func NewTimeRangePlanner(interval time.Duration, offsetReader OffsetReader, now func() time.Time, logger log.Logger) *TimeRangePlanner {
	return &TimeRangePlanner{
		targetPeriod: interval,
		buffer:       interval,
		offsetReader: offsetReader,
		now:          now,
		logger:       logger,
	}
}

func (p *TimeRangePlanner) Name() string {
	return TimeRangeStrategy
}

func (p *TimeRangePlanner) Plan(ctx context.Context) ([]*JobWithPriority[int], error) {
	// truncate to the nearest Interval
	consumeUptoTS := p.now().Add(-p.buffer).Truncate(p.targetPeriod)

	// this will return the latest offset in the partition if no records are produced after this ts.
	consumeUptoOffsets, err := p.offsetReader.ListOffsetsAfterMilli(ctx, consumeUptoTS.UnixMilli())
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to list offsets after timestamp", "err", err)
		return nil, err
	}

	offsets, err := p.offsetReader.GroupLag(ctx)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to get group lag", "err", err)
		return nil, err
	}

	var jobs []*JobWithPriority[int]
	for _, partitionOffset := range offsets {
		startOffset := partitionOffset.Commit.At + 1
		// TODO: we could further break down the work into Interval sized chunks if this partition has pending records spanning a long time range
		// or have the builder consume in chunks and commit the job status back to scheduler.
		endOffset := consumeUptoOffsets[partitionOffset.Partition].Offset

		if startOffset >= endOffset {
			level.Info(p.logger).Log("msg", "no pending records to process", "partition", partitionOffset.Partition,
				"commitOffset", partitionOffset.Commit.At,
				"consumeUptoOffset", consumeUptoOffsets[partitionOffset.Partition].Offset)
			continue
		}

		job := NewJobWithPriority(
			types.NewJob(int(partitionOffset.Partition), types.Offsets{
				Min: startOffset,
				Max: endOffset,
			}), int(endOffset-startOffset),
		)

		jobs = append(jobs, job)
	}

	// Sort jobs by partition number to ensure consistent ordering
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].Job.Partition < jobs[j].Job.Partition
	})

	return jobs, nil
}
