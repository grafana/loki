package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ShareAcknowledge: v0-2 (KIP-932, KIP-1222)
//
// Behavior:
// * Acknowledges records previously acquired via ShareFetch
// * Shares the same session as ShareFetch (epoch incremented on success)
// * On session close (epoch -1), remaining acquired records are released
// * Ack types: 0=Gap, 1=Accept, 2=Release, 3=Reject, 4=Renew (v2+)
//
// Version notes:
// * v0: Initial share acknowledge (KIP-932)
// * v2: IsRenewAck and type 4 (Renew) for lock renewal (KIP-1222)

func init() { regKey(79, 0, 2) }

func (c *Cluster) handleShareAcknowledge(creq *clientReq) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.ShareAcknowledgeRequest)
		resp = req.ResponseKind().(*kmsg.ShareAcknowledgeResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	resp.AcquisitionLockTimeoutMillis = c.shareRecordLockDurationMs()

	var groupID, memberID string
	if req.GroupID != nil {
		groupID = *req.GroupID
	}
	if req.MemberID != nil {
		memberID = *req.MemberID
	}

	// ACL: require GROUP READ.
	if !c.allowedACL(creq, groupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp, nil
	}

	// Validate memberID format (non-empty, <=36 chars).
	if memberID == "" || len(memberID) > 36 {
		resp.ErrorCode = kerr.InvalidRequest.Code
		return resp, nil
	}

	sg := c.shareGroups.get(groupID)
	if sg == nil {
		resp.ErrorCode = kerr.GroupIDNotFound.Code
		return resp, nil
	}

	// Session epoch validation. ShareAcknowledge shares the same session
	// as ShareFetch: the client tracks a single epoch that increments on
	// each successful ShareFetch or ShareAcknowledge.
	sgs := &c.shareGroups
	sessionKey := shareSessionKey{
		group:    groupID,
		memberID: memberID,
		broker:   creq.cc.b.node,
	}

	id2t := c.data.id2t
	maxDelivery := c.shareMaxDeliveryAttempts()
	maxAckType := shareAckReject
	if req.Version >= 2 && req.IsRenewAck {
		maxAckType = shareAckRenew
	}

	// Response-building closure (matching produce handler style).
	// Returns the appended partition pointer so callers can populate
	// optional fields (CurrentLeader on NotLeaderForPartition).
	topicIdx := make(map[uuid]int)
	donep := func(tid uuid, p int32, ec int16) *kmsg.ShareAcknowledgeResponseTopicPartition {
		i, ok := topicIdx[tid]
		if !ok {
			i = len(resp.Topics)
			topicIdx[tid] = i
			t := kmsg.NewShareAcknowledgeResponseTopic()
			t.TopicID = tid
			resp.Topics = append(resp.Topics, t)
		}
		sp := kmsg.NewShareAcknowledgeResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = ec
		resp.Topics[i].Partitions = append(resp.Topics[i].Partitions, sp)
		return &resp.Topics[i].Partitions[len(resp.Topics[i].Partitions)-1]
	}
	onPartition := func(tid uuid, p int32, ec int16) {
		donep(tid, p, ec)
	}
	onNotLeader := func(tid uuid, p int32, pd *partData) {
		sp := donep(tid, p, kerr.NotLeaderForPartition.Code)
		sp.CurrentLeader.LeaderID = pd.leader.node
		sp.CurrentLeader.LeaderEpoch = pd.epoch
	}

	// Session close: process acks, release remaining acquired records,
	// delete session. A member may have multiple sessions (one per
	// broker), so we only release records for this session's partitions.
	if req.ShareSessionEpoch == -1 {
		session := sgs.sessions[sessionKey]
		if session == nil {
			resp.ErrorCode = kerr.ShareSessionNotFound.Code
			return resp, nil
		}
		ackTs := ackTopicsFromAcknowledge(req.Topics)
		sg.mu.Lock()
		toFire := sg.processShareAcks(creq, memberID, ackTs, maxAckType, id2t, maxDelivery, onPartition, onNotLeader)
		released := sg.releaseRecordsForSessionLocked(memberID, session, id2t, maxDelivery)
		sg.mu.Unlock()
		fireAll(toFire)
		if released {
			sg.fireAllShareWatchers()
		}
		delete(sgs.sessions, sessionKey)
		return resp, nil
	}

	// Epoch 0 opens a new session via ShareFetch, not ShareAcknowledge.
	if req.ShareSessionEpoch == 0 {
		resp.ErrorCode = kerr.InvalidShareSessionEpoch.Code
		return resp, nil
	}

	session := sgs.sessions[sessionKey]
	if session == nil {
		resp.ErrorCode = kerr.ShareSessionNotFound.Code
		return resp, nil
	}
	if req.ShareSessionEpoch != session.epoch {
		resp.ErrorCode = kerr.InvalidShareSessionEpoch.Code
		return resp, nil
	}

	ackTs := ackTopicsFromAcknowledge(req.Topics)
	sg.mu.Lock()
	toFire := sg.processShareAcks(creq, memberID, ackTs, maxAckType, id2t, maxDelivery, onPartition, onNotLeader)
	sg.mu.Unlock()
	fireAll(toFire)

	session.bumpEpoch()
	return resp, nil
}
