package sarama

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProducerTxnStatusFlag mark current transaction status.
type ProducerTxnStatusFlag int16

const (
	// ProducerTxnFlagUninitialized when txnmgr is created
	ProducerTxnFlagUninitialized ProducerTxnStatusFlag = 1 << iota
	// ProducerTxnFlagInitializing when txnmgr is initializing
	ProducerTxnFlagInitializing
	// ProducerTxnFlagReady when is ready to receive transaction
	ProducerTxnFlagReady
	// ProducerTxnFlagInTransaction when transaction is started
	ProducerTxnFlagInTransaction
	// ProducerTxnFlagEndTransaction when transaction will be committed
	ProducerTxnFlagEndTransaction
	// ProducerTxnFlagInError when having abortable or fatal error
	ProducerTxnFlagInError
	// ProducerTxnFlagCommittingTransaction when committing txn
	ProducerTxnFlagCommittingTransaction
	// ProducerTxnFlagAbortingTransaction when committing txn
	ProducerTxnFlagAbortingTransaction
	// ProducerTxnFlagAbortableError when producer encounter an abortable error
	// Must call AbortTxn in this case.
	ProducerTxnFlagAbortableError
	// ProducerTxnFlagFatalError when producer encounter an fatal error
	// Must Close an recreate it.
	ProducerTxnFlagFatalError
)

func (s ProducerTxnStatusFlag) String() string {
	status := make([]string, 0)
	if s&ProducerTxnFlagUninitialized != 0 {
		status = append(status, "ProducerTxnStateUninitialized")
	}
	if s&ProducerTxnFlagInitializing != 0 {
		status = append(status, "ProducerTxnStateInitializing")
	}
	if s&ProducerTxnFlagReady != 0 {
		status = append(status, "ProducerTxnStateReady")
	}
	if s&ProducerTxnFlagInTransaction != 0 {
		status = append(status, "ProducerTxnStateInTransaction")
	}
	if s&ProducerTxnFlagEndTransaction != 0 {
		status = append(status, "ProducerTxnStateEndTransaction")
	}
	if s&ProducerTxnFlagInError != 0 {
		status = append(status, "ProducerTxnStateInError")
	}
	if s&ProducerTxnFlagCommittingTransaction != 0 {
		status = append(status, "ProducerTxnStateCommittingTransaction")
	}
	if s&ProducerTxnFlagAbortingTransaction != 0 {
		status = append(status, "ProducerTxnStateAbortingTransaction")
	}
	if s&ProducerTxnFlagAbortableError != 0 {
		status = append(status, "ProducerTxnStateAbortableError")
	}
	if s&ProducerTxnFlagFatalError != 0 {
		status = append(status, "ProducerTxnStateFatalError")
	}
	return strings.Join(status, "|")
}

// transactionManager keeps the state necessary to ensure idempotent production
type transactionManager struct {
	producerID         int64
	producerEpoch      int16
	sequenceNumbers    map[string]int32
	mutex              sync.Mutex
	transactionalID    string
	transactionTimeout time.Duration
	client             Client

	// when kafka cluster is at least 2.5.0.
	// used to recover when producer failed.
	coordinatorSupportsBumpingEpoch bool

	// When producer need to bump it's epoch. Produce errors set it from
	// brokerProducer goroutines, so it is atomic.
	epochBumpRequired atomic.Bool
	// Record last seen error.
	lastError error

	// Ensure that status is never accessed with a race-condition.
	statusLock sync.RWMutex
	status     ProducerTxnStatusFlag

	// Ensure that only one goroutine will update partitions in current transaction.
	partitionInTxnLock            sync.Mutex
	pendingPartitionsInCurrentTxn topicPartitionSet
	partitionsInCurrentTxn        topicPartitionSet

	// Offsets to add to transaction, keyed by the group metadata they were
	// added with.
	offsetsInCurrentTxn map[groupMetadataKey]topicPartitionOffsets

	// Consumer group metadata whose offsets are added to the transaction.
	groupMetadataInCurrentTxn map[groupMetadataKey]*ConsumerGroupMetadata

	// AddOffsetsToTxn has added a group to the transaction on the coordinator,
	// so the transaction must end with EndTxn even with no records produced.
	offsetsAddedToTxn bool
}

// groupMetadataKey identifies the group member and generation that offsets
// were added for. TxnOffsetCommit sends each set with its own generation;
// sent with a later one, offsets for a partition a rebalance has since
// revoked would pass the coordinator's generation check.
type groupMetadataKey struct {
	groupID         string
	generationID    int32
	memberID        string
	groupInstanceID string
	hasInstanceID   bool
}

func newGroupMetadataKey(m *ConsumerGroupMetadata) groupMetadataKey {
	k := groupMetadataKey{groupID: m.GroupID, generationID: m.GenerationID, memberID: m.MemberID}
	if m.GroupInstanceID != nil {
		k.groupInstanceID, k.hasInstanceID = *m.GroupInstanceID, true
	}
	return k
}

