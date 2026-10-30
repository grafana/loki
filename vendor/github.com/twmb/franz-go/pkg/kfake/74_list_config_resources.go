package kfake

import (
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// ListConfigResources: v0-1
//
// KIP-1000 introduced the request as ListClientMetricsResources.
// KIP-1142 renamed it to ListConfigResources and expanded it to
// cover topics, brokers, broker loggers, client metrics, and groups.
//
// Behavior:
// * v0: only supports CLIENT_METRICS (kfake has none, so returns empty)
// * v1: supports TOPIC, BROKER, BROKER_LOGGER, CLIENT_METRICS, GROUP
// * Empty ResourceTypes in v1 returns all supported types
// * Unsupported resource types return UNSUPPORTED_VERSION
//
// Version notes:
// * v1: ResourceTypes filter and per-resource Type in response

func init() { regKey(74, 0, 1) }

func (c *Cluster) handleListConfigResources(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.ListConfigResourcesRequest)
	resp := req.ResponseKind().(*kmsg.ListConfigResourcesResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if !c.allowedClusterACL(creq, kmsg.ACLOperationDescribeConfigs) {
		resp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
		return resp, nil
	}

	// Supported types per version (matches Kafka):
	// v0: CLIENT_METRICS
	// v1: TOPIC, BROKER, BROKER_LOGGER, CLIENT_METRICS, GROUP
	supported := map[int8]bool{
		int8(kmsg.ConfigResourceTypeClientMetrics): true,
	}
	if req.Version >= 1 {
		supported[int8(kmsg.ConfigResourceTypeTopic)] = true
		supported[int8(kmsg.ConfigResourceTypeBroker)] = true
		supported[int8(kmsg.ConfigResourceTypeBrokerLogger)] = true
		supported[int8(kmsg.ConfigResourceTypeGroupConfig)] = true
	}

	types := req.ResourceTypes
	if len(types) == 0 {
		types = make([]int8, 0, len(supported))
		for t := range supported {
			types = append(types, t)
		}
	}
	wanted := make(map[int8]bool, len(types))
	for _, t := range types {
		if !supported[t] {
			resp.ErrorCode = kerr.UnsupportedVersion.Code
			return resp, nil
		}
		wanted[t] = true
	}

	add := func(name string, t kmsg.ConfigResourceType) {
		r := kmsg.NewListConfigResourcesResponseConfigResource()
		r.Name = name
		r.Type = int8(t)
		resp.ConfigResources = append(resp.ConfigResources, r)
	}

	if wanted[int8(kmsg.ConfigResourceTypeTopic)] {
		for topic := range c.data.tps {
			add(topic, kmsg.ConfigResourceTypeTopic)
		}
	}
	if wanted[int8(kmsg.ConfigResourceTypeBroker)] {
		for _, b := range c.bs {
			add(strconv.Itoa(int(b.node)), kmsg.ConfigResourceTypeBroker)
		}
	}
	if wanted[int8(kmsg.ConfigResourceTypeBrokerLogger)] {
		for _, b := range c.bs {
			add(strconv.Itoa(int(b.node)), kmsg.ConfigResourceTypeBrokerLogger)
		}
	}
	if wanted[int8(kmsg.ConfigResourceTypeGroupConfig)] {
		for group := range c.groupConfigs {
			add(group, kmsg.ConfigResourceTypeGroupConfig)
		}
	}
	// ClientMetrics: kfake has no client-metrics resources.

	return resp, nil
}
