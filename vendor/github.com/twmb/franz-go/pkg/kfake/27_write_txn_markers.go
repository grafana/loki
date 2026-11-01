package kfake

import (
	"hash/crc32"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// WriteTxnMarkers: v0-2
//
// Broker-to-broker request used by the transaction coordinator to write
// commit/abort markers to partition logs. kfake is both coordinator and
// broker, so EndTxn already writes markers; this handler exists mostly
// for admin tooling that sends WriteTxnMarkers directly (e.g. kadm).
//
// Behavior:
// * Writes a control batch per requested partition with the given pid+epoch
// * Updates abortedTxns tracking and recalculates LSO
// * Does NOT transition pid state (that's the coordinator's role)
// * CoordinatorEpoch is accepted but not validated (kfake is single-coord)
//
// Version notes:
// * v1: Flexible versions
// * v2: TransactionVersion field (KIP-1228) - accepted, ignored

func init() { regKey(27, 0, 2) }

func (c *Cluster) handleWriteTxnMarkers(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.WriteTxnMarkersRequest)
	resp := req.ResponseKind().(*kmsg.WriteTxnMarkersResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	clusterAuthorized := c.allowedClusterACL(creq, kmsg.ACLOperationClusterAction)

	for _, m := range req.Markers {
		respMarker := kmsg.NewWriteTxnMarkersResponseMarker()
		respMarker.ProducerID = m.ProducerID

		for _, mt := range m.Topics {
			respTopic := kmsg.NewWriteTxnMarkersResponseMarkerTopic()
			respTopic.Topic = mt.Topic

			ps, topicExists := c.data.tps.gett(mt.Topic)
			for _, p := range mt.Partitions {
				respPart := kmsg.NewWriteTxnMarkersResponseMarkerTopicPartition()
				respPart.Partition = p

				switch {
				case !clusterAuthorized:
					respPart.ErrorCode = kerr.ClusterAuthorizationFailed.Code
				case !topicExists:
					respPart.ErrorCode = kerr.UnknownTopicOrPartition.Code
				default:
					pd, ok := ps[p]
					if !ok {
						respPart.ErrorCode = kerr.UnknownTopicOrPartition.Code
					} else if pd.leader != creq.cc.b {
						respPart.ErrorCode = kerr.NotLeaderForPartition.Code
					} else if off := c.writeTxnMarker(pd, m.ProducerID, m.ProducerEpoch, m.Committed); off < 0 {
						respPart.ErrorCode = kerr.UnknownServerError.Code
					}
				}
				respTopic.Partitions = append(respTopic.Partitions, respPart)
			}
			respMarker.Topics = append(respMarker.Topics, respTopic)
		}
		resp.Markers = append(resp.Markers, respMarker)
	}

	return resp, nil
}

// writeTxnMarker writes a commit/abort control batch for (pid, epoch) to
// the partition, updates aborted-txn tracking, and recalculates the LSO.
// Returns the control batch offset, or -1 on persist failure.
func (c *Cluster) writeTxnMarker(pd *partData, pid int64, epoch int16, commit bool) int64 {
	var controlType byte // 0=abort
	if commit {
		controlType = 1 // commit
	}
	rec := kmsg.Record{Key: []byte{0, 0, 0, controlType}}
	rec.Length = int32(len(rec.AppendTo(nil)) - 1)
	now := time.Now().UnixMilli()
	b := kmsg.RecordBatch{
		PartitionLeaderEpoch: -1,
		Magic:                2,
		Attributes:           int16(0b00000000_00110000), // control + txnl
		LastOffsetDelta:      0,
		FirstTimestamp:       now,
		MaxTimestamp:         now,
		ProducerID:           pid,
		ProducerEpoch:        epoch,
		FirstSequence:        -1,
		NumRecords:           1,
		Records:              rec.AppendTo(nil),
	}
	benc := b.AppendTo(nil)
	b.Length = int32(len(benc) - 12)
	b.CRC = int32(crc32.Checksum(benc[21:], crc32c))

	firstOffset, hadTxn := pd.uncommittedPIDs[pid]
	delete(pd.uncommittedPIDs, pid)

	controlOffset := c.pushBatch(pd, len(benc), b, false)
	if controlOffset < 0 {
		pd.recalculateLSO()
		return -1
	}
	if !commit && hadTxn {
		pd.abortedTxns = append(pd.abortedTxns, abortedTxnEntry{
			producerID:  pid,
			firstOffset: firstOffset,
			lastOffset:  controlOffset,
		})
	}
	pd.recalculateLSO()
	return controlOffset
}