const (
	noProducerID    = -1
	noProducerEpoch = -1

	// see publishTxnPartitions comment.
	addPartitionsRetryBackoff = 20 * time.Millisecond
)

// txnmngr allowed transitions.
var producerTxnTransitions = map[ProducerTxnStatusFlag][]ProducerTxnStatusFlag{
	ProducerTxnFlagUninitialized: {
		ProducerTxnFlagReady,
		ProducerTxnFlagInError,
	},
	// When we need are initializing
	ProducerTxnFlagInitializing: {
		ProducerTxnFlagInitializing,
		ProducerTxnFlagReady,
		ProducerTxnFlagInError,
	},
	// When we have initialized transactional producer
	ProducerTxnFlagReady: {
		ProducerTxnFlagInTransaction,
	},
	// When beginTxn has been called
	ProducerTxnFlagInTransaction: {
		// When calling commit or abort
		ProducerTxnFlagEndTransaction,
		// When got an error
		ProducerTxnFlagInError,
	},
	ProducerTxnFlagEndTransaction: {
		// When epoch bump
		ProducerTxnFlagInitializing,
		// When commit is good
		ProducerTxnFlagReady,
		// When got an error
		ProducerTxnFlagInError,
	},
	// Need to abort transaction
	ProducerTxnFlagAbortableError: {
		// Call AbortTxn
		ProducerTxnFlagAbortingTransaction,
		// When got an error
		ProducerTxnFlagInError,
	},
	// Need to close producer
	ProducerTxnFlagFatalError: {
		ProducerTxnFlagFatalError,
	},
}

type topicPartition struct {
	topic     string
	partition int32
}

// to ensure that we don't do a full scan every time a partition or an offset is added.
type (
	topicPartitionSet     map[topicPartition]struct{}
	topicPartitionOffsets map[topicPartition]*PartitionOffsetMetadata
)

func (s topicPartitionSet) mapToRequest() map[string][]int32 {
	result := make(map[string][]int32, len(s))
	for tp := range s {
		result[tp.topic] = append(result[tp.topic], tp.partition)
	}
	return result
}

func (s topicPartitionOffsets) mapToRequest() map[string][]*PartitionOffsetMetadata {
	result := make(map[string][]*PartitionOffsetMetadata, len(s))
	for tp, offset := range s {
		result[tp.topic] = append(result[tp.topic], offset)
	}
	return result
}

// Return true if current transition is allowed.
func (t *transactionManager) isTransitionValid(target ProducerTxnStatusFlag) bool {
	for status, allowedTransitions := range producerTxnTransitions {
		if status&t.status != 0 {
			for _, allowedTransition := range allowedTransitions {
				if allowedTransition&target != 0 {
					return true
				}
			}
		}
	}
	return false
}

// Get current transaction status.
func (t *transactionManager) currentTxnStatus() ProducerTxnStatusFlag {
	t.statusLock.RLock()
	defer t.statusLock.RUnlock()

	return t.status
}

// Try to transition to a valid status and return an error otherwise.
func (t *transactionManager) transitionTo(target ProducerTxnStatusFlag, err error) error {
	t.statusLock.Lock()
	defer t.statusLock.Unlock()

	if !t.isTransitionValid(target) {
		return ErrTransitionNotAllowed
	}

	if target&ProducerTxnFlagInError != 0 {
		if err == nil {
			return ErrCannotTransitionNilError
		}
		t.lastError = err
	} else {
		t.lastError = nil
	}

	DebugLogger.Printf("txnmgr/transition [%s] transition from %s to %s\n", t.transactionalID, t.status, target)

	t.status = target
	return err
}

// getAndAddSequenceNumbers reserves n sequence numbers for the partition and
// returns the first, with the producer id and epoch they belong to. All three
// come from one critical section, so an epoch bump cannot split a batch.
func (t *transactionManager) getAndAddSequenceNumbers(topic string, partition int32, n int32) (int64, int16, int32) {
	key := fmt.Sprintf("%s-%d", topic, partition)
	t.mutex.Lock()
	defer t.mutex.Unlock()
	first := t.sequenceNumbers[key]
	t.sequenceNumbers[key] = first + n
	return t.producerID, t.producerEpoch, first
}

func (t *transactionManager) bumpEpoch() {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.producerEpoch++
	for k := range t.sequenceNumbers {
		t.sequenceNumbers[k] = 0
	}
}

func (t *transactionManager) getProducerID() (int64, int16) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.producerID, t.producerEpoch
}

// Compute retry backoff considered current attempts.
func (t *transactionManager) computeBackoff(attemptsRemaining int) time.Duration {
	if t.client.Config().Producer.Transaction.Retry.BackoffFunc != nil {
		maxRetries := t.client.Config().Producer.Transaction.Retry.Max
		retries := maxRetries - attemptsRemaining
		return t.client.Config().Producer.Transaction.Retry.BackoffFunc(retries, maxRetries)
	}
	return t.client.Config().Producer.Transaction.Retry.Backoff
}

