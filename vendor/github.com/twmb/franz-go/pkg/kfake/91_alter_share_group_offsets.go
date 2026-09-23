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

	if c.coordinator(req.GroupID).node != creq.cc.b.node {
		resp.ErrorCode = kerr.NotCoordinator.Code
		return resp, nil
	}

	// ACL: require GROUP READ (Kafka uses READ, not ALTER).
	if e := c.deny(creq, req.GroupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationRead, faultKey{group: req.GroupID}); e != nil && creq.skipsWork(e) { // a timed-out reset falls through to the per-partition checks
		resp.ErrorCode = e.Code
		return resp, nil
	}

	// Group type exclusivity: a consumer group under this id means
	// there is no share group to create. Kafka's
	// getOrMaybeCreateShareGroup throws GROUP_ID_NOT_FOUND.
	if _, isConsumer := c.groups.gs[req.GroupID]; isConsumer {
		resp.ErrorCode = kerr.GroupIDNotFound.Code
		return resp, nil
	}

	// Auto-create the share group if it doesn't exist.
	sg := c.shareGroups.getOrCreate(req.GroupID)

	if len(sg.members) > 0 {
		resp.ErrorCode = kerr.NonEmptyGroup.Code
		return resp, nil
	}

	for i := range req.Topics {
		rt := &req.Topics[i]
		rst := kmsg.NewAlterShareGroupOffsetsResponseTopic()
		rst.Topic = rt.Topic
		id := c.data.t2id[rt.Topic]
		rst.TopicID = id

		// ACL: per-topic READ check.
		if e := c.deny(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationRead, faultKey{topic: rt.Topic, topicID: id}); e != nil && creq.skipsWork(e) { // a timed-out reset falls through to the per-partition check
			for j := range rt.Partitions {
				rsp := kmsg.NewAlterShareGroupOffsetsResponseTopicPartition()
				rsp.Partition = rt.Partitions[j].Partition
				rsp.ErrorCode = e.Code
				rst.Partitions = append(rst.Partitions, rsp)
			}
			resp.Topics = append(resp.Topics, rst)
			continue
		}

		for j := range rt.Partitions {
			rp := &rt.Partitions[j]
			rsp := kmsg.NewAlterShareGroupOffsetsResponseTopicPartition()
			rsp.Partition = rp.Partition

			e := creq.faults.check(faultKey{group: req.GroupID, topic: rt.Topic, topicID: id}.part(rp.Partition))
			if e != nil {
				rsp.ErrorCode = e.Code
				if creq.skipsWork(e) { // a timed-out reset still resets
					rst.Partitions = append(rst.Partitions, rsp)
					continue
				}
			}
			if _, ok := c.data.tps.getp(rt.Topic, rp.Partition); !ok {
				if e == nil {
					rsp.ErrorCode = kerr.UnknownTopicOrPartition.Code
				}
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

	return resp, nil
}
