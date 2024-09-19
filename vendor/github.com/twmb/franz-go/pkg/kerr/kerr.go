// Package kerr contains Kafka errors.
//
// The errors are undocumented to avoid duplicating the official descriptions
// that can be found at https://kafka.apache.org/protocol.html#protocol_error_codes (although,
// this code does duplicate the descriptions into the errors themselves, so the
// descriptions can be seen as the documentation).
//
// Since this package is dedicated to errors and the package is named "kerr",
// all errors elide the standard "Err" prefix.
package kerr

import (
	"errors"
	"fmt"
)

// Error is a Kafka error.
type Error struct {
	// Message is the string form of a Kafka error code
	// (UNKNOWN_SERVER_ERROR, etc).
	Message string
	// Code is a Kafka error code.
	Code int16
	// Retriable is whether the error is considered retriable by Kafka.
	Retriable bool
	// Description is a succinct description of what this error means.
	Description string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Message, e.Description)
}

// ErrorForCode returns the error corresponding to the given error code.
//
// If the code is unknown, this returns UnknownServerError.
// If the code is 0, this returns nil.
func ErrorForCode(code int16) error {
	err, exists := code2err[code]
	if !exists {
		return UnknownServerError
	}
	return err
}

// TypedErrorForCode returns the kerr.Error corresponding to the given error
// code.
//
// If the code is unknown, this returns UnknownServerError.
// If the code is 0, this returns nil.
//
// Note that this function is provided as a simplicity function for code that
// needs to work with the *Error only, but this function comes with caveats.
// Because this can return a typed nil, passing the return of this to a
// function that accepts an error (the Go error interface), the return from
// this will never be considered a nil error. Instead, it will be an error with
// a nil internal value.
func TypedErrorForCode(code int16) *Error {
	err, exists := code2err[code]
	if !exists {
		return UnknownServerError
	}
	if err == nil {
		return nil
	}
	return err.(*Error)
}

// IsRetriable returns whether a Kafka error is considered retriable.
func IsRetriable(err error) bool {
	var kerr *Error
	return errors.As(err, &kerr) && kerr.Retriable
}