// return true is txnmngr is transactinal.
func (t *transactionManager) isTransactional() bool {
	return t.transactionalID != ""
}

// add specified offsets to current transaction.
func (t *transactionManager) addOffsetsToTxn(offsetsToAdd map[string][]*PartitionOffsetMetadata, groupMetadata *ConsumerGroupMetadata) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if t.currentTxnStatus()&ProducerTxnFlagInTransaction == 0 {
		return ErrTransactionNotReady
	}

	if t.currentTxnStatus()&ProducerTxnFlagFatalError != 0 {
		return t.lastError
	}

	key := newGroupMetadataKey(groupMetadata)
	if _, ok := t.offsetsInCurrentTxn[key]; !ok {
		t.offsetsInCurrentTxn[key] = topicPartitionOffsets{}
	}
	t.groupMetadataInCurrentTxn[key] = groupMetadata

	for topic, offsets := range offsetsToAdd {
		for _, offset := range offsets {
			tp := topicPartition{topic: topic, partition: offset.Partition}
			// a newer offset for the partition replaces one added for the
			// same group under other metadata
			for k, other := range t.offsetsInCurrentTxn {
				if k != key && k.groupID == key.groupID {
					delete(other, tp)
					if len(other) == 0 {
						delete(t.offsetsInCurrentTxn, k)
						delete(t.groupMetadataInCurrentTxn, k)
					}
				}
			}
			t.offsetsInCurrentTxn[key][tp] = offset
		}
	}
	return nil
}

