package sarama

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// TransactionClusterAdmin extends ClusterAdmin with the read-only KIP-664
// transaction-detection APIs: DescribeProducers, DescribeTransactions, and
// ListTransactions. Values returned by NewClusterAdmin and
// NewClusterAdminFromClient implement TransactionClusterAdmin.
type TransactionClusterAdmin interface {
	ClusterAdmin

	// DescribeProducers lists the active producers for the given topic
	// partitions, querying each partition's leader. Requires Kafka 2.8.0.0 or
	// higher.
	DescribeProducers(topicPartitions map[string][]int32) (map[string]map[int32]DescribeProducersResponsePartition, error)

	// DescribeTransactions returns the current state of the given transactional
	// ids, querying each transaction's coordinator. Requires Kafka 3.0.0.0 or
	// higher.
	DescribeTransactions(transactionalIDs []string) (map[string]TransactionState, error)

	// ListTransactions lists the transactions known to the cluster, optionally
	// filtered by state, producer id, or (Kafka 3.8.0.0+) minimum duration in
	// milliseconds. Requires Kafka 3.0.0.0 or higher. A durationFilterMs less
	// than 0 disables the duration filter.
	ListTransactions(stateFilters []string, producerIDFilters []int64, durationFilterMs int64) ([]ListTransactionsResponseTransactionState, error)
}

var _ TransactionClusterAdmin = (*clusterAdmin)(nil)