var (
	UnknownServerError                 = &Error{"UNKNOWN_SERVER_ERROR", -1, false, "The server experienced an unexpected error when processing the request."}
	OffsetOutOfRange                   = &Error{"OFFSET_OUT_OF_RANGE", 1, false, "The requested offset is not within the range of offsets maintained by the server."}
	CorruptMessage                     = &Error{"CORRUPT_MESSAGE", 2, true, "This message has failed its CRC checksum, exceeds the valid size, has a null key for a compacted topic, or is otherwise corrupt."}
	UnknownTopicOrPartition            = &Error{"UNKNOWN_TOPIC_OR_PARTITION", 3, true, "This server does not host this topic-partition."}
	InvalidFetchSize                   = &Error{"INVALID_FETCH_SIZE", 4, false, "The requested fetch size is invalid."}
	LeaderNotAvailable                 = &Error{"LEADER_NOT_AVAILABLE", 5, true, "There is no leader for this topic-partition as we are in the middle of a leadership election."}
	NotLeaderForPartition              = &Error{"NOT_LEADER_FOR_PARTITION", 6, true, "This server is not the leader for that topic-partition."}
	RequestTimedOut                    = &Error{"REQUEST_TIMED_OUT", 7, true, "The request timed out."}
	BrokerNotAvailable                 = &Error{"BROKER_NOT_AVAILABLE", 8, true, "The broker is not available."}
	ReplicaNotAvailable                = &Error{"REPLICA_NOT_AVAILABLE", 9, true, "The replica is not available for the requested topic-partition."}
	MessageTooLarge                    = &Error{"MESSAGE_TOO_LARGE", 10, false, "The request included a message larger than the max message size the server will accept."}
	StaleControllerEpoch               = &Error{"STALE_CONTROLLER_EPOCH", 11, false, "The controller moved to another broker."}
	OffsetMetadataTooLarge             = &Error{"OFFSET_METADATA_TOO_LARGE", 12, false, "The metadata field of the offset request was too large."}
	NetworkException                   = &Error{"NETWORK_EXCEPTION", 13, true, "The server disconnected before a response was received."}
	CoordinatorLoadInProgress          = &Error{"COORDINATOR_LOAD_IN_PROGRESS", 14, true, "The coordinator is loading and hence can't process requests."}
	CoordinatorNotAvailable            = &Error{"COORDINATOR_NOT_AVAILABLE", 15, true, "The coordinator is not available."}
	NotCoordinator                     = &Error{"NOT_COORDINATOR", 16, true, "This is not the correct coordinator."}
	InvalidTopicException              = &Error{"INVALID_TOPIC_EXCEPTION", 17, false, "The request attempted to perform an operation on an invalid topic."}
	RecordListTooLarge                 = &Error{"RECORD_LIST_TOO_LARGE", 18, false, "The request included message batch larger than the configured segment size on the server."}
	NotEnoughReplicas                  = &Error{"NOT_ENOUGH_REPLICAS", 19, true, "Messages are rejected since there are fewer in-sync replicas than required."}
	NotEnoughReplicasAfterAppend       = &Error{"NOT_ENOUGH_REPLICAS_AFTER_APPEND", 20, true, "Messages are written to the log, but to fewer in-sync replicas than required."}
	InvalidRequiredAcks                = &Error{"INVALID_REQUIRED_ACKS", 21, false, "Produce request specified an invalid value for required acks."}
	IllegalGeneration                  = &Error{"ILLEGAL_GENERATION", 22, false, "Specified group generation id is not valid."}
	InconsistentGroupProtocol          = &Error{"INCONSISTENT_GROUP_PROTOCOL", 23, false, "The group member's supported protocols are incompatible with those of existing members or first group member tried to join with empty protocol type or empty protocol list."}
	InvalidGroupID                     = &Error{"INVALID_GROUP_ID", 24, false, "The configured groupID is invalid."}
	UnknownMemberID                    = &Error{"UNKNOWN_MEMBER_ID", 25, false, "The coordinator is not aware of this member."}
	InvalidSessionTimeout              = &Error{"INVALID_SESSION_TIMEOUT", 26, false, "The session timeout is not within the range allowed by the broker (as configured by group.min.session.timeout.ms and group.max.session.timeout.ms)."}
	RebalanceInProgress                = &Error{"REBALANCE_IN_PROGRESS", 27, false, "The group is rebalancing, so a rejoin is needed."}
	InvalidCommitOffsetSize            = &Error{"INVALID_COMMIT_OFFSET_SIZE", 28, false, "The committing offset data size is not valid."}
	TopicAuthorizationFailed           = &Error{"TOPIC_AUTHORIZATION_FAILED", 29, false, "Not authorized to access topics: [Topic authorization failed.]"}
	GroupAuthorizationFailed           = &Error{"GROUP_AUTHORIZATION_FAILED", 30, false, "Not authorized to access group: Group authorization failed."}
	ClusterAuthorizationFailed         = &Error{"CLUSTER_AUTHORIZATION_FAILED", 31, false, "Cluster authorization failed."}
	InvalidTimestamp                   = &Error{"INVALID_TIMESTAMP", 32, false, "The timestamp of the message is out of acceptable range."}
	UnsupportedSaslMechanism           = &Error{"UNSUPPORTED_SASL_MECHANISM", 33, false, "The broker does not support the requested SASL mechanism."}
	IllegalSaslState                   = &Error{"ILLEGAL_SASL_STATE", 34, false, "Request is not valid given the current SASL state."}
	UnsupportedVersion                 = &Error{"UNSUPPORTED_VERSION", 35, false, "The version of API is not supported."}
	TopicAlreadyExists                 = &Error{"TOPIC_ALREADY_EXISTS", 36, false, "Topic with this name already exists."}
	InvalidPartitions                  = &Error{"INVALID_PARTITIONS", 37, false, "Number of partitions is below 1."}
	InvalidReplicationFactor           = &Error{"INVALID_REPLICATION_FACTOR", 38, false, "Replication factor is below 1 or larger than the number of available brokers."}
	InvalidReplicaAssignment           = &Error{"INVALID_REPLICA_ASSIGNMENT", 39, false, "Replica assignment is invalid."}
	InvalidConfig                      = &Error{"INVALID_CONFIG", 40, false, "Configuration is invalid."}
	NotController                      = &Error{"NOT_CONTROLLER", 41, true, "This is not the correct controller for this cluster."}
	InvalidRequest                     = &Error{"INVALID_REQUEST", 42, false, "This most likely occurs because of a request being malformed by the client library or the message was sent to an incompatible broker. See the broker logs for more details."}
	UnsupportedForMessageFormat        = &Error{"UNSUPPORTED_FOR_MESSAGE_FORMAT", 43, false, "The message format version on the broker does not support the request."}
	PolicyViolation                    = &Error{"POLICY_VIOLATION", 44, false, "Request parameters do not satisfy the configured policy."}
	OutOfOrderSequenceNumber           = &Error{"OUT_OF_ORDER_SEQUENCE_NUMBER", 45, false, "The broker received an out of order sequence number."}
	DuplicateSequenceNumber            = &Error{"DUPLICATE_SEQUENCE_NUMBER", 46, false, "The broker received a duplicate sequence number."}
	InvalidProducerEpoch               = &Error{"INVALID_PRODUCER_EPOCH", 47, false, "Producer attempted an operation with an old epoch."}
	InvalidTxnState                    = &Error{"INVALID_TXN_STATE", 48, false, "The producer attempted a transactional operation in an invalid state."}
	InvalidProducerIDMapping           = &Error{"INVALID_PRODUCER_ID_MAPPING", 49, false, "The producer attempted to use a producer id which is not currently assigned to its transactional id."}
	InvalidTransactionTimeout          = &Error{"INVALID_TRANSACTION_TIMEOUT", 50, false, "The transaction timeout is larger than the maximum value allowed by the broker (as configured by transaction.max.timeout.ms)."}
	ConcurrentTransactions             = &Error{"CONCURRENT_TRANSACTIONS", 51, false, "The producer attempted to update a transaction while another concurrent operation on the same transaction was ongoing."}
	TransactionCoordinatorFenced       = &Error{"TRANSACTION_COORDINATOR_FENCED", 52, false, "Indicates that the transaction coordinator sending a WriteTxnMarker is no longer the current coordinator for a given producer."}
	TransactionalIDAuthorizationFailed = &Error{"TRANSACTIONAL_ID_AUTHORIZATION_FAILED", 53, false, "Transactional ID authorization failed."}
	SecurityDisabled                   = &Error{"SECURITY_DISABLED", 54, false, "Security features are disabled."}
	OperationNotAttempted              = &Error{"OPERATION_NOT_ATTEMPTED", 55, false, "The broker did not attempt to execute this operation. This may happen for batched RPCs where some operations in the batch failed, causing the broker to respond without trying the rest."}
	KafkaStorageError                  = &Error{"KAFKA_STORAGE_ERROR", 56, true, "Disk error when trying to access log file on the disk."}
	LogDirNotFound                     = &Error{"LOG_DIR_NOT_FOUND", 57, false, "The user-specified log directory is not found in the broker config."}
	SaslAuthenticationFailed           = &Error{"SASL_AUTHENTICATION_FAILED", 58, false, "SASL Authentication failed."}
	UnknownProducerID                  = &Error{"UNKNOWN_PRODUCER_ID", 59, false, "This exception is raised by the broker if it could not locate the producer metadata associated with the producerID in question. This could happen if, for instance, the producer's records were deleted because their retention time had elapsed. Once the last records of the producerID are removed, the producer's metadata is removed from the broker, and future appends by the producer will return this exception."}
	ReassignmentInProgress             = &Error{"REASSIGNMENT_IN_PROGRESS", 60, false, "A partition reassignment is in progress."}
	DelegationTokenAuthDisabled        = &Error{"DELEGATION_TOKEN_AUTH_DISABLED", 61, false, "Delegation Token feature is not enabled."}
	DelegationTokenNotFound            = &Error{"DELEGATION_TOKEN_NOT_FOUND", 62, false, "Delegation Token is not found on server."}
	DelegationTokenOwnerMismatch       = &Error{"DELEGATION_TOKEN_OWNER_MISMATCH", 63, false, "Specified Principal is not valid Owner/Renewer."}
	DelegationTokenRequestNotAllowed   = &Error{"DELEGATION_TOKEN_REQUEST_NOT_ALLOWED", 64, false, "Delegation Token requests are not allowed on PLAINTEXT/1-way SSL channels and on delegation token authenticated channels."}
	DelegationTokenAuthorizationFailed = &Error{"DELEGATION_TOKEN_AUTHORIZATION_FAILED", 65, false, "Delegation Token authorization failed."}
	DelegationTokenExpired             = &Error{"DELEGATION_TOKEN_EXPIRED", 66, false, "Delegation Token is expired."}
	InvalidPrincipalType               = &Error{"INVALID_PRINCIPAL_TYPE", 67, false, "Supplied principalType is not supported."}
	NonEmptyGroup                      = &Error{"NON_EMPTY_GROUP", 68, false, "The group is not empty."}
	GroupIDNotFound                    = &Error{"GROUP_ID_NOT_FOUND", 69, false, "The group id does not exist."}
	FetchSessionIDNotFound             = &Error{"FETCH_SESSION_ID_NOT_FOUND", 70, true, "The fetch session ID was not found."}
	InvalidFetchSessionEpoch           = &Error{"INVALID_FETCH_SESSION_EPOCH", 71, true, "The fetch session epoch is invalid."}
	ListenerNotFound                   = &Error{"LISTENER_NOT_FOUND", 72, true, "There is no listener on the leader broker that matches the listener on which metadata request was processed."}
	TopicDeletionDisabled              = &Error{"TOPIC_DELETION_DISABLED", 73, false, "Topic deletion is disabled."}
	FencedLeaderEpoch                  = &Error{"FENCED_LEADER_EPOCH", 74, true, "The leader epoch in the request is older than the epoch on the broker"}
	UnknownLeaderEpoch                 = &Error{"UNKNOWN_LEADER_EPOCH", 75, true, "The leader epoch in the request is newer than the epoch on the broker"}
	UnsupportedCompressionType         = &Error{"UNSUPPORTED_COMPRESSION_TYPE", 76, false, "The requesting client does not support the compression type of given partition."}
	StaleBrokerEpoch                   = &Error{"STALE_BROKER_EPOCH", 77, false, "Broker epoch has changed"}
	OffsetNotAvailable                 = &Error{"OFFSET_NOT_AVAILABLE", 78, true, "The leader high watermark has not caught up from a recent leader election so the offsets cannot be guaranteed to be monotonically increasing"}
	MemberIDRequired                   = &Error{"MEMBER_ID_REQUIRED", 79, false, "The group member needs to have a valid member id before actually entering a consumer group"}
	PreferredLeaderNotAvailable        = &Error{"PREFERRED_LEADER_NOT_AVAILABLE", 80, true, "The preferred leader was not available"}
	GroupMaxSizeReached                = &Error{"GROUP_MAX_SIZE_REACHED", 81, false, "The consumer group has reached its max size"}
	FencedInstanceID                   = &Error{"FENCED_INSTANCE_ID", 82, false, "The broker rejected this static consumer since another consumer with the same group.instance.id has registered with a different member.id."}
	EligibleLeadersNotAvailable        = &Error{"ELIGIBLE_LEADERS_NOT_AVAILABLE", 83, true, "Eligible topic partition leaders are not available"}
	ElectionNotNeeded                  = &Error{"ELECTION_NOT_NEEDED", 84, true, "Leader election not needed for topic partition"}
	NoReassignmentInProgress           = &Error{"NO_REASSIGNMENT_IN_PROGRESS", 85, false, "No partition reassignment is in progress."}
	GroupSubscribedToTopic             = &Error{"GROUP_SUBSCRIBED_TO_TOPIC", 86, false, "Deleting offsets of a topic is forbidden while the consumer group is actively subscribed to it."}
	InvalidRecord                      = &Error{"INVALID_RECORD", 87, false, "This record has failed the validation on broker and hence be rejected."}
	UnstableOffsetCommit               = &Error{"UNSTABLE_OFFSET_COMMIT", 88, true, "There are unstable offsets that need to be cleared."}
	ThrottlingQuotaExceeded            = &Error{"THROTTLING_QUOTA_EXCEEDED", 89, true, "The throttling quota has been exceeded."}
	ProducerFenced                     = &Error{"PRODUCER_FENCED", 90, false, "There is a newer producer with the same transactionalId which fences the current one."}
	ResourceNotFound                   = &Error{"RESOURCE_NOT_FOUND", 91, false, "A request illegally referred to a resource that does not exist."}
	DuplicateResource                  = &Error{"DUPLICATE_RESOURCE", 92, false, "A request illegally referred to the same resource twice."}
	UnacceptableCredential             = &Error{"UNACCEPTABLE_CREDENTIAL", 93, false, "Requested credential would not meet criteria for acceptability."}
	InconsistentVoterSet               = &Error{"INCONSISTENT_VOTER_SET", 94, false, "Indicates that either the sender or recipient of a voter-only request is not one of the expected voters."}
	InvalidUpdateVersion               = &Error{"INVALID_UPDATE_VERSION", 95, false, "The given update version was invalid."}
	FeatureUpdateFailed                = &Error{"FEATURE_UPDATE_FAILED", 96, false, "Unable to update finalized features due to an unexpected server error."}
	PrincipalDeserializationFailure    = &Error{"PRINCIPAL_DESERIALIZATION_FAILURE", 97, false, "Request principal deserialization failed during forwarding. This indicates an internal error on the broker cluster security setup."}
	SnapshotNotFound                   = &Error{"SNAPSHOT_NOT_FOUND", 98, false, "Requested snapshot was not found."}
	PositionOutOfRange                 = &Error{"POSITION_OUT_OF_RANGE", 99, false, "Requested position is not greater than or equal to zero, and less than the size of the snapshot."}
	UnknownTopicID                     = &Error{"UNKNOWN_TOPIC_ID", 100, true, "This server does not host this topic ID."}
	DuplicateBrokerRegistration        = &Error{"DUPLICATE_BROKER_REGISTRATION", 101, false, "This broker ID is already in use."}
	BrokerIDNotRegistered              = &Error{"BROKER_ID_NOT_REGISTERED", 102, false, "The given broker ID was not registered."}
	InconsistentTopicID                = &Error{"INCONSISTENT_TOPIC_ID", 103, true, "The log's topic ID did not match the topic ID in the request."}
	InconsistentClusterID              = &Error{"INCONSISTENT_CLUSTER_ID", 104, false, "The clusterId in the request does not match that found on the server."}
	TransactionalIDNotFound            = &Error{"TRANSACTIONAL_ID_NOT_FOUND", 105, false, "The transactionalId could not be found."}
	FetchSessionTopicIDError           = &Error{"FETCH_SESSION_TOPIC_ID_ERROR", 106, true, "The fetch session encountered inconsistent topic ID usage."}
	IneligibleReplica                  = &Error{"INELIGIBLE_REPLICA", 107, false, "The new ISR contains at least one ineligible replica."}
	NewLeaderElected                   = &Error{"NEW_LEADER_ELECTED", 108, false, "The AlterPartition request successfully updated the partition state but the leader has changed."}
	OffsetMovedToTieredStorage         = &Error{"OFFSET_MOVED_TO_TIERED_STORAGE", 109, false, "The requested offset is moved to tiered storage."}
	FencedMemberEpoch                  = &Error{"FENCED_MEMBER_EPOCH", 110, false, "The member epoch is fenced by the group coordinator. The member must abandon all its partitions and rejoin."}
	UnreleasedInstanceID               = &Error{"UNRELEASED_INSTANCE_ID", 111, false, "The instance ID is still used by another member in the consumer group. That member must leave first."}
	UnsupportedAssignor                = &Error{"UNSUPPORTED_ASSIGNOR", 112, false, "The assignor or its version range is not supported by the consumer group."}
	StaleMemberEpoch                   = &Error{"STALE_MEMBER_EPOCH", 113, false, "The member epoch is stale. The member must retry after receiving its updated member epoch via the ConsumerGroupHeartbeat API."}
	MismatchedEndpointType             = &Error{"MISMATCHED_ENDPOINT_TYPE", 114, false, "The request was sent to an endpoint of the wrong type."}
	UnsupportedEndpointType            = &Error{"UNSUPPORTED_ENDPOINT_TYPE", 115, false, "This endpoint type is not supported yet."}
	UnknownControllerID                = &Error{"UNKNOWN_CONTROLLER_ID", 116, false, "This controller ID is not known"}
)

