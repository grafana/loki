package blockbuilder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/multierror"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

const (
	kafkaStartOffset = -2
	kafkaEndOffset   = -1
)

var defaultBackoffConfig = backoff.Config{
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: time.Second,
	MaxRetries: 0, // Retry forever (unless context is canceled / deadline exceeded).
}

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
	Process(context.Context, Offsets, chan<- []partition.Record) (int64, error)

	Close() error
}

type partitionReader struct {
	topic       string
	group       string
	partitionID int32

	metrics partition.ReaderMetrics
	logger  log.Logger
	client  *kgo.Client
	aClient *kadm.Client
	reg     prometheus.Registerer
}

// Fetches the desired offset in the partition itself, not the consumer group
// NB(owen-d): lifted from `pkg/kafka/partition/reader.go:Reader`
func (r *partitionReader) fetchPartitionOffset(ctx context.Context, position int64) (int64, error) {
	// Create a custom request to fetch the latest offset of a specific partition.
	// We manually create a request so that we can request the offset for a single partition
	// only, which is more performant than requesting the offsets for all partitions.
	partitionReq := kmsg.NewListOffsetsRequestTopicPartition()
	partitionReq.Partition = r.partitionID
	partitionReq.Timestamp = position

	topicReq := kmsg.NewListOffsetsRequestTopic()
	topicReq.Topic = r.topic
	topicReq.Partitions = []kmsg.ListOffsetsRequestTopicPartition{partitionReq}

	req := kmsg.NewPtrListOffsetsRequest()
	req.IsolationLevel = 0 // 0 means READ_UNCOMMITTED.
	req.Topics = []kmsg.ListOffsetsRequestTopic{topicReq}

	// Even if we share the same client, other in-flight requests are not canceled once this context is canceled
	// (or its deadline is exceeded). We've verified it with a unit test.
	resps := r.client.RequestSharded(ctx, req)

	// Since we issued a request for only 1 partition, we expect exactly 1 response.
	if expected := 1; len(resps) != 1 {
		return 0, fmt.Errorf("unexpected number of responses (expected: %d, got: %d)", expected, len(resps))
	}

	// Ensure no error occurred.
	res := resps[0]
	if res.Err != nil {
		return 0, res.Err
	}

	// Parse the response.
	listRes, ok := res.Resp.(*kmsg.ListOffsetsResponse)
	if !ok {
		return 0, errors.New("unexpected response type")
	}
	if expected, actual := 1, len(listRes.Topics); actual != expected {
		return 0, fmt.Errorf("unexpected number of topics in the response (expected: %d, got: %d)", expected, actual)
	}
	if expected, actual := r.topic, listRes.Topics[0].Topic; expected != actual {
		return 0, fmt.Errorf("unexpected topic in the response (expected: %s, got: %s)", expected, actual)
	}
	if expected, actual := 1, len(listRes.Topics[0].Partitions); actual != expected {
		return 0, fmt.Errorf("unexpected number of partitions in the response (expected: %d, got: %d)", expected, actual)
	}
	if expected, actual := r.partitionID, listRes.Topics[0].Partitions[0].Partition; actual != expected {
		return 0, fmt.Errorf("unexpected partition in the response (expected: %d, got: %d)", expected, actual)
	}
	if err := kerr.ErrorForCode(listRes.Topics[0].Partitions[0].ErrorCode); err != nil {
		return 0, err
	}

	return listRes.Topics[0].Partitions[0].Offset, nil
}

// Fetches the highest committe offset in the consumer group
// NB(owen-d): lifted from `pkg/kafka/partition/reader.go:Reader`
// TODO(owen-d): expose errors: the failure case of restarting at
// the beginning of a partition is costly and duplicates data
func (r *partitionReader) fetchLastCommittedOffset(ctx context.Context) int64 {
	// We manually create a request so that we can request the offset for a single partition
	// only, which is more performant than requesting the offsets for all partitions.
	req := kmsg.NewPtrOffsetFetchRequest()
	req.Topics = []kmsg.OffsetFetchRequestTopic{{Topic: r.topic, Partitions: []int32{r.partitionID}}}
	req.Group = r.group

	resps := r.client.RequestSharded(ctx, req)

	// Since we issued a request for only 1 partition, we expect exactly 1 response.
	if expected, actual := 1, len(resps); actual != expected {
		level.Error(r.logger).Log("msg", fmt.Sprintf("unexpected number of responses (expected: %d, got: %d)", expected, actual), "expected", expected, "actual", len(resps))
		return kafkaStartOffset
	}
	// Ensure no error occurred.
	res := resps[0]
	if res.Err != nil {
		level.Error(r.logger).Log("msg", "error fetching group offset for partition", "err", res.Err)
		return kafkaStartOffset
	}

	// Parse the response.
	fetchRes, ok := res.Resp.(*kmsg.OffsetFetchResponse)
	if !ok {
		level.Error(r.logger).Log("msg", "unexpected response type")
		return kafkaStartOffset
	}
	if expected, actual := 1, len(fetchRes.Groups); actual != expected {
		level.Error(r.logger).Log("msg", fmt.Sprintf("unexpected number of groups in the response (expected: %d, got: %d)", expected, actual))
		return kafkaStartOffset
	}
	if expected, actual := 1, len(fetchRes.Groups[0].Topics); actual != expected {
		level.Error(r.logger).Log("msg", fmt.Sprintf("unexpected number of topics in the response (expected: %d, got: %d)", expected, actual))
		return kafkaStartOffset
	}
	if expected, actual := r.topic, fetchRes.Groups[0].Topics[0].Topic; expected != actual {
		level.Error(r.logger).Log("msg", fmt.Sprintf("unexpected topic in the response (expected: %s, got: %s)", expected, actual))
		return kafkaStartOffset
	}
	if expected, actual := 1, len(fetchRes.Groups[0].Topics[0].Partitions); actual != expected {
		level.Error(r.logger).Log("msg", fmt.Sprintf("unexpected number of partitions in the response (expected: %d, got: %d)", expected, actual))
		return kafkaStartOffset
	}
	if expected, actual := r.partitionID, fetchRes.Groups[0].Topics[0].Partitions[0].Partition; actual != expected {
		level.Error(r.logger).Log("msg", fmt.Sprintf("unexpected partition in the response (expected: %d, got: %d)", expected, actual))
		return kafkaStartOffset
	}
	if err := kerr.ErrorForCode(fetchRes.Groups[0].Topics[0].Partitions[0].ErrorCode); err != nil {
		level.Error(r.logger).Log("msg", "unexpected error in the response", "err", err)
		return kafkaStartOffset
	}

	return fetchRes.Groups[0].Topics[0].Partitions[0].Offset
}

