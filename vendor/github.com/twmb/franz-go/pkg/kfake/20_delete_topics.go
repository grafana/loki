package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(20, 0, 6) }

func (c *Cluster) handleDeleteTopics(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.DeleteTopicsRequest)
	resp := req.ResponseKind().(*kmsg.DeleteTopicsResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	donet := func(t *string, id uuid, errCode int16) *kmsg.DeleteTopicsResponseTopic {
		st := kmsg.NewDeleteTopicsResponseTopic()
		st.Topic = t
		st.TopicID = id
		st.ErrorCode = errCode
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donets := func(errCode int16) {
		for _, rt := range req.Topics {
			donet(rt.Topic, rt.TopicID, errCode)
		}
	}

	if req.Version <= 5 {
		for _, topic := range req.TopicNames {
			rt := kmsg.NewDeleteTopicsRequestTopic()
			rt.Topic = kmsg.StringPtr(topic)
			req.Topics = append(req.Topics, rt)
		}
	}

	if b != c.controller {
		donets(kerr.NotController.Code)
		return resp, nil
	}
	for _, rt := range req.Topics {
		if rt.TopicID != noID && rt.Topic != nil {
			donets(kerr.InvalidRequest.Code)
			return resp, nil
		}
	}

	type toDelete struct {
		topic string
		id    uuid
	}
	var toDeletes []toDelete
	defer func() {
		for _, td := range toDeletes {
			delete(c.data.tps, td.topic)
			delete(c.data.id2t, td.id)
			delete(c.data.t2id, td.topic)

		}
	}()
	for _, rt := range req.Topics {
		var topic string
		var id uuid
		if rt.Topic != nil {
			topic = *rt.Topic
			id = c.data.t2id[topic]
		} else {
			topic = c.data.id2t[rt.TopicID]
			id = rt.TopicID
		}
		t, ok := c.data.tps.gett(topic)
		if !ok {
			if rt.Topic != nil {
				donet(&topic, id, kerr.UnknownTopicOrPartition.Code)
			} else {
				donet(&topic, id, kerr.UnknownTopicID.Code)
			}
			continue
		}

		donet(&topic, id, 0)
		toDeletes = append(toDeletes, toDelete{topic, id})
		for _, pd := range t {
			for watch := range pd.watch {
				watch.deleted()
			}
		}
	}

	return resp, nil
}
