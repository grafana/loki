package kfake

import (
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterConfigs: v0-2
//
// Supported resource types:
// * BROKER (2)
// * TOPIC (4)
//
// Behavior:
// * Replaces all configs with the provided set (non-incremental)
// * ValidateOnly mode supported
//
// Version notes:
// * v1: ThrottleMillis
// * v2: Flexible versions
//
// Note: Deprecated in favor of IncrementalAlterConfigs (44)

func init() { regKey(33, 0, 2) }

func (c *Cluster) handleAlterConfigs(creq *clientReq) (kmsg.Response, error) {
	var (
		b    = creq.cc.b
		req  = creq.kreq.(*kmsg.AlterConfigsRequest)
		resp = req.ResponseKind().(*kmsg.AlterConfigsResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	doner := func(n string, t kmsg.ConfigResourceType, errCode int16) {
		st := kmsg.NewAlterConfigsResponseResource()
		st.ResourceName = n
		st.ResourceType = t
		st.ErrorCode = errCode
		resp.Resources = append(resp.Resources, st)
	}

outer:
	for i := range req.Resources {
		rr := &req.Resources[i]
		switch rr.ResourceType {
		case kmsg.ConfigResourceTypeBroker:
			if !c.allowedClusterACL(creq, kmsg.ACLOperationAlterConfigs) {
				doner(rr.ResourceName, rr.ResourceType, kerr.ClusterAuthorizationFailed.Code)
				continue outer
			}
			if rr.ResourceName != "" {
				iid, err := strconv.Atoi(rr.ResourceName)
				if err != nil || int32(iid) != b.node {
					doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
					continue outer
				}
			}
			newBcfgs := make(map[string]*string, len(rr.Configs))
			var invalid bool
			for i := range rr.Configs {
				rc := &rr.Configs[i]
				if !validateBrokerConfig(rc.Name, rc.Value) {
					invalid = true
				}
				newBcfgs[rc.Name] = rc.Value
			}
			if invalid {
				doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
				continue
			}
			doner(rr.ResourceName, rr.ResourceType, 0)
			if req.ValidateOnly {
				continue
			}
			c.storeBcfgs(newBcfgs)
			c.persistBrokerConfigsState()

		case kmsg.ConfigResourceTypeTopic:
			if !c.allowedACL(creq, rr.ResourceName, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationAlterConfigs) {
				doner(rr.ResourceName, rr.ResourceType, kerr.TopicAuthorizationFailed.Code)
				continue
			}
			if _, ok := c.data.tps.gett(rr.ResourceName); !ok {
				doner(rr.ResourceName, rr.ResourceType, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			var invalid bool
			for i := range rr.Configs {
				rc := &rr.Configs[i]
				invalid = invalid || !c.data.setTopicConfig(rr.ResourceName, rc.Name, rc.Value, true)
			}
			if invalid {
				doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
				continue
			}
			doner(rr.ResourceName, rr.ResourceType, 0)
			if req.ValidateOnly {
				continue
			}
			delete(c.data.tcfgs, rr.ResourceName)
			for i := range rr.Configs {
				rc := &rr.Configs[i]
				c.data.setTopicConfig(rr.ResourceName, rc.Name, rc.Value, false)
			}
			c.persistTopicsState()

		default:
			doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
		}
	}

	c.refreshCompactTicker()
	return resp, nil
}
