package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ListTransactions: v0-2
//
// KIP-664: List transactions, optionally filtered by state or producer ID.
//
// Behavior:
// * ACL: DESCRIBE on TRANSACTIONAL_ID (filters results to only those allowed)
// * Returns list of transactions with their state
//
// Version notes:
// * v0: Initial version
// * v1: DurationFilterMillis
// * v2: TransactionalIDPattern (regex filter)

func init() { regKey(66, 0, 2) }

func (c *Cluster) handleListTransactions(creq *clientReq) (kmsg.Response, error) {
	if err := c.checkReqVersion(creq.kreq.Key(), creq.kreq.GetVersion()); err != nil {
		return nil, err
	}
	return c.pids.doListTransactions(creq), nil
}
