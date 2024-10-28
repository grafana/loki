package blockbuilder

import (
	"context"
)

// [min,max)
type Offsets struct {
	Min, Max int64
}

type Job struct {
	Partition int32
	Offsets   Offsets
}

// Interface required for interacting with queue partitions.
type PartitionController interface {
	// Returns the highest committed offset from the consumer group
	HighestCommittedOffset(ctx context.Context) (int64, error)
	// Returns the highest available offset in the partition
	HighestPartitionOffset(ctx context.Context) (int64, error)
	// Commits the offset to the consumer group.
	Commit(context.Context, int64) error
	// Process will run load batches at a time and send them to channel,
	// so it's advised to not buffer the channel for natural backpressure.
	// As a convenience, it returns the last seen offset, which matches
	// the final record sent on the channel.
	Process(context.Context, Offsets, chan<- []AppendInput) (int64, error)

	Close() error
}

// PartitionJobController loads a single job a time, bound to a given
// * topic
// * partition
// * offset_step_len: the number of offsets each job to contain. e.g. "10" could yield a job w / min=15, max=25
//
// At a high level, it watches a source topic/partition (where log data is ingested) and a "committed" topic/partition.
// The "comitted" partition corresponds to the offsets from the source partition which have been committed to object storage.
// In essence, the following loop is performed
//  1. load the most recent record from the "comitted" partition. This contains the highest msg offset in the "source" partition
//     that has been committed to object storage. We'll call that $START_POS.
//  2. Create a job with `min=$START_POS+1,end=$START_POS+1+$STEP_LEN`
//  3. Sometime later when the job has been processed, we'll commit the final processed offset from the "source" partition (which
//     will be <= $END_POS) to the "committed" partition.
//
// NB(owen-d): In our case, "source" is the partition
//
//	containing log data and "committed" is the consumer group
type PartitionJobController struct {
	topic     string
	partition int32
	stepLen   int64
	part      PartitionController
}

// LoadJob(ctx) returns the next job by finding the most recent unconsumed offset in the partition
// Returns whether an applicable job exists, the job, and an error
func (l *PartitionJobController) LoadJob(ctx context.Context) (bool, Job, error) {
	// Read the most recent committed offset
	startOffset, err := l.part.HighestCommittedOffset(ctx)
	if err != nil {
		return false, Job{}, err
	}

	highestOffset, err := l.part.HighestPartitionOffset(ctx)
	if err != nil {
		return false, Job{}, err
	}

	if highestOffset == startOffset {
		return false, Job{}, nil
	}

	// Create the job with the calculated offsets
	job := Job{
		Partition: l.partition,
		Offsets: Offsets{
			Min: startOffset,
			Max: startOffset + l.stepLen,
		},
	}

	return true, job, nil
}