var code2err = map[int16]error{
	-1:  UnknownServerError,
	0:   nil,
	1:   OffsetOutOfRange,
	2:   CorruptMessage,
	3:   UnknownTopicOrPartition,
	4:   InvalidFetchSize,
	5:   LeaderNotAvailable,
	6:   NotLeaderForPartition,
	7:   RequestTimedOut,
	8:   BrokerNotAvailable,
	9:   ReplicaNotAvailable,
	10:  MessageTooLarge,
	11:  StaleControllerEpoch,
	12:  OffsetMetadataTooLarge,
	13:  NetworkException,
	14:  CoordinatorLoadInProgress,
	15:  CoordinatorNotAvailable,
	16:  NotCoordinator,
	17:  InvalidTopicException,
	18:  RecordListTooLarge,
	19:  NotEnoughReplicas,
	20:  NotEnoughReplicasAfterAppend,
	21:  InvalidRequiredAcks,
	22:  IllegalGeneration,
	23:  InconsistentGroupProtocol,
	24:  InvalidGroupID,
	25:  UnknownMemberID,
	26:  InvalidSessionTimeout,
	27:  RebalanceInProgress,
	28:  InvalidCommitOffsetSize,
	29:  TopicAuthorizationFailed,
	30:  GroupAuthorizationFailed,
	31:  ClusterAuthorizationFailed,
	32:  InvalidTimestamp,
	33:  UnsupportedSaslMechanism,
	34:  IllegalSaslState,
	35:  UnsupportedVersion,
	36:  TopicAlreadyExists,
	37:  InvalidPartitions,
	38:  InvalidReplicationFactor,
	39:  InvalidReplicaAssignment,
	40:  InvalidConfig,
	41:  NotController,
	42:  InvalidRequest,
	43:  UnsupportedForMessageFormat,
	44:  PolicyViolation,
	45:  OutOfOrderSequenceNumber,
	46:  DuplicateSequenceNumber,
	47:  InvalidProducerEpoch,
	48:  InvalidTxnState,
	49:  InvalidProducerIDMapping,
	50:  InvalidTransactionTimeout,
	51:  ConcurrentTransactions,
	52:  TransactionCoordinatorFenced,
	53:  TransactionalIDAuthorizationFailed,
	54:  SecurityDisabled,
	55:  OperationNotAttempted,
	56:  KafkaStorageError,
	57:  LogDirNotFound,
	58:  SaslAuthenticationFailed,
	59:  UnknownProducerID,
	60:  ReassignmentInProgress,
	61:  DelegationTokenAuthDisabled,
	62:  DelegationTokenNotFound,
	63:  DelegationTokenOwnerMismatch,
	64:  DelegationTokenRequestNotAllowed,
	65:  DelegationTokenAuthorizationFailed,
	66:  DelegationTokenExpired,
	67:  InvalidPrincipalType,
	68:  NonEmptyGroup,
	69:  GroupIDNotFound,
	70:  FetchSessionIDNotFound,
	71:  InvalidFetchSessionEpoch,
	72:  ListenerNotFound,
	73:  TopicDeletionDisabled,
	74:  FencedLeaderEpoch,
	75:  UnknownLeaderEpoch,
	76:  UnsupportedCompressionType,
	77:  StaleBrokerEpoch,
	78:  OffsetNotAvailable,
	79:  MemberIDRequired,
	80:  PreferredLeaderNotAvailable,
	81:  GroupMaxSizeReached,
	82:  FencedInstanceID,
	83:  EligibleLeadersNotAvailable,
	84:  ElectionNotNeeded,
	85:  NoReassignmentInProgress,
	86:  GroupSubscribedToTopic,
	87:  InvalidRecord,
	88:  UnstableOffsetCommit,
	89:  ThrottlingQuotaExceeded,
	90:  ProducerFenced,
	91:  ResourceNotFound,
	92:  DuplicateResource,
	93:  UnacceptableCredential,
	94:  InconsistentVoterSet,
	95:  InvalidUpdateVersion,
	96:  FeatureUpdateFailed,
	97:  PrincipalDeserializationFailure,
	98:  SnapshotNotFound,
	99:  PositionOutOfRange,
	100: UnknownTopicID,
	101: DuplicateBrokerRegistration,
	102: BrokerIDNotRegistered,
	103: InconsistentTopicID,
	104: InconsistentClusterID,
	105: TransactionalIDNotFound,
	106: FetchSessionTopicIDError,
	107: IneligibleReplica,
	108: NewLeaderElected,
	109: OffsetMovedToTieredStorage, // KIP-405, v3.5
	110: FencedMemberEpoch,          // KIP-848, released unstable in v3.6, stable in 3.7
	111: UnreleasedInstanceID,       // ""
	112: UnsupportedAssignor,        // ""
	113: StaleMemberEpoch,           // ""
	114: MismatchedEndpointType,     // KIP-919, v3.7
	115: UnsupportedEndpointType,    // ""
	116: UnknownControllerID,        // ""

}
