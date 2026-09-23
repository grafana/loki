package kfake

import (
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
// * CoordinatorEpoch is not validated (kfake is single-coord); it is written into the marker value
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

				// setErr sets the code unless a fault already answered.
				setErr := func(code int16) {
					if respPart.ErrorCode == 0 {
						respPart.ErrorCode = code
					}
				}
				if !clusterAuthorized {
					respPart.ErrorCode = kerr.ClusterAuthorizationFailed.Code
					respTopic.Partitions = append(respTopic.Partitions, respPart)
					continue
				}
				if fe := creq.faults.check(faultKey{topic: mt.Topic}.part(p)); fe != nil {
					respPart.ErrorCode = fe.Code
					if creq.skipsWork(fe) { // a timed-out marker is still written
						respTopic.Partitions = append(respTopic.Partitions, respPart)
						continue
					}
				}
				pd, ok := ps[p]
				switch {
				case !topicExists, !ok:
					setErr(kerr.UnknownTopicOrPartition.Code)
				case pd.leader != creq.cc.b:
					setErr(kerr.NotLeaderForPartition.Code)
				default:
					if off := c.writeTxnMarker(pd, m.ProducerID, m.ProducerEpoch, m.Committed, m.CoordinatorEpoch); off < 0 {
						setErr(kerr.UnknownServerError.Code)
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
func (c *Cluster) writeTxnMarker(pd *partData, pid int64, epoch int16, commit bool, coordinatorEpoch int32) int64 {
	b, nbytes := txnMarkerBatch(pid, epoch, commit, coordinatorEpoch)

	firstOffset, hadTxn := pd.uncommittedPIDs[pid]
	delete(pd.uncommittedPIDs, pid)

	controlOffset := c.pushBatch(pd, nbytes, b, false)
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