func (r *partitionReader) updateReaderOffset(offset int64) {
	r.client.AddConsumePartitions(map[string]map[int32]kgo.Offset{
		r.topic: {r.partitionID: kgo.NewOffset().At(offset)},
	})
}

func (r *partitionReader) HighestCommittedOffset(ctx context.Context) (int64, error) {
	return withBackoff(
		ctx,
		defaultBackoffConfig,
		func() (int64, error) {
			return r.fetchLastCommittedOffset(ctx), nil
		},
	)
}

func (r *partitionReader) HighestPartitionOffset(ctx context.Context) (int64, error) {
	return withBackoff(
		ctx,
		defaultBackoffConfig,
		func() (int64, error) {
			return r.fetchPartitionOffset(ctx, kafkaEndOffset)
		},
	)
}

// pollFetches retrieves the next batch of records from Kafka and measures the fetch duration.
// NB(owen-d): lifted from `pkg/kafka/partition/reader.go:Reader`
func (p *partitionReader) poll(ctx context.Context) []partition.Record {
	defer func(start time.Time) {
		p.metrics.FetchWaitDuration.Observe(time.Since(start).Seconds())
	}(time.Now())
	fetches := p.client.PollFetches(ctx)
	p.recordFetchesMetrics(fetches)
	p.logFetchErrors(fetches)
	fetches = partition.FilterOutErrFetches(fetches)
	if fetches.NumRecords() == 0 {
		return nil
	}
	records := make([]partition.Record, 0, fetches.NumRecords())
	fetches.EachRecord(func(rec *kgo.Record) {
		if rec.Partition != p.partitionID {
			level.Error(p.logger).Log("msg", "wrong partition record received", "partition", rec.Partition, "expected_partition", p.partitionID)
			return
		}
		records = append(records, partition.Record{
			// This context carries the tracing data for this individual record;
			// kotel populates this data when it fetches the messages.
			Ctx:      rec.Context,
			TenantID: string(rec.Key),
			Content:  rec.Value,
			Offset:   rec.Offset,
		})
	})
	return records
}

// logFetchErrors logs any errors encountered during the fetch operation.
func (p *partitionReader) logFetchErrors(fetches kgo.Fetches) {
	mErr := multierror.New()
	fetches.EachError(func(topic string, partition int32, err error) {
		if errors.Is(err, context.Canceled) {
			return
		}

		// kgo advises to "restart" the kafka client if the returned error is a kerr.Error.
		// Recreating the client would cause duplicate metrics registration, so we don't do it for now.
		mErr.Add(fmt.Errorf("topic %q, partition %d: %w", topic, partition, err))
	})
	if len(mErr) == 0 {
		return
	}
	p.metrics.FetchesErrors.Add(float64(len(mErr)))
	level.Error(p.logger).Log("msg", "encountered error while fetching", "err", mErr.Err())
}

// recordFetchesMetrics updates various metrics related to the fetch operation.
// NB(owen-d): lifted from `pkg/kafka/partition/reader.go:Reader`
func (p *partitionReader) recordFetchesMetrics(fetches kgo.Fetches) {
	var (
		now        = time.Now()
		numRecords = 0
	)
	fetches.EachRecord(func(record *kgo.Record) {
		numRecords++
		delay := now.Sub(record.Timestamp).Seconds()
		p.metrics.ReceiveDelayWhenRunning.Observe(delay)
	})

	p.metrics.FetchesTotal.Add(float64(len(fetches)))
	p.metrics.RecordsPerFetch.Observe(float64(numRecords))
}

func (r *partitionReader) Process(ctx context.Context, offsets Offsets, ch chan<- []partition.Record) (int64, error) {
	r.updateReaderOffset(offsets.Min)

	var lastOffset int64 = offsets.Min - 1

	_, err := withBackoff(
		ctx,
		defaultBackoffConfig,
		func() (struct{}, error) {
			fetches := r.poll(ctx)
			if len(fetches) > 0 {
				lastOffset = fetches[len(fetches)-1].Offset
				select {
				case ch <- fetches:
				case <-ctx.Done():
				}
			}
			return struct{}{}, nil
		},
	)

	return lastOffset, err
}

func (r *partitionReader) Close() error {
	r.aClient.Close()
	r.client.Close()
	return nil
}

func withBackoff[T any](
	ctx context.Context,
	config backoff.Config,
	fn func() (T, error),
) (T, error) {
	var zero T

	var boff = backoff.New(ctx, config)
	for boff.Ongoing() {
		res, err := fn()
		if err != nil {
			boff.Wait()
			continue
		}
		return res, nil
	}

	return zero, boff.ErrCause()
}
