package blockbuilder

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/push"
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
	Topic() string
	Partition() int32
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
// The "committed" partition corresponds to the offsets from the source partition which have been committed to object storage.
// In essence, the following loop is performed
//  1. load the most recent record from the "committed" partition. This contains the highest msg offset in the "source" partition
//     that has been committed to object storage. We'll call that $START_POS.
//  2. Create a job with `min=$START_POS+1,end=$START_POS+1+$STEP_LEN`
//  3. Sometime later when the job has been processed, we'll commit the final processed offset from the "source" partition (which
//     will be <= $END_POS) to the "committed" partition.
//
// NB(owen-d): In our case, "source" is the partition
//
//	containing log data and "committed" is the consumer group
type PartitionJobController struct {
	stepLen int64
	part    PartitionController
}

func NewPartitionJobController(
	controller PartitionController,
) *PartitionJobController {
	return &PartitionJobController{
		stepLen: 1000, // Default step length of 1000 offsets per job
		part:    controller,
	}
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
		Partition: l.part.Partition(),
		Offsets: Offsets{
			Min: startOffset,
			Max: startOffset + l.stepLen,
		},
	}

	return true, job, nil
}

// implement a dummy controller which can be parameterized to
// deterministically simulate partitions
type dummyPartitionController struct {
	topic            string
	partition        int32
	committed        int64
	highest          int64
	numTenants       int // number of unique tenants to simulate
	streamsPerTenant int // number of streams per tenant
	entriesPerOffset int // coefficient for entries per offset
}

// used in testing
// nolint:revive
func NewDummyPartitionController(topic string, partition int32, highest int64) *dummyPartitionController {
	return &dummyPartitionController{
		topic:            topic,
		partition:        partition,
		committed:        0, // always starts at zero
		highest:          highest,
		numTenants:       2, // default number of tenants
		streamsPerTenant: 2, // default streams per tenant
		entriesPerOffset: 1, // default entries per offset coefficient
	}
}

func (d *dummyPartitionController) Topic() string {
	return d.topic
}

func (d *dummyPartitionController) Partition() int32 {
	return d.partition
}

func (d *dummyPartitionController) HighestCommittedOffset(_ context.Context) (int64, error) {
	return d.committed, nil
}

func (d *dummyPartitionController) HighestPartitionOffset(_ context.Context) (int64, error) {
	return d.highest, nil
}

func (d *dummyPartitionController) Commit(ctx context.Context, offset int64) error {
	d.committed = offset
	return nil
}

func (d *dummyPartitionController) Process(ctx context.Context, offsets Offsets, ch chan<- []AppendInput) (int64, error) {
	for i := int(offsets.Min); i < int(offsets.Max); i++ {
		batch := d.createBatch(i)
		select {
		case <-ctx.Done():
			return int64(i - 1), ctx.Err()
		case ch <- batch:
		}
	}
	return offsets.Max - 1, nil
}

// creates (tenants*streams) inputs
func (d *dummyPartitionController) createBatch(offset int) []AppendInput {
	result := make([]AppendInput, 0, d.numTenants*d.streamsPerTenant)
	for i := 0; i < d.numTenants; i++ {
		tenant := fmt.Sprintf("tenant-%d", i)
		for j := 0; j < d.streamsPerTenant; j++ {
			lbls := labels.Labels{
				{Name: "stream", Value: fmt.Sprintf("stream-%d", j)},
			}
			entries := make([]push.Entry, d.entriesPerOffset)
			for k := 0; k < d.entriesPerOffset; k++ {
				entries[k] = push.Entry{
					Timestamp: time.Now(),
					Line:      fmt.Sprintf("tenant=%d stream=%d line=%d offset=%d", i, j, k, offset),
				}
			}
			result = append(result, AppendInput{
				tenant:    tenant,
				labels:    lbls,
				labelsStr: lbls.String(),
				entries:   entries,
			})
		}
	}
	return result
}

func (d *dummyPartitionController) Close() error {
	return nil
}
