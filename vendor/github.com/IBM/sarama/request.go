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
	case 0:
		return &ProduceRequest{Version: version}
	case 1:
		return &FetchRequest{Version: version}
	case 2:
		return &OffsetRequest{Version: version}
	case 3:
		return &MetadataRequest{Version: version}
	// 4: LeaderAndIsrRequest
	// 5: StopReplicaRequest
	// 6: UpdateMetadataRequest
	// 7: ControlledShutdownRequest
	case 8:
		return &OffsetCommitRequest{Version: version}
	case 9:
		return &OffsetFetchRequest{Version: version}
	case 10:
		return &FindCoordinatorRequest{Version: version}
	case 11:
		return &JoinGroupRequest{Version: version}
	case 12:
		return &HeartbeatRequest{Version: version}
	case 13:
		return &LeaveGroupRequest{Version: version}
	case 14:
		return &SyncGroupRequest{Version: version}
	case 15:
		return &DescribeGroupsRequest{Version: version}
	case 16:
		return &ListGroupsRequest{Version: version}
	case 17:
		return &SaslHandshakeRequest{Version: version}
	case 18:
		return &ApiVersionsRequest{Version: version}
	case 19:
		return &CreateTopicsRequest{Version: version}
	case 20:
		return &DeleteTopicsRequest{Version: version}
	case 21:
		return &DeleteRecordsRequest{Version: version}
	case 22:
		return &InitProducerIDRequest{Version: version}
	// 23: OffsetForLeaderEpochRequest
	case 24:
		return &AddPartitionsToTxnRequest{Version: version}
	case 25:
		return &AddOffsetsToTxnRequest{Version: version}
	case 26:
		return &EndTxnRequest{Version: version}
	// 27: WriteTxnMarkersRequest
	case 28:
		return &TxnOffsetCommitRequest{Version: version}
	case 29:
		return &DescribeAclsRequest{Version: int(version)}
	case 30:
		return &CreateAclsRequest{Version: version}
	case 31:
		return &DeleteAclsRequest{Version: int(version)}
	case 32:
		return &DescribeConfigsRequest{Version: version}
	case 33:
		return &AlterConfigsRequest{Version: version}
	// 34: AlterReplicaLogDirsRequest
	case 35:
		return &DescribeLogDirsRequest{Version: version}
	case 36:
		return &SaslAuthenticateRequest{Version: version}
	case 37:
		return &CreatePartitionsRequest{Version: version}
	// 38: CreateDelegationTokenRequest
	// 39: RenewDelegationTokenRequest
	// 40: ExpireDelegationTokenRequest
	// 41: DescribeDelegationTokenRequest
	case 42:
		return &DeleteGroupsRequest{Version: version}
	// 43: ElectLeadersRequest
	case 44:
		return &IncrementalAlterConfigsRequest{Version: version}
	case 45:
		return &AlterPartitionReassignmentsRequest{Version: version}
	case 46:
		return &ListPartitionReassignmentsRequest{Version: version}
	case 47:
		return &DeleteOffsetsRequest{Version: version}
	case 48:
		return &DescribeClientQuotasRequest{Version: version}
	case 49:
		return &AlterClientQuotasRequest{Version: version}
	case 50:
		return &DescribeUserScramCredentialsRequest{Version: version}
	case 51:
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
