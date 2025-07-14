package sarama

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
)

// ErrOutOfBrokers is the error returned when the client has run out of brokers to talk to because all of them errored
// or otherwise failed to respond.
var ErrOutOfBrokers = errors.New("kafka: client has run out of available brokers to talk to")

// ErrBrokerNotFound is the error returned when there's no broker found for the requested ID.
var ErrBrokerNotFound = errors.New("kafka: broker for ID is not found")

// ErrClosedClient is the error returned when a method is called on a client that has been closed.
var ErrClosedClient = errors.New("kafka: tried to use a client that was closed")

// ErrIncompleteResponse is the error returned when the server returns a syntactically valid response, but it does
// not contain the expected information.
var ErrIncompleteResponse = errors.New("kafka: response did not contain all the expected topic/partition blocks")

// ErrInvalidPartition is the error returned when a partitioner returns an invalid partition index
// (meaning one outside of the range [0...numPartitions-1]).
var ErrInvalidPartition = errors.New("kafka: partitioner returned an invalid partition index")

// ErrAlreadyConnected is the error returned when calling Open() on a Broker that is already connected or connecting.
var ErrAlreadyConnected = errors.New("kafka: broker connection already initiated")

// ErrNotConnected is the error returned when trying to send or call Close() on a Broker that is not connected.
var ErrNotConnected = errors.New("kafka: broker not connected")

// ErrInsufficientData is returned when decoding and the packet is truncated. This can be expected
// when requesting messages, since as an optimization the server is allowed to return a partial message at the end
// of the message set.
var ErrInsufficientData = errors.New("kafka: insufficient data to decode packet, more bytes expected")

// ErrShuttingDown is returned when a producer receives a message during shutdown.
var ErrShuttingDown = errors.New("kafka: message received by producer in process of shutting down")

// ErrMessageTooLarge is returned when the next message to consume is larger than the configured Consumer.Fetch.Max
var ErrMessageTooLarge = errors.New("kafka: message is larger than Consumer.Fetch.Max")

// ErrConsumerOffsetNotAdvanced is returned when a partition consumer didn't advance its offset after parsing
// a RecordBatch.
var ErrConsumerOffsetNotAdvanced = errors.New("kafka: consumer offset was not advanced after a RecordBatch")

// ErrControllerNotAvailable is returned when server didn't give correct controller id. May be kafka server's version
// is lower than 0.10.0.0.
var ErrControllerNotAvailable = errors.New("kafka: controller is not available")

// ErrNoTopicsToUpdateMetadata is returned when Meta.Full is set to false but no specific topics were found to update
// the metadata.
var ErrNoTopicsToUpdateMetadata = errors.New("kafka: no specific topics to update metadata")

// ErrUnknownScramMechanism is returned when user tries to AlterUserScramCredentials with unknown SCRAM mechanism
var ErrUnknownScramMechanism = errors.New("kafka: unknown SCRAM mechanism provided")

// ErrReassignPartitions is returned when altering partition assignments for a topic fails
var ErrReassignPartitions = errors.New("failed to reassign partitions for topic")

// ErrDeleteRecords is the type of error returned when fail to delete the required records
var ErrDeleteRecords = errors.New("kafka server: failed to delete records")

// ErrCreateACLs is the type of error returned when ACL creation failed
var ErrCreateACLs = errors.New("kafka server: failed to create one or more ACL rules")

// ErrAddPartitionsToTxn is returned when AddPartitionsToTxn failed multiple times
var ErrAddPartitionsToTxn = errors.New("transaction manager: failed to send partitions to transaction")

// ErrTxnOffsetCommit is returned when TxnOffsetCommit failed multiple times
var ErrTxnOffsetCommit = errors.New("transaction manager: failed to send offsets to transaction")

// ErrTransactionNotReady when transaction status is invalid for the current action.
var ErrTransactionNotReady = errors.New("transaction manager: transaction is not ready")

// ErrNonTransactedProducer when calling BeginTxn, CommitTxn or AbortTxn on a non transactional producer.
var ErrNonTransactedProducer = errors.New("transaction manager: you need to add TransactionalID to producer")

