package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// OffsetCommit: v0-10
//
// Version notes:
// * v1: Generation, MemberID
// * v2: RetentionTimeMillis (removed in v5)
// * v3: ThrottleMillis
// * v6: LeaderEpoch in request
// * v7: InstanceID - currently returns error if set
// * v8: Flexible versions
// * v10: TopicID replaces Topic (KIP-848 / KAFKA-19186)

func init() { regKey(8, 0, 10) }

func (c *Cluster) handleOffsetCommit(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.OffsetCommitRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// v10: resolve TopicIDs to topic names. Topics with unknown IDs
	// get per-partition UNKNOWN_TOPIC_ID errors; valid topics are
	// passed through to the group goroutine.
	if req.Version >= 10 {
		var errTopics []kmsg.OffsetCommitResponseTopic
		valid := req.Topics[:0]
		for i := range req.Topics {
			t := &req.Topics[i]
			name, ok := c.data.id2t[t.TopicID]
			if !ok {
				st := kmsg.NewOffsetCommitResponseTopic()
				st.TopicID = t.TopicID
				for _, p := range t.Partitions {
					sp := kmsg.NewOffsetCommitResponseTopicPartition()
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

		if len(errTopics) > 0 && len(req.Topics) == 0 {
			// All topics had unknown IDs; return errors directly.
			resp := req.ResponseKind().(*kmsg.OffsetCommitResponse)
			resp.Topics = errTopics
			return resp, nil
		}
		if len(errTopics) > 0 {
			// Stash error responses so the group handler can
			// merge them into the final response.
			creq.offsetCommitErrTopics = errTopics
		}
	}

	c.groups.handleOffsetCommit(creq)
	return nil, nil
}
