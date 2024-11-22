package blockbuilder

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"

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
	// Returns the earliest available offset in the partition
	EarliestPartitionOffset(ctx context.Context) (int64, error)
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
	part    partition.ReaderIfc
	backoff backoff.Config
	decoder *kafka.Decoder
}

func NewPartitionJobController(
	controller partition.ReaderIfc,
	backoff backoff.Config,
) (*PartitionJobController, error) {
	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, err
	}
	return &PartitionJobController{
		stepLen: 1000, // Default step length of 1000 offsets per job
		part:    controller,
		backoff: backoff,
		decoder: decoder,
	}, nil
}

func (l *PartitionJobController) HighestCommittedOffset(ctx context.Context) (int64, error) {
	return withBackoff(
		ctx,
		l.backoff,
		func() (int64, error) {
			return l.part.FetchLastCommittedOffset(ctx)
		},
	)
}

func (l *PartitionJobController) HighestPartitionOffset(ctx context.Context) (int64, error) {
	return withBackoff(
		ctx,
		l.backoff,
		func() (int64, error) {
			return l.part.FetchPartitionOffset(ctx, partition.KafkaEndOffset)
		},
	)
}

func (l *PartitionJobController) EarliestPartitionOffset(ctx context.Context) (int64, error) {
	return withBackoff(
		ctx,
		l.backoff,
		func() (int64, error) {
			return l.part.FetchPartitionOffset(ctx, partition.KafkaStartOffset)
		},
	)
}

func (l *PartitionJobController) Process(ctx context.Context, offsets Offsets, ch chan<- []AppendInput) (int64, error) {
	l.part.SetOffsetForConsumption(offsets.Min)

	var (
		lastOffset = offsets.Min - 1
		boff       = backoff.New(ctx, l.backoff)
		err        error
	)

	for boff.Ongoing() {
		var records []partition.Record
		records, err = l.part.Poll(ctx)
		if err != nil {
			boff.Wait()
			continue
		}

		if len(records) == 0 {
			// No more records available
			break
		}

		// Reset backoff on successful poll
		boff.Reset()

		converted := make([]AppendInput, 0, len(records))
		for _, record := range records {
			offset := records[len(records)-1].Offset
			if offset >= offsets.Max {
				break
			}
			lastOffset = offset

			stream, labels, err := l.decoder.Decode(record.Content)
			if err != nil {
				return 0, fmt.Errorf("failed to decode record: %w", err)
			}
			if len(stream.Entries) == 0 {
				continue
			}

			converted = append(converted, AppendInput{
				tenant:    record.TenantID,
				labels:    labels,
				labelsStr: stream.Labels,
				entries:   stream.Entries,
			})

			select {
			case ch <- converted:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
	}

	return lastOffset, err
}

// LoadJob(ctx) returns the next job by finding the most recent unconsumed offset in the partition
// Returns whether an applicable job exists, the job, and an error
func (l *PartitionJobController) LoadJob(ctx context.Context) (bool, Job, error) {
	// Read the most recent committed offset
	committedOffset, err := l.HighestCommittedOffset(ctx)
	if err != nil {
		return false, Job{}, err
	}

	earliestOffset, err := l.EarliestPartitionOffset(ctx)
	if err != nil {
		return false, Job{}, err
	}

	startOffset := committedOffset + 1
	if startOffset < earliestOffset {
		startOffset = earliestOffset
	}

	highestOffset, err := l.HighestPartitionOffset(ctx)
	if err != nil {
		return false, Job{}, err
	}
	if highestOffset == committedOffset {
		return false, Job{}, nil
	}

	// Create the job with the calculated offsets
	job := Job{
		Partition: l.part.Partition(),
		Offsets: Offsets{
			Min: startOffset,
			Max: min(startOffset+l.stepLen, highestOffset),
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