// ErrTransitionNotAllowed when txnmgr state transition is not valid.
var ErrTransitionNotAllowed = errors.New("transaction manager: invalid transition attempted")

// ErrCannotTransitionNilError when transition is attempted with an nil error.
var ErrCannotTransitionNilError = errors.New("transaction manager: cannot transition with a nil error")

// ErrTxnUnableToParseResponse when response is nil
var ErrTxnUnableToParseResponse = errors.New("transaction manager: unable to parse response")

// MultiErrorFormat specifies the formatter applied to format multierrors. The
// default implementation is a condensed version of the hashicorp/go-multierror
// default one
var MultiErrorFormat multierror.ErrorFormatFunc = func(es []error) string {
	if len(es) == 1 {
		return es[0].Error()
	}

	points := make([]string, len(es))
	for i, err := range es {
		points[i] = fmt.Sprintf("* %s", err)
	}

	return fmt.Sprintf(
		"%d errors occurred:\n\t%s\n",
		len(es), strings.Join(points, "\n\t"))
}

type sentinelError struct {
	sentinel error
	wrapped  error
}

func (err sentinelError) Error() string {
	if err.wrapped != nil {
		return fmt.Sprintf("%s: %v", err.sentinel, err.wrapped)
	} else {
		return fmt.Sprintf("%s", err.sentinel)
	}
}

func (err sentinelError) Is(target error) bool {
	return errors.Is(err.sentinel, target) || errors.Is(err.wrapped, target)
}

func (err sentinelError) Unwrap() error {
	return err.wrapped
}

func Wrap(sentinel error, wrapped ...error) sentinelError {
	return sentinelError{sentinel: sentinel, wrapped: multiError(wrapped...)}
}

func multiError(wrapped ...error) error {
	merr := multierror.Append(nil, wrapped...)
	if MultiErrorFormat != nil {
		merr.ErrorFormat = MultiErrorFormat
	}
	return merr.ErrorOrNil()
}

// PacketEncodingError is returned from a failure while encoding a Kafka packet. This can happen, for example,
// if you try to encode a string over 2^15 characters in length, since Kafka's encoding rules do not permit that.
type PacketEncodingError struct {
	Info string
}

func (err PacketEncodingError) Error() string {
	return fmt.Sprintf("kafka: error encoding packet: %s", err.Info)
}

// PacketDecodingError is returned when there was an error (other than truncated data) decoding the Kafka broker's response.
// This can be a bad CRC or length field, or any other invalid value.
type PacketDecodingError struct {
	Info string
}

func (err PacketDecodingError) Error() string {
	return fmt.Sprintf("kafka: error decoding packet: %s", err.Info)
}

// ConfigurationError is the type of error returned from a constructor (e.g. NewClient, or NewConsumer)
// when the specified configuration is invalid.
type ConfigurationError string

func (err ConfigurationError) Error() string {
	return "kafka: invalid configuration (" + string(err) + ")"
}

// KError is the type of error that can be returned directly by the Kafka broker.
// See https://cwiki.apache.org/confluence/display/KAFKA/A+Guide+To+The+Kafka+Protocol#AGuideToTheKafkaProtocol-ErrorCodes
type KError int16