// send txnmgnr save offsets to transaction coordinator.
func (t *transactionManager) publishOffsetsToTxn(offsets topicPartitionOffsets, groupMetadata *ConsumerGroupMetadata) (topicPartitionOffsets, error) {
	groupId := groupMetadata.GroupID
	// First AddOffsetsToTxn
	attemptsRemaining := t.client.Config().Producer.Transaction.Retry.Max
	exec := func(run func() (bool, error), err error) error {
		for attemptsRemaining >= 0 {
			var retry bool
			retry, err = run()
			if !retry {
				return err
			}
			backoff := t.computeBackoff(attemptsRemaining)
			Logger.Printf("txnmgr/add-offset-to-txn [%s] retrying after %dms... (%d attempts remaining) (%s)\n",
				t.transactionalID, backoff/time.Millisecond, attemptsRemaining, err)
			time.Sleep(backoff)
			attemptsRemaining--
		}
		return err
	}
	lastError := exec(func() (bool, error) {
		coordinator, err := t.client.TransactionCoordinator(t.transactionalID)
		if err != nil {
			return true, err
		}
		request := &AddOffsetsToTxnRequest{
			TransactionalID: t.transactionalID,
			ProducerEpoch:   t.producerEpoch,
			ProducerID:      t.producerID,
			GroupID:         groupId,
		}
		if t.client.Config().Version.IsAtLeast(V2_8_0_0) {
			// Version 3 enables flexible versions.
			request.Version = 3
		} else if t.client.Config().Version.IsAtLeast(V2_7_0_0) {
			// Version 2 adds the support for new error code PRODUCER_FENCED.
			request.Version = 2
		} else if t.client.Config().Version.IsAtLeast(V2_0_0_0) {
			// Version 1 is the same as version 0.
			request.Version = 1
		}
		response, err := coordinator.AddOffsetsToTxn(request)
		if err != nil {
			// If an error occurred try to refresh current transaction coordinator.
			_ = coordinator.Close()
			_ = t.client.RefreshTransactionCoordinator(t.transactionalID)
			return true, err
		}
		if response == nil {
			// If no response is returned just retry.
			return true, ErrTxnUnableToParseResponse
		}
		if response.Err == ErrNoError {
			DebugLogger.Printf("txnmgr/add-offset-to-txn [%s] successful add-offset-to-txn with group %s %+v\n",
				t.transactionalID, groupId, response)
			// If no error, just exit.
			return false, nil
		}
		switch response.Err {
		case ErrConsumerCoordinatorNotAvailable:
			fallthrough
		case ErrNotCoordinatorForConsumer:
			_ = coordinator.Close()
			_ = t.client.RefreshTransactionCoordinator(t.transactionalID)
			fallthrough
		case ErrOffsetsLoadInProgress:
			fallthrough
		case ErrConcurrentTransactions:
			// Retry
		case ErrUnknownProducerID:
			return false, t.abortableErrorIfPossible(response.Err)
		case ErrInvalidProducerIDMapping:
			// fatal: the transactional id expired, and re-initializing could let an
			// instance that was already fenced commit again
			return false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, response.Err)
		case ErrGroupAuthorizationFailed:
			return false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagAbortableError, response.Err)
		default:
			if isRetriableTxnError(response.Err) {
				break
			}
			// Others are fatal
			return false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, response.Err)
		}
		return true, response.Err
	}, nil)

	if lastError != nil {
		return offsets, lastError
	}
	t.offsetsAddedToTxn = true

	resultOffsets := offsets
	// Then TxnOffsetCommit
	// note the result is not completed until the TxnOffsetCommit returns
	attemptsRemaining = t.client.Config().Producer.Transaction.Retry.Max
	execTxnOffsetCommit := func(run func() (topicPartitionOffsets, bool, error), err error) (topicPartitionOffsets, error) {
		var r topicPartitionOffsets
		for attemptsRemaining >= 0 {
			var retry bool
			r, retry, err = run()
			if !retry {
				return r, err
			}
			backoff := t.computeBackoff(attemptsRemaining)
			Logger.Printf("txnmgr/txn-offset-commit [%s] retrying after %dms... (%d attempts remaining) (%s)\n",
				t.transactionalID, backoff/time.Millisecond, attemptsRemaining, err)
			time.Sleep(backoff)
			attemptsRemaining--
		}
		return r, err
	}
	return execTxnOffsetCommit(func() (topicPartitionOffsets, bool, error) {
		consumerGroupCoordinator, err := t.client.Coordinator(groupId)
		if err != nil {
			return resultOffsets, true, err
		}
		request := &TxnOffsetCommitRequest{
			TransactionalID: t.transactionalID,
			ProducerEpoch:   t.producerEpoch,
			ProducerID:      t.producerID,
			GroupID:         groupId,
			Topics:          offsets.mapToRequest(),
		}
		if t.client.Config().Version.IsAtLeast(V2_5_0_0) {
			// Version 3 adds the member ID, group instance ID and generation ID,
			// which let the broker fence stale group members. Callers that don't
			// supply them get the protocol defaults (generation -1, empty member
			// ID, null instance ID) which tell the broker to skip that fencing.
			request.Version = 3
			request.GenerationID = groupMetadata.GenerationID
			request.MemberID = groupMetadata.MemberID
			request.GroupInstanceID = groupMetadata.GroupInstanceID
		} else if t.client.Config().Version.IsAtLeast(V2_1_0_0) {
			// Version 2 adds the committed leader epoch.
			request.Version = 2
		} else if t.client.Config().Version.IsAtLeast(V2_0_0_0) {
			// Version 1 is the same as version 0.
			request.Version = 1
		}
		responses, err := consumerGroupCoordinator.TxnOffsetCommit(request)
		if err != nil {
			_ = consumerGroupCoordinator.Close()
			_ = t.client.RefreshCoordinator(groupId)
			return resultOffsets, true, err
		}

		if responses == nil {
			return resultOffsets, true, ErrTxnUnableToParseResponse
		}

		var responseErrors []error
		failedTxn := topicPartitionOffsets{}
		for topic, partitionErrors := range responses.Topics {
			for _, partitionError := range partitionErrors {
				switch partitionError.Err {
				case ErrNoError:
					continue
				// If the topic is unknown or the coordinator is loading, retry with the current coordinator
				case ErrRequestTimedOut:
					fallthrough
				case ErrConsumerCoordinatorNotAvailable:
					fallthrough
				case ErrNotCoordinatorForConsumer:
					_ = consumerGroupCoordinator.Close()
					_ = t.client.RefreshCoordinator(groupId)
					fallthrough
				case ErrUnknownTopicOrPartition:
					fallthrough
				case ErrOffsetsLoadInProgress:
					// Do nothing just retry
				case ErrIllegalGeneration:
					fallthrough
				case ErrUnknownMemberId:
					fallthrough
				case ErrFencedInstancedId:
					fallthrough
				case ErrGroupAuthorizationFailed:
					return resultOffsets, false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagAbortableError, partitionError.Err)
				default:
					if isRetriableTxnError(partitionError.Err) {
						break
					}
					// Others are fatal
					return resultOffsets, false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, partitionError.Err)
				}
				tp := topicPartition{topic: topic, partition: partitionError.Partition}
				failedTxn[tp] = offsets[tp]
				responseErrors = append(responseErrors, partitionError.Err)
			}
		}

		resultOffsets = failedTxn

		if len(resultOffsets) == 0 {
			DebugLogger.Printf("txnmgr/txn-offset-commit [%s] successful txn-offset-commit with group %s\n",
				t.transactionalID, groupId)
			return resultOffsets, false, nil
		}
		return resultOffsets, true, Wrap(ErrTxnOffsetCommit, responseErrors...)
	}, nil)
}

