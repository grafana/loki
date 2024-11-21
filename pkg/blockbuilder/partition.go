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

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

const (
	kafkaStartOffset = -2
	kafkaEndOffset   = -1
)

type partitionReader struct {
	topic       string
	group       string
	partitionID int32
	decoder     *kafka.Decoder
	backoff     backoff.Config

	readerMetrics *partition.ReaderMetrics
	writerMetrics *partition.CommitterMetrics
	logger        log.Logger
	client        *kgo.Client
	aClient       *kadm.Client
}

func NewPartitionReader(
	kafkaCfg kafka.Config,
	backoff backoff.Config,
	partitionID int32,
	instanceID string,
	logger log.Logger,
	r prometheus.Registerer,
) (*partitionReader, error) {
	readerMetrics := partition.NewReaderMetrics(r)
	writerMetrics := partition.NewCommitterMetrics(r, partitionID)
	group := kafkaCfg.GetConsumerGroup(instanceID, partitionID)

	logger = log.With(logger, "component", "partition_reader")
	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, err
	}

	client, err := client.NewReaderClient(kafkaCfg, readerMetrics.Kprom, logger)
	if err != nil {
		return nil, err
	}

	aClient := kadm.NewClient(client)

	return &partitionReader{
		topic:         kafkaCfg.Topic,
		group:         group,
		partitionID:   partitionID,
		backoff:       backoff,
		readerMetrics: readerMetrics,
		writerMetrics: writerMetrics,
		logger: log.With(
			logger,
			"component", "partitionReader",
			"topic", kafkaCfg.Topic,
			"partition_id", partitionID,
			"group", group,
		),
		decoder: decoder,
		client:  client,
		aClient: aClient,
	}, nil
}

func (r *partitionReader) Topic() string    { return r.topic }
func (r *partitionReader) Partition() int32 { return r.partitionID }

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

	level.Debug(r.logger).Log(
		"msg", "fetched partition offset",
		"partition", r.partitionID,
		"position", position,
		"topic", r.topic,
		"err", res.Err,
		"offset", listRes.Topics[0].Partitions[0].Offset,
	)

	return listRes.Topics[0].Partitions[0].Offset, nil
}

// Fetches the highest committee offset in the consumer group
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

	position := fetchRes.Groups[0].Topics[0].Partitions[0].Offset

	level.Debug(r.logger).Log(
		"msg", "fetched last committed offset",
		"partition", r.partitionID,
		"position", position,
		"topic", r.topic,
		"group", r.group,
		"err", res.Err,
	)

	return position
}

func (r *partitionReader) updateReaderOffset(offset int64) {
	r.client.AddConsumePartitions(map[string]map[int32]kgo.Offset{
		r.topic: {r.partitionID: kgo.NewOffset().At(offset)},
	})
}

func (r *partitionReader) HighestCommittedOffset(ctx context.Context) (int64, error) {
	return withBackoff(
		ctx,
		r.backoff,
		func() (int64, error) {
			return r.fetchLastCommittedOffset(ctx), nil
		},
	)
}

func (r *partitionReader) HighestPartitionOffset(ctx context.Context) (int64, error) {
	return withBackoff(
		ctx,
		r.backoff,
		func() (int64, error) {
			return r.fetchPartitionOffset(ctx, kafkaEndOffset)
		},
	)
}

func (r *partitionReader) EarliestPartitionOffset(ctx context.Context) (int64, error) {
	return withBackoff(
		ctx,
		r.backoff,
		func() (int64, error) {
			return r.fetchPartitionOffset(ctx, kafkaStartOffset)
		},
	)
}

// pollFetches retrieves the next batch of records from Kafka and measures the fetch duration.
// NB(owen-d): originally lifted from `pkg/kafka/partition/reader.go:Reader`
func (r *partitionReader) poll(
	ctx context.Context,
	maxOffset int64, // exclusive
) ([]partition.Record, bool) {
	defer func(start time.Time) {
		r.readerMetrics.FetchWaitDuration.Observe(time.Since(start).Seconds())
	}(time.Now())
	fetches := r.client.PollFetches(ctx)
	r.recordFetchesMetrics(fetches)
	r.logFetchErrors(fetches)
	fetches = partition.FilterOutErrFetches(fetches)
	if fetches.NumRecords() == 0 {
		return nil, false
	}
	records := make([]partition.Record, 0, fetches.NumRecords())

	itr := fetches.RecordIter()
	for !itr.Done() {
		rec := itr.Next()
		if rec.Partition != r.partitionID {
			level.Error(r.logger).Log("msg", "wrong partition record received", "partition", rec.Partition, "expected_partition", r.partitionID)
			continue
		}

		if rec.Offset >= maxOffset {
			return records, true
		}

		records = append(records, partition.Record{
			// This context carries the tracing data for this individual record;
			// kotel populates this data when it fetches the messages.
			Ctx:      rec.Context,
			TenantID: string(rec.Key),
			Content:  rec.Value,
			Offset:   rec.Offset,
		})
	}

	return records, false
}

