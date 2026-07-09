package sarama

import (
	"errors"
	"sync"
)

// ListOffsetsOptions configures how offsets are fetched.
type ListOffsetsOptions struct {
	// IsolationLevel selects between ReadUncommitted (default) and ReadCommitted
	// when fetching the latest offset. Only honored by brokers running v0.11+.
	IsolationLevel IsolationLevel
}

// OffsetResult contains the response for a single topic partition.
type OffsetResult struct {
	Offset      int64
	Timestamp   int64
	LeaderEpoch int32
	Err         error
}

// OffsetAndMetadata describes the offset commit for a single partition.
type OffsetAndMetadata struct {
	Offset   int64
	Metadata string
	// LeaderEpoch contains the leader epoch of the last consumed record.
	// It is used by the broker to fence stale commits after leader changes.
	// Use the epoch returned by offset fetch/list APIs, or -1 to omit.
	LeaderEpoch int32
}

// AlterConsumerGroupOffsetsOptions configures how offsets are committed.
// It is currently empty and reserved for future Kafka protocol options
type AlterConsumerGroupOffsetsOptions struct{}

// ListOffsets fans out across the partition leaders to fetch offsets in parallel.
// Per-partition results may carry their own Err (e.g. NotLeaderForPartition,
// UnknownTopicOrPartition) when metadata is stale; the caller can refresh
// metadata via the underlying client and retry those partitions if needed. The
// retry loop here only covers transport-level failures.
func (ca *clusterAdmin) ListOffsets(partitions map[string]map[int32]int64, options *ListOffsetsOptions) (map[string]map[int32]*OffsetResult, error) {
	type topicPartition struct {
		topic     string
		partition int32
	}

	type brokerOffsetRequest struct {
		request    *OffsetRequest
		partitions []topicPartition
	}

	type brokerOffsetResult struct {
		result map[topicPartition]*OffsetResult
		err    error
	}

	if len(partitions) == 0 {
		return nil, ConfigurationError("no partitions provided")
	}

	if options == nil {
		options = &ListOffsetsOptions{}
	}

	allResults := make(map[string]map[int32]*OffsetResult, len(partitions))
	setResult := func(topic string, partition int32, result *OffsetResult) {
		if allResults[topic] == nil {
			allResults[topic] = make(map[int32]*OffsetResult)
		}
		allResults[topic][partition] = result
	}

	requests := make(map[*Broker]*brokerOffsetRequest)
	for topic, topicOffsets := range partitions {
		for partition, offsetQuery := range topicOffsets {
			broker, _, err := ca.client.LeaderAndEpoch(topic, partition)
			if err != nil {
				setResult(topic, partition, &OffsetResult{Err: err})
				continue
			}

			req := requests[broker]
			if req == nil {
				req = &brokerOffsetRequest{
					request: NewOffsetRequest(ca.conf.Version),
				}
				req.request.IsolationLevel = options.IsolationLevel
				requests[broker] = req
			}
			req.request.AddBlock(topic, partition, offsetQuery, 1)
			req.partitions = append(req.partitions, topicPartition{topic: topic, partition: partition})
		}
	}

	if len(requests) == 0 {
		return allResults, nil
	}

	results := make(chan brokerOffsetResult, len(requests))
	var wg sync.WaitGroup

	for broker, req := range requests {
		wg.Go(func() {
			var resp *OffsetResponse
			err := ca.retryOnError(isRetriableBrokerError, func() error {
				var err error
				_ = broker.Open(ca.client.Config())
				resp, err = broker.GetAvailableOffsets(req.request)
				return err
			})
			if err != nil {
				results <- brokerOffsetResult{err: err}
				return
			}
			broker.handleThrottledResponse(resp)

			partitionResults := make(map[topicPartition]*OffsetResult, len(req.partitions))
			for _, tp := range req.partitions {
				block := resp.GetBlock(tp.topic, tp.partition)
				if block == nil {
					partitionResults[tp] = &OffsetResult{Err: ErrIncompleteResponse}
					continue
				}
				partitionResults[tp] = &OffsetResult{
					Offset:      block.Offset,
					Timestamp:   block.Timestamp,
					LeaderEpoch: block.LeaderEpoch,
					Err:         block.Err,
				}
			}

			results <- brokerOffsetResult{result: partitionResults}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var errs []error
	for res := range results {
		if res.err != nil {
			errs = append(errs, res.err)
		}
		for tp, info := range res.result {
			setResult(tp.topic, tp.partition, info)
		}
	}

	return allResults, errors.Join(errs...)
}

// AlterConsumerGroupOffsets retries on transport-level errors and on
// per-partition coordinator errors (NOT_COORDINATOR,
// COORDINATOR_NOT_AVAILABLE, EOF). Other per-partition errors
// (e.g. UNKNOWN_TOPIC_OR_PARTITION) are returned to the caller in
// OffsetCommitResponse.Errors without retry.
func (ca *clusterAdmin) AlterConsumerGroupOffsets(group string, offsets map[string]map[int32]OffsetAndMetadata, _ *AlterConsumerGroupOffsetsOptions) (*OffsetCommitResponse, error) {
	if len(offsets) == 0 {
		return nil, ConfigurationError("no offsets provided")
	}

	var response *OffsetCommitResponse
	request := NewOffsetCommitRequest(ca.conf, group)

	var commitTimestamp int64
	if request.Version == 1 {
		commitTimestamp = ReceiveTime
	}

	for topic, topicOffsets := range offsets {
		for partition, offset := range topicOffsets {
			request.AddBlockWithLeaderEpoch(topic, partition, offset.Offset, offset.LeaderEpoch, commitTimestamp, offset.Metadata)
		}
	}

	err := ca.retryOnError(isRetriableGroupCoordinatorError, func() (err error) {
		defer func() {
			if err != nil && isRetriableGroupCoordinatorError(err) {
				_ = ca.client.RefreshCoordinator(group)
			}
		}()

		coordinator, err := ca.client.Coordinator(group)
		if err != nil {
			return err
		}

		response, err = coordinator.CommitOffset(request)
		if err != nil {
			return err
		}

		for _, topicErrors := range response.Errors {
			for _, partErr := range topicErrors {
				if isRetriableGroupCoordinatorError(partErr) {
					return partErr
				}
			}
		}

		return nil
	})

	return response, err
}