func (t *transactionManager) initProducerId() (int64, int16, error) {
	isEpochBump := false

	req := &InitProducerIDRequest{}
	if t.isTransactional() {
		req.TransactionalID = &t.transactionalID
		req.TransactionTimeout = t.transactionTimeout
	}

	if t.client.Config().Version.IsAtLeast(V2_5_0_0) {
		if t.client.Config().Version.IsAtLeast(V2_7_0_0) {
			// Version 4 adds the support for new error code PRODUCER_FENCED.
			req.Version = 4
		} else {
			// Version 3 adds ProducerId and ProducerEpoch, allowing producers to try
			// to resume after an INVALID_PRODUCER_EPOCH error
			req.Version = 3
		}
		isEpochBump = t.producerID != noProducerID && t.producerEpoch != noProducerEpoch
		t.coordinatorSupportsBumpingEpoch = true
		req.ProducerID = t.producerID
		req.ProducerEpoch = t.producerEpoch
	} else if t.client.Config().Version.IsAtLeast(V2_4_0_0) {
		// Version 2 is the first flexible version.
		req.Version = 2
	} else if t.client.Config().Version.IsAtLeast(V2_0_0_0) {
		// Version 1 is the same as version 0.
		req.Version = 1
	}

	if isEpochBump {
		err := t.transitionTo(ProducerTxnFlagInitializing, nil)
		if err != nil {
			return -1, -1, err
		}
		DebugLogger.Printf("txnmgr/init-producer-id [%s] invoking InitProducerId with current producer ID %d and epoch %d in order to bump the epoch\n",
			t.transactionalID, t.producerID, t.producerEpoch)
	} else {
		DebugLogger.Printf("txnmgr/init-producer-id [%s] invoking InitProducerId for the first time in order to acquire a producer ID\n",
			t.transactionalID)
	}

	attemptsRemaining := t.client.Config().Producer.Transaction.Retry.Max
	exec := func(run func() (int64, int16, bool, error), err error) (int64, int16, error) {
		pid := int64(-1)
		pepoch := int16(-1)
		for attemptsRemaining >= 0 {
			var retry bool
			pid, pepoch, retry, err = run()
			if !retry {
				return pid, pepoch, err
			}
			backoff := t.computeBackoff(attemptsRemaining)
			Logger.Printf("txnmgr/init-producer-id [%s] retrying after %dms... (%d attempts remaining) (%s)\n",
				t.transactionalID, backoff/time.Millisecond, attemptsRemaining, err)
			time.Sleep(backoff)
			attemptsRemaining--
		}
		return -1, -1, err
	}
	return exec(func() (int64, int16, bool, error) {
		var err error
		var coordinator *Broker
		if t.isTransactional() {
			coordinator, err = t.client.TransactionCoordinator(t.transactionalID)
		} else if coordinator = t.client.LeastLoadedBroker(); coordinator == nil {
			err = ErrOutOfBrokers
		}
		if err != nil {
			return -1, -1, true, err
		}
		response, err := coordinator.InitProducerID(req)
		if err != nil {
			if t.isTransactional() {
				_ = coordinator.Close()
				_ = t.client.RefreshTransactionCoordinator(t.transactionalID)
			}
			return -1, -1, true, err
		}
		if response == nil {
			return -1, -1, true, ErrTxnUnableToParseResponse
		}
		if response.Err == ErrNoError {
			if isEpochBump {
				t.sequenceNumbers = make(map[string]int32)
			}
			err := t.transitionTo(ProducerTxnFlagReady, nil)
			if err != nil {
				return -1, -1, true, err
			}
			DebugLogger.Printf("txnmgr/init-producer-id [%s] successful init producer id %+v\n",
				t.transactionalID, response)
			return response.ProducerID, response.ProducerEpoch, false, nil
		}
		switch response.Err {
		// Retriable errors
		case ErrConsumerCoordinatorNotAvailable, ErrNotCoordinatorForConsumer, ErrOffsetsLoadInProgress:
			if t.isTransactional() {
				_ = coordinator.Close()
				_ = t.client.RefreshTransactionCoordinator(t.transactionalID)
			}
		case ErrConcurrentTransactions:
			// Retry: the coordinator is still finishing the previous transaction.
		// Fatal errors
		default:
			if isRetriableTxnError(response.Err) {
				break
			}
			return -1, -1, false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, response.Err)
		}
		return -1, -1, true, response.Err
	}, nil)
}

// isRetriableTxnError reports whether err maps to a RetriableException in the
// Java client, whose transaction manager resends any transaction request that
// fails with one.
func isRetriableTxnError(err KError) bool {
	switch err {
	case ErrInvalidMessage, ErrUnknownTopicOrPartition, ErrLeaderNotAvailable,
		ErrNotLeaderForPartition, ErrRequestTimedOut, ErrReplicaNotAvailable,
		ErrNetworkException, ErrOffsetsLoadInProgress, ErrConsumerCoordinatorNotAvailable,
		ErrNotCoordinatorForConsumer, ErrNotEnoughReplicas, ErrNotEnoughReplicasAfterAppend,
		ErrNotController, ErrConcurrentTransactions, ErrKafkaStorageError,
		ErrFetchSessionIDNotFound, ErrInvalidFetchSessionEpoch, ErrListenerNotFound,
		ErrFencedLeaderEpoch, ErrUnknownLeaderEpoch, ErrOffsetNotAvailable,
		ErrPreferredLeaderNotAvailable, ErrEligibleLeadersNotAvailable, ErrElectionNotNeeded,
		ErrUnstableOffsetCommit, ErrThrottlingQuotaExceeded:
		return true
	}
	return false
}

