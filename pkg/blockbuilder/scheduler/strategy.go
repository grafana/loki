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
	GroupLag(context.Context, time.Duration) (map[int32]kadm.GroupMemberLag, error)
}

type Planner interface {
	Name() string
	Plan(ctx context.Context, maxJobsPerPartition int) ([]*JobWithMetadata, error)
}

const (
	RecordCountStrategy = "record-count"
)

var validStrategies = []string{
	RecordCountStrategy,
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
