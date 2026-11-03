package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// These run the same code the matching request handler runs, so they cannot
// drift from it, but they do not go through the request path: faults and
// controls do not see them and ACLs do not apply.

// CreateTopic creates a topic with the given configs, nil for none. A topic
// deleted and created again under the same name gets a new topic ID.
func (c *Cluster) CreateTopic(topic string, partitions int32, configs map[string]string) error {
	rt := kmsg.NewCreateTopicsRequestTopic()
	rt.Topic = topic
	rt.NumPartitions = -1 // the cluster default
	if partitions > 0 {
		rt.NumPartitions = partitions
	}
	rt.ReplicationFactor = -1 // the cluster default
	for k, v := range configs {
		rc := kmsg.NewCreateTopicsRequestTopicConfig()
		rc.Name = k
		rc.Value = kmsg.StringPtr(v)
		rt.Configs = append(rt.Configs, rc)
	}
	var e *kerr.Error
	c.admin(func() {
		if _, _, _, e = c.createTopic(&rt, nil, false); e != nil {
			return
		}
		c.notifyTopicChange()
		c.refreshCompactTicker()
		c.persistTopicsState()
	})
	if e != nil {
		return e
	}
	return nil
}

// DeleteTopic deletes a topic. Same as Kafka, the topic's commits are dropped
// from every group before the delete finishes.
func (c *Cluster) DeleteTopic(topic string) error {
	var e *kerr.Error
	c.admin(func() {
		if _, ok := c.data.tps.gett(topic); !ok {
			e = kerr.UnknownTopicOrPartition
			return
		}
		c.deleteTopic(topic, c.data.t2id[topic])
		c.notifyTopicChange()
		c.refreshCompactTicker()
		c.persistTopicsState()
	})
	if e != nil {
		return e
	}
	return nil
}

// DeleteRecords truncates the partition to offset, as a DeleteRecords
// request would. An offset of -1 truncates to the high watermark.
func (c *Cluster) DeleteRecords(topic string, partition int32, offset int64) error {
	var e *kerr.Error
	c.admin(func() {
		pd, ok := c.data.tps.getp(topic, partition)
		if !ok {
			e = kerr.UnknownTopicOrPartition
			return
		}
		_, e = c.deleteRecords(pd, offset)
	})
	if e != nil {
		return e
	}
	return nil
}