func (ca *clusterAdmin) DescribeProducers(topicPartitions map[string][]int32) (map[string]map[int32]DescribeProducersResponsePartition, error) {
	if len(topicPartitions) == 0 {
		return nil, nil
	}

	type topicPartition struct {
		topic     string
		partition int32
	}

	// Group the requested partitions by their leader broker so a single request
	// is sent to each leader.
	partitionsPerBroker := make(map[*Broker][]topicPartition)
	var errs []error
	for topic, partitions := range topicPartitions {
		for _, partition := range partitions {
			leader, err := ca.client.Leader(topic, partition)
			if err != nil {
				errs = append(errs, fmt.Errorf("describe producers: find leader for partition %d of topic %s: %w", partition, topic, err))
				continue
			}
			partitionsPerBroker[leader] = append(partitionsPerBroker[leader], topicPartition{topic, partition})
		}
	}

	// Query each leader in parallel and merge the results.
	type queryResult struct {
		topics []DescribeProducersResponseTopic
		err    error
	}
	results := make(chan queryResult)

	var wg sync.WaitGroup
	for broker, topicPartitions := range partitionsPerBroker {
		wg.Go(func() {
			partitionsPerTopic := make(map[string][]int32)
			for _, tp := range topicPartitions {
				partitionsPerTopic[tp.topic] = append(partitionsPerTopic[tp.topic], tp.partition)
			}

			request := &DescribeProducersRequest{}
			for topic, partitions := range partitionsPerTopic {
				request.Topics = append(request.Topics, DescribeProducersRequestTopic{
					Name:             topic,
					PartitionIndexes: partitions,
				})
			}

			response, err := broker.DescribeProducers(request)
			if err != nil {
				results <- queryResult{err: fmt.Errorf("describe producers on broker %s: %w", broker.Addr(), err)}
				return
			}
			results <- queryResult{topics: response.Topics}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var result map[string]map[int32]DescribeProducersResponsePartition
	for r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		for _, topic := range r.topics {
			for _, partition := range topic.Partitions {
				if result == nil {
					result = make(map[string]map[int32]DescribeProducersResponsePartition)
				}
				if result[topic.Name] == nil {
					result[topic.Name] = make(map[int32]DescribeProducersResponsePartition)
				}
				result[topic.Name][partition.PartitionIndex] = partition
			}
		}
	}

	return result, errors.Join(errs...)
}

func (ca *clusterAdmin) DescribeTransactions(transactionalIDs []string) (map[string]TransactionState, error) {
	if len(transactionalIDs) == 0 {
		return nil, nil
	}

	// Group the transactional ids by their coordinating broker so a single
	// request is sent to each coordinator.
	idsPerCoordinator := make(map[*Broker][]string)
	var errs []error
	for _, id := range transactionalIDs {
		coordinator, err := ca.client.TransactionCoordinator(id)
		if err != nil {
			errs = append(errs, fmt.Errorf("describe transactions: find coordinator for transactional id %s: %w", id, err))
			continue
		}
		idsPerCoordinator[coordinator] = append(idsPerCoordinator[coordinator], id)
	}

	// Query each coordinator in parallel and merge the results.
	type queryResult struct {
		states []TransactionState
		err    error
	}
	results := make(chan queryResult)

	var wg sync.WaitGroup
	for _, ids := range idsPerCoordinator {
		wg.Go(func() {
			var states []TransactionState
			err := ca.retryOnError(isRetriableTransactionCoordinatorError, func() (err error) {
				defer func() {
					if err != nil && isRetriableTransactionCoordinatorError(err) {
						for _, id := range ids {
							_ = ca.client.RefreshTransactionCoordinator(id)
						}
					}
				}()

				perCoordinator := make(map[*Broker][]string)
				for _, id := range ids {
					coordinator, err := ca.client.TransactionCoordinator(id)
					if err != nil {
						return err
					}
					perCoordinator[coordinator] = append(perCoordinator[coordinator], id)
				}

				var attemptStates []TransactionState
				for coordinator, groupIDs := range perCoordinator {
					response, err := coordinator.DescribeTransactions(&DescribeTransactionsRequest{TransactionalIDs: groupIDs})
					if err != nil {
						return err
					}

					// A moved or loading coordinator answers successfully but tags the
					// affected transactions with a retriable coordinator code; lift it so
					// the coordinator is refreshed and the request retried.
					for _, state := range response.TransactionStates {
						if isRetriableTransactionCoordinatorError(state.ErrorCode) {
							return state.ErrorCode
						}
						attemptStates = append(attemptStates, state)
					}
				}

				states = attemptStates
				return nil
			})
			if err != nil {
				results <- queryResult{err: fmt.Errorf("describe transactions for %s: %w", strings.Join(ids, ", "), err)}
				return
			}
			results <- queryResult{states: states}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var result map[string]TransactionState
	for r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		for _, state := range r.states {
			if result == nil {
				result = make(map[string]TransactionState)
			}
			result[state.TransactionalID] = state
		}
	}

	return result, errors.Join(errs...)
}

// isRetriableTransactionCoordinatorError reports whether the given error is a
// transaction-coordinator error that refreshing the coordinator and retrying can
// resolve. It mirrors the set the transaction manager treats as retriable
// (COORDINATOR_NOT_AVAILABLE, NOT_COORDINATOR, COORDINATOR_LOAD_IN_PROGRESS) plus
// EOF for a dropped connection.
func isRetriableTransactionCoordinatorError(err error) bool {
	switch {
	case errors.Is(err, ErrConsumerCoordinatorNotAvailable):
		return true
	case errors.Is(err, ErrNotCoordinatorForConsumer):
		return true
	case errors.Is(err, ErrOffsetsLoadInProgress):
		return true
	case errors.Is(err, io.EOF):
		return true
	default:
		return false
	}
}

func (ca *clusterAdmin) ListTransactions(stateFilters []string, producerIDFilters []int64, durationFilterMs int64) ([]ListTransactionsResponseTransactionState, error) {
	// The DurationFilter field was added in ListTransactions v1 (Kafka 3.8.0.0).
	// Reject an explicit filter up front rather than silently ignoring it; a
	// negative value disables the filter and is safe against any broker.
	if durationFilterMs >= 0 && !ca.conf.Version.IsAtLeast(V3_8_0_0) {
		return nil, ConfigurationError("ListTransactions durationFilterMs requires Version >= V3_8_0_0")
	}

	// Transactions may be listed by any broker, so query all brokers in parallel
	// and merge the results.
	brokers := ca.client.Brokers()

	type queryResult struct {
		states []ListTransactionsResponseTransactionState
		err    error
	}
	results := make(chan queryResult)

	var wg sync.WaitGroup
	for _, b := range brokers {
		wg.Go(func() {
			_ = b.Open(ca.conf) // Ensure that broker is opened

			request := NewListTransactionsRequest(ca.conf.Version)
			request.StateFilters = stateFilters
			request.ProducerIDFilters = producerIDFilters
			request.DurationFilter = durationFilterMs

			response, err := b.ListTransactions(request)
			switch {
			case err != nil:
				results <- queryResult{err: fmt.Errorf("list transactions on broker %s: %w", b.Addr(), err)}
			case !errors.Is(response.ErrorCode, ErrNoError):
				results <- queryResult{err: fmt.Errorf("list transactions on broker %s: %w", b.Addr(), response.ErrorCode)}
			default:
				results <- queryResult{states: response.TransactionStates}
			}
		})
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var allTransactions []ListTransactionsResponseTransactionState
	var errs []error
	for r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		allTransactions = append(allTransactions, r.states...)
	}

	return allTransactions, errors.Join(errs...)
}
