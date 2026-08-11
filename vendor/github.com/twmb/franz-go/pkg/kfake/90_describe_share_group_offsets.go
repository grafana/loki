package kfake

import (
	"maps"
	"slices"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribeShareGroupOffsets: v0-1 (KIP-932, KIP-1226)
//
// Behavior:
// * Returns the Share-Partition Start Offset (SPSO) and lag per partition
// * Lag = HWM - SPSO - deliveryComplete (records already acknowledged/archived)
// * When Topics is nil, describes all topics the group has state for
// * Describe-all silently filters unauthorized topics
//
// Version notes:
// * v0: Initial describe share group offsets (KIP-932)
// * v1: Lag field added (KIP-1226)

func init() { regKey(90, 0, 1) }

func (c *Cluster) handleDescribeShareGroupOffsets(creq *clientReq) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.DescribeShareGroupOffsetsRequest)
		resp = req.ResponseKind().(*kmsg.DescribeShareGroupOffsetsResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	for i := range req.Groups {
		rg := &req.Groups[i]
		rsg := kmsg.NewDescribeShareGroupOffsetsResponseGroup()
		rsg.GroupID = rg.GroupID

		// Coordinator check: DescribeShareGroupOffsets is routed to
		// the share coordinator.
		if c.coordinator(rg.GroupID).node != creq.cc.b.node {
			rsg.ErrorCode = kerr.NotCoordinator.Code
			resp.Groups = append(resp.Groups, rsg)
			continue
		}

		// ACL: require GROUP DESCRIBE.
		if !c.allowedACL(creq, rg.GroupID, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe) {
			rsg.ErrorCode = kerr.GroupAuthorizationFailed.Code
			resp.Groups = append(resp.Groups, rsg)
			continue
		}

		sg := c.shareGroups.get(rg.GroupID)
		func() {
			if sg != nil {
				sg.mu.Lock()
				defer sg.mu.Unlock()
			}

			// Build the list of topics to describe. If Topics is nil,
			// describe all topics the group has state for.
			type topicPartReq struct {
				topic      string
				partitions []int32
			}
			var topicReqs []topicPartReq
			if rg.Topics == nil && sg != nil {
				for topic, parts := range sg.partitions {
					topicReqs = append(topicReqs, topicPartReq{
						topic:      topic,
						partitions: slices.Collect(maps.Keys(parts)),
					})
				}
			} else {
				for j := range rg.Topics {
					rt := &rg.Topics[j]
					topicReqs = append(topicReqs, topicPartReq{
						topic:      rt.Topic,
						partitions: rt.Partitions,
					})
				}
			}

			isDescribeAll := rg.Topics == nil
			for _, tr := range topicReqs {
				rst := kmsg.NewDescribeShareGroupOffsetsResponseGroupTopic()
				rst.Topic = tr.topic
				rst.TopicID = c.data.t2id[tr.topic]

				// ACL: per-topic DESCRIBE check.
				if !c.allowedACL(creq, tr.topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe) {
					if isDescribeAll {
						// Describe-all: silently filter unauthorized topics.
						continue
					}
					for _, partition := range tr.partitions {
						rsp := kmsg.NewDescribeShareGroupOffsetsResponseGroupTopicPartition()
						rsp.Partition = partition
						rsp.ErrorCode = kerr.TopicAuthorizationFailed.Code
						rsp.StartOffset = -1
						rsp.Lag = -1
						rst.Partitions = append(rst.Partitions, rsp)
					}
					rsg.Topics = append(rsg.Topics, rst)
					continue
				}

				for _, partition := range tr.partitions {
					rsp := kmsg.NewDescribeShareGroupOffsetsResponseGroupTopicPartition()
					rsp.Partition = partition

					pd, ok := c.data.tps.getp(tr.topic, partition)
					if !ok {
						// Java treats missing topics as absence of data
						// (no error), not an error condition.
						rsp.StartOffset = -1
						rsp.Lag = -1
						rst.Partitions = append(rst.Partitions, rsp)
						continue
					}

					rsp.LeaderEpoch = pd.epoch
					if sg == nil {
						// Group doesn't exist -- no share state.
						rsp.StartOffset = -1
						rsp.Lag = -1
					} else if sp, ok := sg.partitions.getp(tr.topic, partition); !ok {
						// No share state yet -- SPSO not initialized.
						rsp.StartOffset = -1
						rsp.Lag = -1
					} else {
						rsp.StartOffset = sp.spso
						// Lag = HWM - SPSO - deliveryComplete, where
						// deliveryComplete counts records between SPSO
						// and HWM that are already acknowledged/archived.
						// Records below SPSO are cleaned up by advanceSPSO,
						// so all map entries are >= SPSO.
						deliveryComplete := int64(0)
						for off, sr := range sp.records {
							if off < pd.highWatermark &&
								(sr.state == shareRecordAcknowledged || sr.state == shareRecordArchived) {
								deliveryComplete++
							}
						}
						rsp.Lag = max(0, pd.highWatermark-sp.spso-deliveryComplete)
					}
					rst.Partitions = append(rst.Partitions, rsp)
				}
				rsg.Topics = append(rsg.Topics, rst)
			}
		}()

		resp.Groups = append(resp.Groups, rsg)
	}

	return resp, nil
}
