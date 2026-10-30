package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribeTransactions: v0
//
// KIP-664: Describe the state of transactions by transactional ID.
//
// Behavior:
// * ACL: DESCRIBE on TRANSACTIONAL_ID
// * Returns transaction state, timeout, producer ID/epoch, and partitions
//
// Version notes:
// * v0 only

func init() { regKey(65, 0, 0) }

func (c *Cluster) handleDescribeTransactions(creq *clientReq) (kmsg.Response, error) {
	if err := c.checkReqVersion(creq.kreq.Key(), creq.kreq.GetVersion()); err != nil {
		return nil, err
	}
	return c.pids.doDescribeTransactions(creq), nil
}
