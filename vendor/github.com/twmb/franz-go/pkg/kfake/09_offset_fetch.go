package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// OffsetFetch: v0-10
//
// Version notes:
// * v2: Topics nullable to fetch all
// * v5: LeaderEpoch in response
// * v6: Flexible versions
// * v7: RequireStable (KIP-447) - returns UNSTABLE_OFFSET_COMMIT if pending txn offsets
// * v8: Multiple groups support in single request
// * v9: MemberID, MemberEpoch for new consumer protocol (KIP-848) - not implemented
// * v10: TopicID replaces Topic (KIP-848 / KAFKA-19186)

func init() { regKey(9, 0, 10) }

func (c *Cluster) handleOffsetFetch(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.OffsetFetchRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	return c.groups.handleOffsetFetch(creq), nil
}