// if kafka cluster is at least 2.5.0 mark txnmngr to bump epoch else mark it as fatal.
func (t *transactionManager) abortableErrorIfPossible(err error) error {
	if t.coordinatorSupportsBumpingEpoch {
		t.epochBumpRequired.Store(true)
		return t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagAbortableError, err)
	}
	return t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, err)
}

// End current transaction.
func (t *transactionManager) completeTransaction() error {
	if t.epochBumpRequired.Load() {
		err := t.transitionTo(ProducerTxnFlagInitializing, nil)
		if err != nil {
			return err
		}
	} else {
		err := t.transitionTo(ProducerTxnFlagReady, nil)
		if err != nil {
			return err
		}
	}

	t.epochBumpRequired.Store(false)
	t.partitionsInCurrentTxn = topicPartitionSet{}
	t.pendingPartitionsInCurrentTxn = topicPartitionSet{}
	t.offsetsInCurrentTxn = map[groupMetadataKey]topicPartitionOffsets{}
	t.groupMetadataInCurrentTxn = map[groupMetadataKey]*ConsumerGroupMetadata{}
	t.offsetsAddedToTxn = false

	return nil
}

// send EndTxn request with commit flag. (true when committing false otherwise)
func (t *transactionManager) endTxn(commit bool) error {
	attemptsRemaining := t.client.Config().Producer.Transaction.Retry.Max
	exec := func(run func() (bool, error), err error) error {
		for attemptsRemaining >= 0 {
			var retry bool
			retry, err = run()
			if !retry {
				return err
			}
			backoff := t.computeBackoff(attemptsRemaining)
			Logger.Printf("txnmgr/endtxn [%s] retrying after %dms... (%d attempts remaining) (%s)\n",
				t.transactionalID, backoff/time.Millisecond, attemptsRemaining, err)
			time.Sleep(backoff)
			attemptsRemaining--
		}
		return err
	}
	return exec(func() (bool, error) {
		coordinator, err := t.client.TransactionCoordinator(t.transactionalID)
		if err != nil {
			return true, err
		}
		request := &EndTxnRequest{
			TransactionalID:   t.transactionalID,
			ProducerEpoch:     t.producerEpoch,
			ProducerID:        t.producerID,
			TransactionResult: commit,
		}
		if t.client.Config().Version.IsAtLeast(V2_8_0_0) {
			// Version 3 enables flexible versions.
			request.Version = 3
		} else if t.client.Config().Version.IsAtLeast(V2_7_0_0) {
			// Version 2 adds the support for new error code PRODUCER_FENCED.
			request.Version = 2
		} else if t.client.Config().Version.IsAtLeast(V2_0_0_0) {
			// Version 1 is the same as version 0.
			request.Version = 1
		}
		response, err := coordinator.EndTxn(request)
		if err != nil {
			// Always retry on network error
			_ = coordinator.Close()
			_ = t.client.RefreshTransactionCoordinator(t.transactionalID)
			return true, err
		}
		if response == nil {
			return true, ErrTxnUnableToParseResponse
		}
		if response.Err == ErrNoError {
			DebugLogger.Printf("txnmgr/endtxn [%s] successful to end txn %+v\n",
				t.transactionalID, response)
			return false, t.completeTransaction()
		}
		switch response.Err {
		// Need to refresh coordinator
		case ErrConsumerCoordinatorNotAvailable:
			fallthrough
		case ErrNotCoordinatorForConsumer:
			_ = coordinator.Close()
			_ = t.client.RefreshTransactionCoordinator(t.transactionalID)
			fallthrough
		case ErrOffsetsLoadInProgress:
			fallthrough
		case ErrConcurrentTransactions:
			// Just retry
		case ErrUnknownProducerID:
			return false, t.abortableErrorIfPossible(response.Err)
		case ErrInvalidProducerIDMapping:
			// fatal: the transactional id expired, and re-initializing could let an
			// instance that was already fenced commit again
			return false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, response.Err)
		// Fatal errors
		default:
			if isRetriableTxnError(response.Err) {
				break
			}
			return false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, response.Err)
		}
		return true, response.Err
	}, nil)
}

