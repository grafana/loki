package scheduler

import (
	"context"
	"sort"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

// OffsetReader is an interface to list offsets for all partitions of a topic from Kafka.
type OffsetReader interface {
	GroupLag(context.Context, int64) (map[int32]partition.Lag, error)
}

type Planner interface {
	Name() string
	Plan(ctx context.Context, maxJobsPerPartition int, minOffsetsPerJob int) ([]*JobWithMetadata, error)
}

const (
	RecordCountStrategy = "record-count"
)

var validStrategies = []string{
	RecordCountStrategy,
}

// tries to consume upto targetRecordCount records per partition
type RecordCountPlanner struct {
	targetRecordCount    int64
	fallbackOffsetMillis int64
	offsetReader         OffsetReader
	logger               log.Logger
}

func NewRecordCountPlanner(offsetReader OffsetReader, targetRecordCount int64, fallbackOffsetMillis int64, logger log.Logger) *RecordCountPlanner {
	return &RecordCountPlanner{
		targetRecordCount:    targetRecordCount,
		fallbackOffsetMillis: fallbackOffsetMillis,
		offsetReader:         offsetReader,
		logger:               logger,
	}
}

func (p *RecordCountPlanner) Name() string {
	return RecordCountStrategy
}

func (p *RecordCountPlanner) Plan(ctx context.Context, maxJobsPerPartition int, minOffsetsPerJob int) ([]*JobWithMetadata, error) {
	level.Info(p.logger).Log("msg", "planning jobs", "max_jobs_per_partition", maxJobsPerPartition, "target_record_count", p.targetRecordCount)
	offsets, err := p.offsetReader.GroupLag(ctx, p.fallbackOffsetMillis)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to get group lag", "err", err)
		return nil, err
	}

	jobs := make([]*JobWithMetadata, 0, len(offsets))
	for partition, l := range offsets {
		// Skip if there's no lag
		if l.Lag() <= 0 {
			continue
		}

		var jobCount int
		currentStart := l.FirstUncommittedOffset()
		endOffset := l.NextAvailableOffset()
		// Create jobs of size targetRecordCount until we reach endOffset
		for currentStart < endOffset {
			if maxJobsPerPartition > 0 && jobCount >= maxJobsPerPartition {
				break
			}

			currentEnd := min(currentStart+p.targetRecordCount, endOffset)

			// Skip creating job if it's smaller than minimum size
			if currentEnd-currentStart < int64(minOffsetsPerJob) {
				break
			}

			job := NewJobWithMetadata(
				types.NewJob(partition, types.Offsets{
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
