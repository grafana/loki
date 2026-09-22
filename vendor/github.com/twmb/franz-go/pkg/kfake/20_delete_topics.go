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

	// A fault can answer a topic before the work runs. The work's own
	// answer for that topic must not add an entry or replace the code.
	type answerKey struct {
		t  string
		id uuid
	}
	answered := make(map[answerKey]int)
	donet := func(t *string, id uuid, errCode int16) *kmsg.DeleteTopicsResponseTopic {
		k := answerKey{id: id}
		if t != nil {
			k.t = *t
		}
		if i, ok := answered[k]; ok {
			return &resp.Topics[i]
		}
		answered[k] = len(resp.Topics)
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
			c.deleteTopic(td.topic, td.id)
		}
		if len(toDeletes) > 0 {
			c.notifyTopicChange()
			c.refreshCompactTicker()
			c.persistTopicsState()
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
		e := c.deny(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDescribe, faultKey{topic: topic})
		if e == nil {
			e = c.deny(creq, topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationDelete, faultKey{topic: topic})
		}
		if e != nil {
			donet(&topic, id, e.Code)
			if creq.skipsWork(e) { // a timed-out delete still deletes the topic
				continue
			}
		}
		if _, ok := c.data.tps.gett(topic); !ok {
			if rt.Topic != nil {
				donet(&topic, id, kerr.UnknownTopicOrPartition.Code)
			} else {
				donet(&topic, id, kerr.UnknownTopicID.Code)
			}
			continue
		}

		donet(&topic, id, 0)
		toDeletes = append(toDeletes, toDelete{topic, id})
	}

	return resp, nil
}

// deleteTopic wakes the topic's watching fetchers and tears the topic down:
// its data, its files, and everything else keyed by the topic. The caller
// runs notifyTopicChange, refreshCompactTicker and persistTopicsState once,
// after its whole batch.
func (c *Cluster) deleteTopic(topic string, id uuid) {
	t, ok := c.data.tps.gett(topic)
	if !ok {
		return
	}
	for _, pd := range t {
		for watch := range pd.watch {
			watch.deleted()
		}
	}
	// Close active segment files before removing partition directories.
	for p, pd := range t {
		pd.closeAllFiles(false)
		pdir := partDir(c.storageDir, topic, p)
		if err := c.fs.RemoveAll(pdir); err != nil {
			c.cfg.logger.Logf(LogLevelWarn, "delete topic %s partition %d dir: %v", topic, p, err)
		}
	}
	delete(c.data.tps, topic)
	delete(c.data.id2t, id)
	delete(c.data.t2id, topic)
	delete(c.data.treplicas, topic)
	delete(c.data.tcfgs, topic)
	delete(c.data.tnorms, normalizeTopicName(topic))
	// Producer state is per-log and dies with the topic: a recreated topic
	// rehydrates empty state, and handleProduce decides what each version
	// accepts from a producer it has no state for. Transactional
	// REGISTRATIONS survive (the coordinator is name-keyed on a real
	// broker); endTx re-resolves current partition data when writing
	// markers.
	for _, pidinf := range c.pids.ids {
		delete(pidinf.windows, topic)
	}
	// Share-partition state is topic-ID-keyed on a real broker and dies
	// with the topic; kfake keys by name, so clear it explicitly: a
	// recreated topic starts share consumption fresh (SPSO per group
	// config, no acquired records).
	for _, sg := range c.shareGroups.gs {
		delete(sg.partitions, topic)
	}
	c.dropGroupCommits(topic)
}
