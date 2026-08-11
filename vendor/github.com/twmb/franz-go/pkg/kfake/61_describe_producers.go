package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribeProducers: v0
//
// KIP-664: Describe the state of active producers on partitions.
//
// Behavior:
// * ACL: READ on TOPIC
// * Returns active transactional producers for each partition
//
// Version notes:
// * v0 only

func init() { regKey(61, 0, 0) }

func (c *Cluster) handleDescribeProducers(creq *clientReq) (kmsg.Response, error) {
	if err := c.checkReqVersion(creq.kreq.Key(), creq.kreq.GetVersion()); err != nil {
		return nil, err
	}
	return c.pids.doDescribeProducers(creq), nil
}