// Numeric error codes returned by the Kafka server.
const (
	ErrUnknown                            KError = -1 // Errors.UNKNOWN_SERVER_ERROR
	ErrNoError                            KError = 0  // Errors.NONE
	ErrOffsetOutOfRange                   KError = 1  // Errors.OFFSET_OUT_OF_RANGE
	ErrInvalidMessage                     KError = 2  // Errors.CORRUPT_MESSAGE
	ErrUnknownTopicOrPartition            KError = 3  // Errors.UNKNOWN_TOPIC_OR_PARTITION
	ErrInvalidMessageSize                 KError = 4  // Errors.INVALID_FETCH_SIZE
	ErrLeaderNotAvailable                 KError = 5  // Errors.LEADER_NOT_AVAILABLE
	ErrNotLeaderForPartition              KError = 6  // Errors.NOT_LEADER_OR_FOLLOWER
	ErrRequestTimedOut                    KError = 7  // Errors.REQUEST_TIMED_OUT
	ErrBrokerNotAvailable                 KError = 8  // Errors.BROKER_NOT_AVAILABLE
	ErrReplicaNotAvailable                KError = 9  // Errors.REPLICA_NOT_AVAILABLE
	ErrMessageSizeTooLarge                KError = 10 // Errors.MESSAGE_TOO_LARGE
	ErrStaleControllerEpochCode           KError = 11 // Errors.STALE_CONTROLLER_EPOCH
	ErrOffsetMetadataTooLarge             KError = 12 // Errors.OFFSET_METADATA_TOO_LARGE
	ErrNetworkException                   KError = 13 // Errors.NETWORK_EXCEPTION
	ErrOffsetsLoadInProgress              KError = 14 // Errors.COORDINATOR_LOAD_IN_PROGRESS
	ErrConsumerCoordinatorNotAvailable    KError = 15 // Errors.COORDINATOR_NOT_AVAILABLE
	ErrNotCoordinatorForConsumer          KError = 16 // Errors.NOT_COORDINATOR
	ErrInvalidTopic                       KError = 17 // Errors.INVALID_TOPIC_EXCEPTION
	ErrMessageSetSizeTooLarge             KError = 18 // Errors.RECORD_LIST_TOO_LARGE
	ErrNotEnoughReplicas                  KError = 19 // Errors.NOT_ENOUGH_REPLICAS
	ErrNotEnoughReplicasAfterAppend       KError = 20 // Errors.NOT_ENOUGH_REPLICAS_AFTER_APPEND
	ErrInvalidRequiredAcks                KError = 21 // Errors.INVALID_REQUIRED_ACKS
	ErrIllegalGeneration                  KError = 22 // Errors.ILLEGAL_GENERATION
	ErrInconsistentGroupProtocol          KError = 23 // Errors.INCONSISTENT_GROUP_PROTOCOL
	ErrInvalidGroupId                     KError = 24 // Errors.INVALID_GROUP_ID
	ErrUnknownMemberId                    KError = 25 // Errors.UNKNOWN_MEMBER_ID
	ErrInvalidSessionTimeout              KError = 26 // Errors.INVALID_SESSION_TIMEOUT
	ErrRebalanceInProgress                KError = 27 // Errors.REBALANCE_IN_PROGRESS
	ErrInvalidCommitOffsetSize            KError = 28 // Errors.INVALID_COMMIT_OFFSET_SIZE
	ErrTopicAuthorizationFailed           KError = 29 // Errors.TOPIC_AUTHORIZATION_FAILED
	ErrGroupAuthorizationFailed           KError = 30 // Errors.GROUP_AUTHORIZATION_FAILED
	ErrClusterAuthorizationFailed         KError = 31 // Errors.CLUSTER_AUTHORIZATION_FAILED
	ErrInvalidTimestamp                   KError = 32 // Errors.INVALID_TIMESTAMP
	ErrUnsupportedSASLMechanism           KError = 33 // Errors.UNSUPPORTED_SASL_MECHANISM
	ErrIllegalSASLState                   KError = 34 // Errors.ILLEGAL_SASL_STATE
	ErrUnsupportedVersion                 KError = 35 // Errors.UNSUPPORTED_VERSION
	ErrTopicAlreadyExists                 KError = 36 // Errors.TOPIC_ALREADY_EXISTS
	ErrInvalidPartitions                  KError = 37 // Errors.INVALID_PARTITIONS
	ErrInvalidReplicationFactor           KError = 38 // Errors.INVALID_REPLICATION_FACTOR
	ErrInvalidReplicaAssignment           KError = 39 // Errors.INVALID_REPLICA_ASSIGNMENT
	ErrInvalidConfig                      KError = 40 // Errors.INVALID_CONFIG
	ErrNotController                      KError = 41 // Errors.NOT_CONTROLLER
	ErrInvalidRequest                     KError = 42 // Errors.INVALID_REQUEST
	ErrUnsupportedForMessageFormat        KError = 43 // Errors.UNSUPPORTED_FOR_MESSAGE_FORMAT
	ErrPolicyViolation                    KError = 44 // Errors.POLICY_VIOLATION
	ErrOutOfOrderSequenceNumber           KError = 45 // Errors.OUT_OF_ORDER_SEQUENCE_NUMBER
	ErrDuplicateSequenceNumber            KError = 46 // Errors.DUPLICATE_SEQUENCE_NUMBER
	ErrInvalidProducerEpoch               KError = 47 // Errors.INVALID_PRODUCER_EPOCH
	ErrInvalidTxnState                    KError = 48 // Errors.INVALID_TXN_STATE
	ErrInvalidProducerIDMapping           KError = 49 // Errors.INVALID_PRODUCER_ID_MAPPING
	ErrInvalidTransactionTimeout          KError = 50 // Errors.INVALID_TRANSACTION_TIMEOUT
	ErrConcurrentTransactions             KError = 51 // Errors.CONCURRENT_TRANSACTIONS
	ErrTransactionCoordinatorFenced       KError = 52 // Errors.TRANSACTION_COORDINATOR_FENCED
	ErrTransactionalIDAuthorizationFailed KError = 53 // Errors.TRANSACTIONAL_ID_AUTHORIZATION_FAILED
	ErrSecurityDisabled                   KError = 54 // Errors.SECURITY_DISABLED
	ErrOperationNotAttempted              KError = 55 // Errors.OPERATION_NOT_ATTEMPTED
	ErrKafkaStorageError                  KError = 56 // Errors.KAFKA_STORAGE_ERROR
	ErrLogDirNotFound                     KError = 57 // Errors.LOG_DIR_NOT_FOUND
	ErrSASLAuthenticationFailed           KError = 58 // Errors.SASL_AUTHENTICATION_FAILED
	ErrUnknownProducerID                  KError = 59 // Errors.UNKNOWN_PRODUCER_ID
	ErrReassignmentInProgress             KError = 60 // Errors.REASSIGNMENT_IN_PROGRESS
	ErrDelegationTokenAuthDisabled        KError = 61 // Errors.DELEGATION_TOKEN_AUTH_DISABLED
	ErrDelegationTokenNotFound            KError = 62 // Errors.DELEGATION_TOKEN_NOT_FOUND
	ErrDelegationTokenOwnerMismatch       KError = 63 // Errors.DELEGATION_TOKEN_OWNER_MISMATCH
	ErrDelegationTokenRequestNotAllowed   KError = 64 // Errors.DELEGATION_TOKEN_REQUEST_NOT_ALLOWED
	ErrDelegationTokenAuthorizationFailed KError = 65 // Errors.DELEGATION_TOKEN_AUTHORIZATION_FAILED
	ErrDelegationTokenExpired             KError = 66 // Errors.DELEGATION_TOKEN_EXPIRED
	ErrInvalidPrincipalType               KError = 67 // Errors.INVALID_PRINCIPAL_TYPE
	ErrNonEmptyGroup                      KError = 68 // Errors.NON_EMPTY_GROUP
	ErrGroupIDNotFound                    KError = 69 // Errors.GROUP_ID_NOT_FOUND
	ErrFetchSessionIDNotFound             KError = 70 // Errors.FETCH_SESSION_ID_NOT_FOUND
	ErrInvalidFetchSessionEpoch           KError = 71 // Errors.INVALID_FETCH_SESSION_EPOCH
	ErrListenerNotFound                   KError = 72 // Errors.LISTENER_NOT_FOUND
	ErrTopicDeletionDisabled              KError = 73 // Errors.TOPIC_DELETION_DISABLED
	ErrFencedLeaderEpoch                  KError = 74 // Errors.FENCED_LEADER_EPOCH
	ErrUnknownLeaderEpoch                 KError = 75 // Errors.UNKNOWN_LEADER_EPOCH
	ErrUnsupportedCompressionType         KError = 76 // Errors.UNSUPPORTED_COMPRESSION_TYPE
	ErrStaleBrokerEpoch                   KError = 77 // Errors.STALE_BROKER_EPOCH
	ErrOffsetNotAvailable                 KError = 78 // Errors.OFFSET_NOT_AVAILABLE
	ErrMemberIdRequired                   KError = 79 // Errors.MEMBER_ID_REQUIRED
	ErrPreferredLeaderNotAvailable        KError = 80 // Errors.PREFERRED_LEADER_NOT_AVAILABLE
	ErrGroupMaxSizeReached                KError = 81 // Errors.GROUP_MAX_SIZE_REACHED
	ErrFencedInstancedId                  KError = 82 // Errors.FENCED_INSTANCE_ID
	ErrEligibleLeadersNotAvailable        KError = 83 // Errors.ELIGIBLE_LEADERS_NOT_AVAILABLE
	ErrElectionNotNeeded                  KError = 84 // Errors.ELECTION_NOT_NEEDED
	ErrNoReassignmentInProgress           KError = 85 // Errors.NO_REASSIGNMENT_IN_PROGRESS
	ErrGroupSubscribedToTopic             KError = 86 // Errors.GROUP_SUBSCRIBED_TO_TOPIC
	ErrInvalidRecord                      KError = 87 // Errors.INVALID_RECORD
	ErrUnstableOffsetCommit               KError = 88 // Errors.UNSTABLE_OFFSET_COMMIT
	ErrThrottlingQuotaExceeded            KError = 89 // Errors.THROTTLING_QUOTA_EXCEEDED
	ErrProducerFenced                     KError = 90 // Errors.PRODUCER_FENCED
)

