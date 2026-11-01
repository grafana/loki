package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterShareGroupOffsets: v0 (KIP-932)
//
// Behavior:
// * Resets the SPSO for partitions in an empty share group
// * Clears all in-flight record state and delivery counts
// * Auto-creates the share group if it doesn't exist
// * Rejects requests when the group has active members (NON_EMPTY_GROUP)

func init() { regKey(91, 0, 0) }

func (c *Cluster) handleAlterShareGroupOffsets(creq *clientReq) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.AlterShareGroupOffsetsRequest)
		resp = req.ResponseKind().(*kmsg.AlterShareGroupOffsetsResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// Coordinator check: AlterShareGroupOffsets is routed to the
	// share coordinator.
	if c.coordinator(req.GroupID).node != creq.cc.b.node {
		resp.ErrorCode = kerr.NotCoordinator.Code
		return resp, nil
	}

	// ACL: require GROUP READ (Kafka uses READ, not ALTER).
	if !c.allowedACL(creq, req.GroupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead) {
		resp.ErrorCode = kerr.GroupAuthorizationFailed.Code
		return resp, nil
	}

	// Auto-create the share group if it doesn't exist.
	sg := c.shareGroups.getOrCreate(req.GroupID)

	// Pre-lookup topic IDs, valid partitions, and ACL results while
	// in run() where c.data is safe to read.
	type alterTopicInfo struct {
		id      uuid
		valid   map[int32]struct{}
		aclDeny bool
	}
	topicInfo := make(map[string]alterTopicInfo, len(req.Topics))
	for _, rt := range req.Topics {
		info := alterTopicInfo{id: c.data.t2id[rt.Topic]}
		// ACL: per-topic READ check.
		if !c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead) {
			info.aclDeny = true
		} else {
			info.valid = make(map[int32]struct{})
			for _, rp := range rt.Partitions {
				if _, ok := c.data.tps.getp(rt.Topic, rp.Partition); ok {
					info.valid[rp.Partition] = struct{}{}
				}
			}
		}
		topicInfo[rt.Topic] = info
	}

	if !sg.waitControl(func() {
		if len(sg.members) > 0 {
			resp.ErrorCode = kerr.NonEmptyGroup.Code
			return
		}

		// Guard against concurrent admin operations (e.g., sweep
		// timer or another AlterShareGroupOffsets) while in manage.
		sg.mu.Lock()
		defer sg.mu.Unlock()
		for i := range req.Topics {
			rt := &req.Topics[i]
			rst := kmsg.NewAlterShareGroupOffsetsResponseTopic()
			rst.Topic = rt.Topic
			info := topicInfo[rt.Topic]
			rst.TopicID = info.id

			if info.aclDeny {
				for j := range rt.Partitions {
					rsp := kmsg.NewAlterShareGroupOffsetsResponseTopicPartition()
					rsp.Partition = rt.Partitions[j].Partition
					rsp.ErrorCode = kerr.TopicAuthorizationFailed.Code
					rst.Partitions = append(rst.Partitions, rsp)
				}
				resp.Topics = append(resp.Topics, rst)
				continue
			}

			for j := range rt.Partitions {
				rp := &rt.Partitions[j]
				rsp := kmsg.NewAlterShareGroupOffsetsResponseTopicPartition()
				rsp.Partition = rp.Partition

				if _, ok := info.valid[rp.Partition]; !ok {
					rsp.ErrorCode = kerr.UnknownTopicOrPartition.Code
					rst.Partitions = append(rst.Partitions, rsp)
					continue
				}

				// Reset SPSO, scan cursor, end offset, and all record state.
				sp := sg.partitions.mkp(rt.Topic, rp.Partition, func() *sharePartition {
					return new(sharePartition)
				})
				*sp = sharePartition{
					spso:       rp.StartOffset,
					scanOffset: rp.StartOffset,
					acquireEnd: rp.StartOffset,
					records:    make(map[int64]shareRecord),
				}
				rst.Partitions = append(rst.Partitions, rsp)
			}
			resp.Topics = append(resp.Topics, rst)
		}
	}) {
		resp.ErrorCode = kerr.GroupIDNotFound.Code
	}

	return resp, nil
}
