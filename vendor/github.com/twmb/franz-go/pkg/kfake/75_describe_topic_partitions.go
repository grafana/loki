package kfake

import (
	"slices"
	"strings"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribeTopicPartitions: v0
//
// KIP-966: Paginated topic metadata with richer partition info
// (EligibleLeaderReplicas, LastKnownELR).
//
// Behavior:
// * Empty Topics returns all topics (alphabetically sorted)
// * Cursor paginates by (topic, partition); requested topics must contain
//   the cursor topic when topics are explicitly listed
// * ResponsePartitionLimit caps the total partitions across all topics
// * Unknown topics get UNKNOWN_TOPIC_OR_PARTITION; auto-create is not used
// * Topics not authorized for DESCRIBE return TOPIC_AUTHORIZATION_FAILED
//   when explicitly requested, and are silently omitted otherwise

func init() { regKey(75, 0, 0) }

func (c *Cluster) handleDescribeTopicPartitions(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DescribeTopicPartitionsRequest)
	resp := req.ResponseKind().(*kmsg.DescribeTopicPartitionsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	var cursorTopic string
	var cursorPartition int32
	if req.Cursor != nil {
		cursorTopic = req.Cursor.Topic
		cursorPartition = req.Cursor.Partition
		if cursorPartition < 0 {
			return nil, kerr.InvalidRequest
		}
	}

	fetchAll := len(req.Topics) == 0

	// Build the set of candidate topic names.
	var topics []string
	if fetchAll {
		for t := range c.data.tps {
			if t >= cursorTopic {
				topics = append(topics, t)
			}
		}
	} else {
		seen := make(map[string]bool, len(req.Topics))
		cursorListed := cursorTopic == ""
		for _, rt := range req.Topics {
			if seen[rt.Topic] {
				continue
			}
			seen[rt.Topic] = true
			if rt.Topic == cursorTopic {
				cursorListed = true
			}
			if rt.Topic >= cursorTopic {
				topics = append(topics, rt.Topic)
			}
		}
		if !cursorListed {
			return nil, kerr.InvalidRequest
		}
	}
	slices.Sort(topics)

	limit := req.ResponsePartitionLimit
	if limit < 1 {
		limit = 1
	}
	const hardLimit = 2000 // matches Kafka's maxRequestPartitionSizeLimit default
	if limit > hardLimit {
		limit = hardLimit
	}

	emitted := int32(0)
	var nextCursor *kmsg.DescribeTopicPartitionsResponseNextCursor

	addTopic := func(name string, errCode int16, id uuid, internal bool) *kmsg.DescribeTopicPartitionsResponseTopic {
		st := kmsg.NewDescribeTopicPartitionsResponseTopic()
		st.ErrorCode = errCode
		st.Topic = kmsg.StringPtr(name)
		st.TopicID = id
		st.IsInternal = internal
		st.AuthorizedOperations = c.topicAuthorizedOps(creq, name)
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}

	for _, topic := range topics {
		if emitted >= limit {
			nc := kmsg.NewDescribeTopicPartitionsResponseNextCursor()
			nextCursor = &nc
			nextCursor.Topic = topic
			nextCursor.Partition = 0
			break
		}

		if !c.allowedACL(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe) {
			if !fetchAll {
				addTopic(topic, kerr.TopicAuthorizationFailed.Code, noID, false)
			}
			continue
		}

		ps, ok := c.data.tps.gett(topic)
		if !ok {
			addTopic(topic, kerr.UnknownTopicOrPartition.Code, noID, false)
			continue
		}

		internal := false
		if v, ok := c.data.tcfgs[topic]["kfake.is_internal"]; ok && v != nil && *v == "true" {
			internal = true
		}
		st := addTopic(topic, 0, c.data.t2id[topic], internal)

		partStart := int32(0)
		if topic == cursorTopic {
			partStart = cursorPartition
		}

		// Sort partitions by ID for stable pagination.
		pids := make([]int32, 0, len(ps))
		for p := range ps {
			if p >= partStart {
				pids = append(pids, p)
			}
		}
		slices.Sort(pids)

		nreplicas := c.data.treplicas[topic]
		if nreplicas > len(c.bs) {
			nreplicas = len(c.bs)
		}

		for _, p := range pids {
			if emitted >= limit {
				nc := kmsg.NewDescribeTopicPartitionsResponseNextCursor()
				nextCursor = &nc
				nextCursor.Topic = topic
				nextCursor.Partition = p
				break
			}
			pd := ps[p]
			sp := kmsg.NewDescribeTopicPartitionsResponseTopicPartition()
			sp.Partition = p
			sp.LeaderID = pd.leader.node
			sp.LeaderEpoch = pd.epoch
			for i := 0; i < nreplicas; i++ {
				idx := (pd.leader.bsIdx + i) % len(c.bs)
				sp.Replicas = append(sp.Replicas, c.bs[idx].node)
			}
			sp.ISR = sp.Replicas
			st.Partitions = append(st.Partitions, sp)
			emitted++
		}
		if nextCursor != nil {
			break
		}
	}

	resp.NextCursor = nextCursor
	// Keep topics deterministically ordered in the response.
	slices.SortFunc(resp.Topics, func(a, b kmsg.DescribeTopicPartitionsResponseTopic) int {
		return strings.Compare(*a.Topic, *b.Topic)
	})

	return resp, nil
}
