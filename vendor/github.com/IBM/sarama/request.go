package sarama

import (
	"encoding/binary"
	"fmt"
	"io"
)

type protocolBody interface {
	encoder
	versionedDecoder
	key() int16
	version() int16
	setVersion(int16)
	headerVersion() int16
	isValidVersion() bool
	requiredVersion() KafkaVersion
}

type request struct {
	correlationID int32
	clientID      string
	body          protocolBody
}

func (r *request) encode(pe packetEncoder) error {
	pe.push(&lengthField{})
	pe.putInt16(r.body.key())
	pe.putInt16(r.body.version())
	pe.putInt32(r.correlationID)

	if r.body.headerVersion() >= 1 {
		err := pe.putString(r.clientID)
		if err != nil {
			return err
		}
	}

	if r.body.headerVersion() >= 2 {
		// we don't use tag headers at the moment so we just put an array length of 0
		pe.putUVarint(0)
	}

	err := r.body.encode(pe)
	if err != nil {
		return err
	}

	return pe.pop()
}

func (r *request) decode(pd packetDecoder) (err error) {
	key, err := pd.getInt16()
	if err != nil {
		return err
	}

	version, err := pd.getInt16()
	if err != nil {
		return err
	}

	r.correlationID, err = pd.getInt32()
	if err != nil {
		return err
	}

	r.clientID, err = pd.getString()
	if err != nil {
		return err
	}

	r.body = allocateBody(key, version)
	if r.body == nil {
		return PacketDecodingError{fmt.Sprintf("unknown request key (%d)", key)}
	}

	if r.body.headerVersion() >= 2 {
		// tagged field
		_, err = pd.getUVarint()
		if err != nil {
			return err
		}
	}

	return r.body.decode(pd, version)
}

func decodeRequest(r io.Reader) (*request, int, error) {
	var (
		bytesRead   int
		lengthBytes = make([]byte, 4)
	)

	if _, err := io.ReadFull(r, lengthBytes); err != nil {
		return nil, bytesRead, err
	}

	bytesRead += len(lengthBytes)
	length := int32(binary.BigEndian.Uint32(lengthBytes))

	if length <= 4 || length > MaxRequestSize {
		return nil, bytesRead, PacketDecodingError{fmt.Sprintf("message of length %d too large or too small", length)}
	}

	encodedReq := make([]byte, length)
	if _, err := io.ReadFull(r, encodedReq); err != nil {
		return nil, bytesRead, err
	}

	bytesRead += len(encodedReq)

	req := &request{}
	if err := decode(encodedReq, req, nil); err != nil {
		return nil, bytesRead, err
	}

	return req, bytesRead, nil
}

