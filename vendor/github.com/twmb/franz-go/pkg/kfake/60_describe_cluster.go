package kfake

import (
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribeCluster: v0-2
//
// KIP-700: Admin API to describe cluster.
//
// Version notes:
// * v1: EndpointType
// * v2: IncludeFencedBrokers, IsFenced

func init() { regKey(60, 0, 2) }

func (c *Cluster) handleDescribeCluster(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DescribeClusterRequest)
	resp := req.ResponseKind().(*kmsg.DescribeClusterResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	for _, b := range c.bs {
		sb := kmsg.NewDescribeClusterResponseBroker()
		sb.NodeID = b.node
		sb.Host, sb.Port = b.hostport()
		sb.Rack = &brokerRack
		resp.Brokers = append(resp.Brokers, sb)
	}

	resp.ClusterID = c.cfg.clusterID
	resp.ControllerID = c.controller.node

	if req.IncludeClusterAuthorizedOperations {
		resp.ClusterAuthorizedOperations = c.clusterAuthorizedOps(creq)
	}

	return resp, nil
}
