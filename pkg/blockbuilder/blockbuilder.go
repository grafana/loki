package blockbuilder

import (
	"context"

	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

// [min,max)
type Offsets struct {
	Min, Max int64
}

type Job struct {
	Partition int32
	Offsets   Offsets
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
//  3. Sometime later when the job has been processed, we'll commit the final processed offset from the "source" partition
//     (which will be <= $END_POS).
type PartitionJobController struct {
	topic     string
	partition int32
	stepLen   int64
	source    partiotionReader
	committed partitionRW
}

// LoadJob(ctx) returns the next job by finding the most recent unconsumed offset in the partition
func (l *PartitionJobController) LoadJob(ctx context.Context) (Job, error) {
	// Read the most recent committed offset
	record, err := l.committed.ReadMostRecent(ctx)
	if err != nil {
		return Job{}, err
	}

	// Calculate the start offset for the new job
	startOffset := record.Offset + 1

	// Create the job with the calculated offsets
	job := Job{
		Partition: l.partition,
		Offsets: Offsets{
			Min: startOffset,
			Max: startOffset + l.stepLen,
		},
	}

	return job, nil
}

// CommitJob commits the processed offsets of a job to the committed partition
func (l *PartitionJobController) CommitJob(ctx context.Context, job Job) error {
	return l.committed.Commit(ctx, job.Offsets)
}

type partitionRW interface {
	ReadMostRecent(context.Context) (partition.Record, error)
	Commit(context.Context, Offsets) error
	partiotionReader
}

// TODO
type partiotionReader interface{}