// We will try to publish associated offsets for each groups
// then send endtxn request to mark transaction as finished.
func (t *transactionManager) finishTransaction(commit bool) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	// Ensure no error when committing or aborting
	if commit && t.currentTxnStatus()&ProducerTxnFlagInError != 0 {
		return t.lastError
	} else if !commit && t.currentTxnStatus()&ProducerTxnFlagFatalError != 0 {
		return t.lastError
	}

	// if nothing has been added to the transaction on the brokers and a commit
	// has no offsets to add, don't do anything.
	if len(t.partitionsInCurrentTxn) == 0 && !t.offsetsAddedToTxn &&
		(!commit || len(t.offsetsInCurrentTxn) == 0) {
		// There is no EndTxn to send, but a required epoch bump must still
		// happen. Otherwise the producer stays in Initializing.
		epochBump := t.epochBumpRequired.Load()
		if err := t.completeTransaction(); err != nil || !epochBump {
			return err
		}
		return t.initializeTransactions()
	}

	epochBump := t.epochBumpRequired.Load()
	// If we're aborting the transaction, so there should be no need to add offsets.
	if commit && len(t.offsetsInCurrentTxn) > 0 {
		for key, offsets := range t.offsetsInCurrentTxn {
			newOffsets, err := t.publishOffsetsToTxn(offsets, t.groupMetadataInCurrentTxn[key])
			if err != nil {
				t.offsetsInCurrentTxn[key] = newOffsets
				return err
			}
			delete(t.offsetsInCurrentTxn, key)
			delete(t.groupMetadataInCurrentTxn, key)
		}
	}

	if t.currentTxnStatus()&ProducerTxnFlagFatalError != 0 {
		return t.lastError
	}

	if err := t.endTxn(commit); err != nil {
		return err
	}
	if !epochBump {
		return nil
	}
	// reset pid and epoch if needed.
	return t.initializeTransactions()
}

// called before sending any transactional record
// won't do anything if current topic-partition is already added to transaction.
func (t *transactionManager) maybeAddPartitionToCurrentTxn(topic string, partition int32) {
	if t.currentTxnStatus()&ProducerTxnFlagInError != 0 {
		return
	}

	tp := topicPartition{topic: topic, partition: partition}

	t.partitionInTxnLock.Lock()
	defer t.partitionInTxnLock.Unlock()
	if _, ok := t.partitionsInCurrentTxn[tp]; ok {
		// partition is already added
		return
	}

	t.pendingPartitionsInCurrentTxn[tp] = struct{}{}
}

// allPartitionsInTxn reports whether every partition in the set has been
// added to the current transaction.
func (t *transactionManager) allPartitionsInTxn(set *produceSet) bool {
	t.partitionInTxnLock.Lock()
	defer t.partitionInTxnLock.Unlock()

	return !set.anyPartition(func(topic string, partition int32, _ *partitionSet) bool {
		_, ok := t.partitionsInCurrentTxn[topicPartition{topic: topic, partition: partition}]
		return !ok
	})
}

