package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DeleteTopics: v0-6
//
// Behavior:
// * Must be sent to the controller
// * Deletes topics by name (v0-5) or by name/ID (v6+)
// * Wakes any watching fetchers on deletion
//
// Version notes:
// * v1: ThrottleMillis
// * v4: Flexible versions
// * v5: ErrorMessage in response
// * v6: Topics array with TopicID support

func init() { regKey(20, 0, 6) }

func (c *Cluster) handleDeleteTopics(creq *clientReq) (kmsg.Response, error) {
	var (
		b   = creq.cc.b
		req = creq.kreq.(*kmsg.DeleteTopicsRequest)
	)
	resp := req.ResponseKind().(*kmsg.DeleteTopicsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
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
			// Close active segment files before removing partition directories.
			if t, ok := c.data.tps.gett(td.topic); ok {
				for p, pd := range t {
					pd.closeAllFiles(false)
					pdir := partDir(c.storageDir, td.topic, p)
					if err := c.fs.RemoveAll(pdir); err != nil {
						c.cfg.logger.Logf(LogLevelWarn, "delete topic %s partition %d dir: %v", td.topic, p, err)
					}
				}
			}
			delete(c.data.tps, td.topic)
			delete(c.data.id2t, td.id)
			delete(c.data.t2id, td.topic)
			delete(c.data.treplicas, td.topic)
			delete(c.data.tcfgs, td.topic)
			delete(c.data.tnorms, normalizeTopicName(td.topic))
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
		// ACL check: DESCRIBE first (to identify topic), then DELETE
		if !c.allowedACL(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe) {
			donet(&topic, id, kerr.TopicAuthorizationFailed.Code)
			continue
		}
		if !c.allowedACL(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDelete) {
			donet(&topic, id, kerr.TopicAuthorizationFailed.Code)
			continue
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

	if len(toDeletes) > 0 {
		c.notifyTopicChange()
		c.refreshCompactTicker()
		c.persistTopicsState()
	}

	return resp, nil
}