func (err KError) Error() string {
	// Error messages stolen/adapted from
	// https://kafka.apache.org/protocol#protocol_error_codes
	switch err {
	case ErrNoError:
		return "kafka server: Not an error, why are you printing me?"
	case ErrUnknown:
		return "kafka server: Unexpected (unknown?) server error"
	case ErrOffsetOutOfRange:
		return "kafka server: The requested offset is outside the range of offsets maintained by the server for the given topic/partition"
	case ErrInvalidMessage:
		return "kafka server: Message contents does not match its CRC"
	case ErrUnknownTopicOrPartition:
		return "kafka server: Request was for a topic or partition that does not exist on this broker"
	case ErrInvalidMessageSize:
		return "kafka server: The message has a negative size"
	case ErrLeaderNotAvailable:
		return "kafka server: In the middle of a leadership election, there is currently no leader for this partition and hence it is unavailable for writes"
	case ErrNotLeaderForPartition:
		return "kafka server: Tried to send a message to a replica that is not the leader for some partition. Your metadata is out of date"
	case ErrRequestTimedOut:
		return "kafka server: Request exceeded the user-specified time limit in the request"
	case ErrBrokerNotAvailable:
		return "kafka server: Broker not available. Not a client facing error, we should never receive this!!!"
	case ErrReplicaNotAvailable:
		return "kafka server: Replica information not available, one or more brokers are down"
	case ErrMessageSizeTooLarge:
		return "kafka server: Message was too large, server rejected it to avoid allocation error"
	case ErrStaleControllerEpochCode:
		return "kafka server: StaleControllerEpochCode (internal error code for broker-to-broker communication)"
	case ErrOffsetMetadataTooLarge:
		return "kafka server: Specified a string larger than the configured maximum for offset metadata"
	case ErrNetworkException:
		return "kafka server: The server disconnected before a response was received"
	case ErrOffsetsLoadInProgress:
		return "kafka server: The coordinator is still loading offsets and cannot currently process requests"
	case ErrConsumerCoordinatorNotAvailable:
		return "kafka server: The coordinator is not available"
	case ErrNotCoordinatorForConsumer:
		return "kafka server: Request was for a consumer group that is not coordinated by this broker"
	case ErrInvalidTopic:
		return "kafka server: The request attempted to perform an operation on an invalid topic"
	case ErrMessageSetSizeTooLarge:
		return "kafka server: The request included message batch larger than the configured segment size on the server"
	case ErrNotEnoughReplicas:
		return "kafka server: Messages are rejected since there are fewer in-sync replicas than required"
	case ErrNotEnoughReplicasAfterAppend:
		return "kafka server: Messages are written to the log, but to fewer in-sync replicas than required"
	case ErrInvalidRequiredAcks:
		return "kafka server: The number of required acks is invalid (should be either -1, 0, or 1)"
	case ErrIllegalGeneration:
		return "kafka server: The provided generation id is not the current generation"
	case ErrInconsistentGroupProtocol:
		return "kafka server: The provider group protocol type is incompatible with the other members"
	case ErrInvalidGroupId:
		return "kafka server: The provided group id was empty"
	case ErrUnknownMemberId:
		return "kafka server: The provided member is not known in the current generation"
	case ErrInvalidSessionTimeout:
		return "kafka server: The provided session timeout is outside the allowed range"
	case ErrRebalanceInProgress:
		return "kafka server: A rebalance for the group is in progress. Please re-join the group"
	case ErrInvalidCommitOffsetSize:
		return "kafka server: The provided commit metadata was too large"
	case ErrTopicAuthorizationFailed:
		return "kafka server: The client is not authorized to access this topic"
	case ErrGroupAuthorizationFailed:
		return "kafka server: The client is not authorized to access this group"
	case ErrClusterAuthorizationFailed:
		return "kafka server: The client is not authorized to send this request type"
	case ErrInvalidTimestamp:
		return "kafka server: The timestamp of the message is out of acceptable range"
	case ErrUnsupportedSASLMechanism:
		return "kafka server: The broker does not support the requested SASL mechanism"
	case ErrIllegalSASLState:
		return "kafka server: Request is not valid given the current SASL state"
	case ErrUnsupportedVersion:
		return "kafka server: The version of API is not supported"
	case ErrTopicAlreadyExists:
		return "kafka server: Topic with this name already exists"
	case ErrInvalidPartitions:
		return "kafka server: Number of partitions is invalid"
	case ErrInvalidReplicationFactor:
		return "kafka server: Replication-factor is invalid"
	case ErrInvalidReplicaAssignment:
		return "kafka server: Replica assignment is invalid"
	case ErrInvalidConfig:
		return "kafka server: Configuration is invalid"
	case ErrNotController:
		return "kafka server: This is not the correct controller for this cluster"
	case ErrInvalidRequest:
		return "kafka server: This most likely occurs because of a request being malformed by the client library or the message was sent to an incompatible broker. See the broker logs for more details"
	case ErrUnsupportedForMessageFormat:
		return "kafka server: The requested operation is not supported by the message format version"
	case ErrPolicyViolation:
		return "kafka server: Request parameters do not satisfy the configured policy"
	case ErrOutOfOrderSequenceNumber:
		return "kafka server: The broker received an out of order sequence number"
	case ErrDuplicateSequenceNumber:
		return "kafka server: The broker received a duplicate sequence number"
	case ErrInvalidProducerEpoch:
		return "kafka server: Producer attempted an operation with an old epoch"
	case ErrInvalidTxnState:
		return "kafka server: The producer attempted a transactional operation in an invalid state"
	case ErrInvalidProducerIDMapping:
		return "kafka server: The producer attempted to use a producer id which is not currently assigned to its transactional id"
	case ErrInvalidTransactionTimeout:
		return "kafka server: The transaction timeout is larger than the maximum value allowed by the broker (as configured by max.transaction.timeout.ms)"
	case ErrConcurrentTransactions:
		return "kafka server: The producer attempted to update a transaction while another concurrent operation on the same transaction was ongoing"
	case ErrTransactionCoordinatorFenced:
		return "kafka server: The transaction coordinator sending a WriteTxnMarker is no longer the current coordinator for a given producer"
	case ErrTransactionalIDAuthorizationFailed:
		return "kafka server: Transactional ID authorization failed"
	case ErrSecurityDisabled:
		return "kafka server: Security features are disabled"
	case ErrOperationNotAttempted:
		return "kafka server: The broker did not attempt to execute this operation"
	case ErrKafkaStorageError:
		return "kafka server: Disk error when trying to access log file on the disk"
	case ErrLogDirNotFound:
		return "kafka server: The specified log directory is not found in the broker config"
	case ErrSASLAuthenticationFailed:
		return "kafka server: SASL Authentication failed"
	case ErrUnknownProducerID:
		return "kafka server: The broker could not locate the producer metadata associated with the Producer ID"
	case ErrReassignmentInProgress:
		return "kafka server: A partition reassignment is in progress"
	case ErrDelegationTokenAuthDisabled:
		return "kafka server: Delegation Token feature is not enabled"
	case ErrDelegationTokenNotFound:
		return "kafka server: Delegation Token is not found on server"
	case ErrDelegationTokenOwnerMismatch:
		return "kafka server: Specified Principal is not valid Owner/Renewer"
	case ErrDelegationTokenRequestNotAllowed:
		return "kafka server: Delegation Token requests are not allowed on PLAINTEXT/1-way SSL channels and on delegation token authenticated channels"
	case ErrDelegationTokenAuthorizationFailed:
		return "kafka server: Delegation Token authorization failed"
	case ErrDelegationTokenExpired:
		return "kafka server: Delegation Token is expired"
	case ErrInvalidPrincipalType:
		return "kafka server: Supplied principalType is not supported"
	case ErrNonEmptyGroup:
		return "kafka server: The group is not empty"
	case ErrGroupIDNotFound:
		return "kafka server: The group id does not exist"
	case ErrFetchSessionIDNotFound:
		return "kafka server: The fetch session ID was not found"
	case ErrInvalidFetchSessionEpoch:
		return "kafka server: The fetch session epoch is invalid"
	case ErrListenerNotFound:
		return "kafka server: There is no listener on the leader broker that matches the listener on which metadata request was processed"
	case ErrTopicDeletionDisabled:
		return "kafka server: Topic deletion is disabled"
	case ErrFencedLeaderEpoch:
		return "kafka server: The leader epoch in the request is older than the epoch on the broker"
	case ErrUnknownLeaderEpoch:
		return "kafka server: The leader epoch in the request is newer than the epoch on the broker"
	case ErrUnsupportedCompressionType:
		return "kafka server: The requesting client does not support the compression type of given partition"
	case ErrStaleBrokerEpoch:
		return "kafka server: Broker epoch has changed"
	case ErrOffsetNotAvailable:
		return "kafka server: The leader high watermark has not caught up from a recent leader election so the offsets cannot be guaranteed to be monotonically increasing"
	case ErrMemberIdRequired:
		return "kafka server: The group member needs to have a valid member id before actually entering a consumer group"
	case ErrPreferredLeaderNotAvailable:
		return "kafka server: The preferred leader was not available"
	case ErrGroupMaxSizeReached:
		return "kafka server: Consumer group The consumer group has reached its max size. already has the configured maximum number of members"
	case ErrFencedInstancedId:
		return "kafka server: The broker rejected this static consumer since another consumer with the same group.instance.id has registered with a different member.id"
	case ErrEligibleLeadersNotAvailable:
		return "kafka server: Eligible topic partition leaders are not available"
	case ErrElectionNotNeeded:
		return "kafka server: Leader election not needed for topic partition"
	case ErrNoReassignmentInProgress:
		return "kafka server: No partition reassignment is in progress"
	case ErrGroupSubscribedToTopic:
		return "kafka server: Deleting offsets of a topic is forbidden while the consumer group is actively subscribed to it"
	case ErrInvalidRecord:
		return "kafka server: This record has failed the validation on broker and hence will be rejected"
	case ErrUnstableOffsetCommit:
		return "kafka server: There are unstable offsets that need to be cleared"
	}

	return fmt.Sprintf("Unknown error, how did this happen? Error code = %d", err)
}