func allocateBody(key, version int16) protocolBody {
	switch key {
	case apiKeyProduce:
		return &ProduceRequest{Version: version}
	case apiKeyFetch:
		return &FetchRequest{Version: version}
	case apiKeyListOffsets:
		return &OffsetRequest{Version: version}
	case apiKeyMetadata:
		return &MetadataRequest{Version: version}
	// 4: LeaderAndIsrRequest
	// 5: StopReplicaRequest
	// 6: UpdateMetadataRequest
	// 7: ControlledShutdownRequest
	case apiKeyOffsetCommit:
		return &OffsetCommitRequest{Version: version}
	case apiKeyOffsetFetch:
		return &OffsetFetchRequest{Version: version}
	case apiKeyFindCoordinator:
		return &FindCoordinatorRequest{Version: version}
	case apiKeyJoinGroup:
		return &JoinGroupRequest{Version: version}
	case apiKeyHeartbeat:
		return &HeartbeatRequest{Version: version}
	case apiKeyLeaveGroup:
		return &LeaveGroupRequest{Version: version}
	case apiKeySyncGroup:
		return &SyncGroupRequest{Version: version}
	case apiKeyDescribeGroups:
		return &DescribeGroupsRequest{Version: version}
	case apiKeyListGroups:
		return &ListGroupsRequest{Version: version}
	case apiKeySaslHandshake:
		return &SaslHandshakeRequest{Version: version}
	case apiKeyApiVersions:
		return &ApiVersionsRequest{Version: version}
	case apiKeyCreateTopics:
		return &CreateTopicsRequest{Version: version}
	case apiKeyDeleteTopics:
		return &DeleteTopicsRequest{Version: version}
	case apiKeyDeleteRecords:
		return &DeleteRecordsRequest{Version: version}
	case apiKeyInitProducerId:
		return &InitProducerIDRequest{Version: version}
	// 23: OffsetForLeaderEpochRequest
	case apiKeyAddPartitionsToTxn:
		return &AddPartitionsToTxnRequest{Version: version}
	case apiKeyAddOffsetsToTxn:
		return &AddOffsetsToTxnRequest{Version: version}
	case apiKeyEndTxn:
		return &EndTxnRequest{Version: version}
	// 27: WriteTxnMarkersRequest
	case apiKeyTxnOffsetCommit:
		return &TxnOffsetCommitRequest{Version: version}
	case apiKeyDescribeAcls:
		return &DescribeAclsRequest{Version: int(version)}
	case apiKeyCreateAcls:
		return &CreateAclsRequest{Version: version}
	case apiKeyDeleteAcls:
		return &DeleteAclsRequest{Version: int(version)}
	case apiKeyDescribeConfigs:
		return &DescribeConfigsRequest{Version: version}
	case apiKeyAlterConfigs:
		return &AlterConfigsRequest{Version: version}
	// 34: AlterReplicaLogDirsRequest
	case apiKeyDescribeLogDirs:
		return &DescribeLogDirsRequest{Version: version}
	case apiKeySASLAuth:
		return &SaslAuthenticateRequest{Version: version}
	case apiKeyCreatePartitions:
		return &CreatePartitionsRequest{Version: version}
	// 38: CreateDelegationTokenRequest
	// 39: RenewDelegationTokenRequest
	// 40: ExpireDelegationTokenRequest
	// 41: DescribeDelegationTokenRequest
	case apiKeyDeleteGroups:
		return &DeleteGroupsRequest{Version: version}
	case apiKeyElectLeaders:
		return &ElectLeadersRequest{Version: version}
	case apiKeyIncrementalAlterConfigs:
		return &IncrementalAlterConfigsRequest{Version: version}
	case apiKeyAlterPartitionReassignments:
		return &AlterPartitionReassignmentsRequest{Version: version}
	case apiKeyListPartitionReassignments:
		return &ListPartitionReassignmentsRequest{Version: version}
	case apiKeyOffsetDelete:
		return &DeleteOffsetsRequest{Version: version}
	case apiKeyDescribeClientQuotas:
		return &DescribeClientQuotasRequest{Version: version}
	case apiKeyAlterClientQuotas:
		return &AlterClientQuotasRequest{Version: version}
	case apiKeyDescribeUserScramCredentials:
		return &DescribeUserScramCredentialsRequest{Version: version}
	case apiKeyAlterUserScramCredentials:
		return &AlterUserScramCredentialsRequest{Version: version}
		// 52: VoteRequest
		// 53: BeginQuorumEpochRequest
		// 54: EndQuorumEpochRequest
		// 55: DescribeQuorumRequest
		// 56: AlterPartitionRequest
		// 57: UpdateFeaturesRequest
		// 58: EnvelopeRequest
		// 59: FetchSnapshotRequest
		// 60: DescribeClusterRequest
		// 61: DescribeProducersRequest
		// 62: BrokerRegistrationRequest
		// 63: BrokerHeartbeatRequest
		// 64: UnregisterBrokerRequest
		// 65: DescribeTransactionsRequest
		// 66: ListTransactionsRequest
		// 67: AllocateProducerIdsRequest
		// 68: ConsumerGroupHeartbeatRequest
	}
	return nil
}