// logFetchErrors logs any errors encountered during the fetch operation.
func (r *partitionReader) logFetchErrors(fetches kgo.Fetches) {
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
	r.readerMetrics.FetchesErrors.Add(float64(len(mErr)))
	level.Error(r.logger).Log("msg", "encountered error while fetching", "err", mErr.Err())
}

// recordFetchesMetrics updates various metrics related to the fetch operation.
// NB(owen-d): lifted from `pkg/kafka/partition/reader.go:Reader`
func (r *partitionReader) recordFetchesMetrics(fetches kgo.Fetches) {
	var (
		now        = time.Now()
		numRecords = 0
	)
	fetches.EachRecord(func(record *kgo.Record) {
		numRecords++
		delay := now.Sub(record.Timestamp).Seconds()
		r.readerMetrics.ReceiveDelay.WithLabelValues(partition.PhaseRunning).Observe(delay)
	})

	r.readerMetrics.FetchesTotal.Add(float64(len(fetches)))
	r.readerMetrics.RecordsPerFetch.Observe(float64(numRecords))
}

func (r *partitionReader) Process(ctx context.Context, offsets Offsets, ch chan<- []AppendInput) (int64, error) {
	r.updateReaderOffset(offsets.Min)

	var (
		lastOffset = offsets.Min - 1
		boff       = backoff.New(ctx, r.backoff)
		err        error
	)

	for boff.Ongoing() {
		fetches, done := r.poll(ctx, offsets.Max)
		level.Debug(r.logger).Log(
			"msg", "polling kafka",
			"records", len(fetches),
			"done", done,
			"max_offset", offsets.Max,
		)
		if len(fetches) > 0 {
			lastOffset = fetches[len(fetches)-1].Offset
			converted := make([]AppendInput, 0, len(fetches))

			for _, fetch := range fetches {
				stream, labels, err := r.decoder.Decode(fetch.Content)
				if err != nil {
					return 0, fmt.Errorf("failed to decode record: %w", err)
				}
				if len(stream.Entries) == 0 {
					continue
				}

				converted = append(converted, AppendInput{
					tenant:    fetch.TenantID,
					labels:    labels,
					labelsStr: stream.Labels,
					entries:   stream.Entries,
				})
			}

			select {
			case ch <- converted:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}

		if done {
			break
		}
	}

	return lastOffset, err
}

func (r *partitionReader) Close() error {
	r.aClient.Close()
	r.client.Close()
	return nil
}

// Commits the offset to the consumer group.
func (r *partitionReader) Commit(ctx context.Context, offset int64) (err error) {
	startTime := time.Now()
	r.writerMetrics.CommitRequestsTotal.Inc()

	logger := log.With(
		r.logger,
		"offset", offset,
		"phase", "committer",
	)

	defer func() {
		r.writerMetrics.CommitRequestsLatency.Observe(time.Since(startTime).Seconds())

		if err != nil {
			level.Error(logger).Log("msg", "failed to commit last consumed offset to Kafka", "err", err, "offset", offset)
			r.writerMetrics.CommitFailuresTotal.Inc()
		}
	}()

	// Commit the last consumed offset.
	toCommit := kadm.Offsets{}
	toCommit.AddOffset(r.topic, r.partitionID, offset, -1)
	committed, err := r.aClient.CommitOffsets(ctx, r.group, toCommit)
	if err != nil {
		return err
	} else if !committed.Ok() {
		return committed.Error()
	}

	committedOffset, _ := committed.Lookup(r.topic, r.partitionID)
	level.Debug(logger).Log("msg", "last commit offset successfully committed to Kafka", "committed", committedOffset.At)
	r.writerMetrics.LastCommittedOffset.Set(float64(committedOffset.At))
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
