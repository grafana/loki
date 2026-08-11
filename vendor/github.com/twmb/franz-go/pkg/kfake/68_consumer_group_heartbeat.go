package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ConsumerGroupHeartbeat: v0-1 (KIP-848)
//
// Version notes:
// * v0: Initial version
// * v1: SubscribedTopicRegex, MemberType, fully stabilized

func init() { regKey(68, 0, 1) }

func (c *Cluster) handleConsumerGroupHeartbeat(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.ConsumerGroupHeartbeatRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	c.groups.handleConsumerGroupHeartbeat(creq)
	return nil, nil
}