// Makes a request to kafka to add a list of partitions to the current transaction.
func (t *transactionManager) publishTxnPartitions() error {
	t.partitionInTxnLock.Lock()
	defer t.partitionInTxnLock.Unlock()

	if t.currentTxnStatus()&ProducerTxnFlagInError != 0 {
		return t.lastError
	}

	if len(t.pendingPartitionsInCurrentTxn) == 0 {
		return nil
	}

	// Remove the partitions from the pending set regardless of the result. We use the presence
	// of partitions in the pending set to know when it is not safe to send batches. However, if
	// the partitions failed to be added and we enter an error state, we expect the batches to be
	// aborted anyway. In this case, we must be able to continue sending the batches which are in
	// retry for partitions that were successfully added.
	removeAllPartitionsOnFatalOrAbortedError := func() {
		t.pendingPartitionsInCurrentTxn = topicPartitionSet{}
	}

	// We only want to reduce the backoff when retrying the first AddPartition which errored out due to a
	// CONCURRENT_TRANSACTIONS error since this means that the previous transaction is still completing and
	// we don't want to wait too long before trying to start the new one.
	//
	// This is only a temporary fix, the long term solution is being tracked in
	// https://issues.apache.org/jira/browse/KAFKA-5482
	retryBackoff := t.client.Config().Producer.Transaction.Retry.Backoff
	computeBackoff := func(attemptsRemaining int) time.Duration {
		if t.client.Config().Producer.Transaction.Retry.BackoffFunc != nil {
			maxRetries := t.client.Config().Producer.Transaction.Retry.Max
			retries := maxRetries - attemptsRemaining
			return t.client.Config().Producer.Transaction.Retry.BackoffFunc(retries, maxRetries)
		}
		return retryBackoff
	}
	attemptsRemaining := t.client.Config().Producer.Transaction.Retry.Max

	exec := func(run func() (bool, error), err error) error {
		for attemptsRemaining >= 0 {
			var retry bool
			retry, err = run()
			if !retry {
				return err
			}
			backoff := computeBackoff(attemptsRemaining)
			Logger.Printf("txnmgr/add-partition-to-txn retrying after %dms... (%d attempts remaining) (%s)\n", backoff/time.Millisecond, attemptsRemaining, err)
			time.Sleep(backoff)
			attemptsRemaining--
		}
		return err
	}
	return exec(func() (bool, error) {
		coordinator, err := t.client.TransactionCoordinator(t.transactionalID)
		if err != nil {
			return true, err
		}
		request := &AddPartitionsToTxnRequest{
			TransactionalID: t.transactionalID,
			ProducerID:      t.producerID,
			ProducerEpoch:   t.producerEpoch,
			TopicPartitions: t.pendingPartitionsInCurrentTxn.mapToRequest(),
		}
		if t.client.Config().Version.IsAtLeast(V2_8_0_0) {
			// Version 3 is first flexible version
			request.Version = 3
		} else if t.client.Config().Version.IsAtLeast(V2_7_0_0) {
			// Version 2 adds the support for new error code PRODUCER_FENCED.
			request.Version = 2
		} else if t.client.Config().Version.IsAtLeast(V2_0_0_0) {
			// Version 1 is the same as version 0.
			request.Version = 1
		}
		addPartResponse, err := coordinator.AddPartitionsToTxn(request)
		if err != nil {
			_ = coordinator.Close()
			_ = t.client.RefreshTransactionCoordinator(t.transactionalID)
			return true, err
		}

		if addPartResponse == nil {
			return true, ErrTxnUnableToParseResponse
		}

		// remove from the list partitions that have been successfully updated
		var responseErrors []error
		for topic, results := range addPartResponse.Errors {
			for _, response := range results {
				tp := topicPartition{topic: topic, partition: response.Partition}
				switch response.Err {
				case ErrNoError:
					// Mark partition as added to transaction
					t.partitionsInCurrentTxn[tp] = struct{}{}
					delete(t.pendingPartitionsInCurrentTxn, tp)
					continue
				case ErrConsumerCoordinatorNotAvailable:
					fallthrough
				case ErrNotCoordinatorForConsumer:
					_ = coordinator.Close()
					_ = t.client.RefreshTransactionCoordinator(t.transactionalID)
					fallthrough
				case ErrUnknownTopicOrPartition:
					fallthrough
				case ErrOffsetsLoadInProgress:
					// Retry topicPartition
				case ErrConcurrentTransactions:
					if len(t.partitionsInCurrentTxn) == 0 && retryBackoff > addPartitionsRetryBackoff {
						retryBackoff = addPartitionsRetryBackoff
					}
				case ErrOperationNotAttempted:
					fallthrough
				case ErrTopicAuthorizationFailed:
					removeAllPartitionsOnFatalOrAbortedError()
					return false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagAbortableError, response.Err)
				case ErrUnknownProducerID:
					removeAllPartitionsOnFatalOrAbortedError()
					return false, t.abortableErrorIfPossible(response.Err)
				case ErrInvalidProducerIDMapping:
					// fatal: the transactional id expired, and re-initializing could let an
					// instance that was already fenced commit again
					removeAllPartitionsOnFatalOrAbortedError()
					return false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, response.Err)
				// Fatal errors
				default:
					if isRetriableTxnError(response.Err) {
						break
					}
					removeAllPartitionsOnFatalOrAbortedError()
					return false, t.transitionTo(ProducerTxnFlagInError|ProducerTxnFlagFatalError, response.Err)
				}
				responseErrors = append(responseErrors, response.Err)
			}
		}

		// handle end
		if len(t.pendingPartitionsInCurrentTxn) == 0 {
			DebugLogger.Printf("txnmgr/add-partition-to-txn [%s] successful to add partitions txn %+v\n",
				t.transactionalID, addPartResponse)
			return false, nil
		}
		return true, Wrap(ErrAddPartitionsToTxn, responseErrors...)
	}, nil)
}

// Build a new transaction manager sharing producer client.
func newTransactionManager(conf *Config, client Client) (*transactionManager, error) {
	txnmgr := &transactionManager{
		producerID:                    noProducerID,
		producerEpoch:                 noProducerEpoch,
		client:                        client,
		pendingPartitionsInCurrentTxn: topicPartitionSet{},
		partitionsInCurrentTxn:        topicPartitionSet{},
		offsetsInCurrentTxn:           make(map[groupMetadataKey]topicPartitionOffsets),
		groupMetadataInCurrentTxn:     make(map[groupMetadataKey]*ConsumerGroupMetadata),
		status:                        ProducerTxnFlagUninitialized,
	}

	if conf.Producer.Idempotent {
		txnmgr.transactionalID = conf.Producer.Transaction.ID
		txnmgr.transactionTimeout = conf.Producer.Transaction.Timeout
		txnmgr.sequenceNumbers = make(map[string]int32)
		txnmgr.mutex = sync.Mutex{}

		var err error
		txnmgr.producerID, txnmgr.producerEpoch, err = txnmgr.initProducerId()
		if err != nil {
			return nil, err
		}
		Logger.Printf("txnmgr/init-producer-id [%s] obtained a ProducerId: %d and ProducerEpoch: %d\n",
			txnmgr.transactionalID, txnmgr.producerID, txnmgr.producerEpoch)
	}

	return txnmgr, nil
}

// re-init producer-id and producer-epoch if needed.
func (t *transactionManager) initializeTransactions() error {
	producerID, producerEpoch, err := t.initProducerId()
	if err != nil {
		// Keep the current producer id and epoch, and bump again on the next
		// commit or abort. Otherwise the producer can reach Ready with no
		// producer id.
		t.epochBumpRequired.Store(true)
		return err
	}
	t.producerID, t.producerEpoch = producerID, producerEpoch
	return nil
}
