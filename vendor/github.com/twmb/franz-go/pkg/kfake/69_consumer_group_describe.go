package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ConsumerGroupDescribe: v0-1 (KIP-848)
//
// Version notes:
// * v0: Initial version
// * v1: SubscribedTopicRegex, MemberType

func init() { regKey(69, 0, 1) }

func (c *Cluster) handleConsumerGroupDescribe(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.ConsumerGroupDescribeRequest)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	return c.groups.handleConsumerGroupDescribe(creq), nil
}
