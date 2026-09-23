package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// TxnOffsetCommit: v0-5
//
// Behavior:
// * Stages offset commits as part of a transaction
// * Offsets are applied on EndTxn commit, discarded on abort
// * v3+: GenerationID/MemberID validation for zombie fencing (KIP-447)
// * v5+: Implicit group addition to transaction (KIP-890)
//
// Version notes:
// * v2: ThrottleMillis
// * v3: Flexible versions, GenerationID and MemberID for zombie fencing (KIP-447)
// * v4-5: No field changes; v5 enables implicit group addition (KIP-890)
// * v6: TopicID replaces Topic (KIP-1319)

func init() { regKey(28, 0, 6) }

func (c *Cluster) handleTxnOffsetCommit(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.TxnOffsetCommitRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// v6: resolve TopicIDs to topic names, mirroring OffsetCommit v10.
	// Topics with unknown IDs get per-partition UNKNOWN_TOPIC_ID errors;
	// valid topics pass through by name.
	var errTopics []kmsg.TxnOffsetCommitResponseTopic
	if req.Version >= 6 {
		valid := req.Topics[:0]
		for i := range req.Topics {
			t := &req.Topics[i]
			name, ok := c.data.id2t[t.TopicID]
			if !ok {
				st := kmsg.NewTxnOffsetCommitResponseTopic()
				st.TopicID = t.TopicID
				for _, p := range t.Partitions {
					sp := kmsg.NewTxnOffsetCommitResponseTopicPartition()
					sp.Partition = p.Partition
					sp.ErrorCode = kerr.UnknownTopicID.Code
					st.Partitions = append(st.Partitions, sp)
				}
				errTopics = append(errTopics, st)
				continue
			}
			t.Topic = name
			valid = append(valid, *t)
		}
		req.Topics = valid
		if len(req.Topics) == 0 && len(errTopics) > 0 {
			resp := req.ResponseKind().(*kmsg.TxnOffsetCommitResponse)
			resp.Topics = errTopics
			return resp, nil
		}
	}

	errResp := func(errCode int16) kmsg.Response {
		resp := req.ResponseKind().(*kmsg.TxnOffsetCommitResponse)
		for _, rt := range req.Topics {
			st := kmsg.NewTxnOffsetCommitResponseTopic()
			st.Topic = rt.Topic
			st.TopicID = rt.TopicID
			for _, rp := range rt.Partitions {
				sp := kmsg.NewTxnOffsetCommitResponseTopicPartition()
				sp.Partition = rp.Partition
				sp.ErrorCode = errCode
				st.Partitions = append(st.Partitions, sp)
			}
			resp.Topics = append(resp.Topics, st)
		}
		return resp
	}

	// ACL check: WRITE on TxnID
	if e := c.deny(creq, req.TransactionalID, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationWrite, faultKey{txnID: req.TransactionalID}); e != nil && creq.skipsWork(e) { // a timed-out commit falls through to the per-partition checks
		return errResp(e.Code), nil
	}

	// ACL check: READ on Group
	if e := c.deny(creq, req.Group, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead, faultKey{txnID: req.TransactionalID, group: req.Group}); e != nil && creq.skipsWork(e) {
		return errResp(e.Code), nil
	}

	// ACL check: READ on each Topic
	for _, rt := range req.Topics {
		if e := c.deny(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead, faultKey{txnID: req.TransactionalID, group: req.Group, topic: rt.Topic}); e != nil && creq.skipsWork(e) {
			return errResp(e.Code), nil
		}
	}

	resp := c.pids.doTxnOffsetCommit(creq).(*kmsg.TxnOffsetCommitResponse)

	// v6: echo TopicIDs on the response (the name is not on the wire) and
	// merge in any unknown-id error topics from the resolution above.
	if req.Version >= 6 {
		t2id := make(map[string][16]byte, len(req.Topics))
		for i := range req.Topics {
			t2id[req.Topics[i].Topic] = req.Topics[i].TopicID
		}
		for i := range resp.Topics {
			resp.Topics[i].TopicID = t2id[resp.Topics[i].Topic]
		}
		resp.Topics = append(resp.Topics, errTopics...)
	}
	return resp, nil
}
