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

	type resource struct {
		n string
		t kmsg.ConfigResourceType
	}
	answered := make(map[resource]bool)
	doner := func(n string, t kmsg.ConfigResourceType, errCode int16) {
		// A fault can answer a resource before the work runs. The
		// work's own answer for that resource must not add an entry or
		// replace the code.
		if answered[resource{n, t}] {
			return
		}
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
			if e := c.denyCluster(creq, kmsg.ACLOperationAlterConfigs); e != nil {
				doner(rr.ResourceName, rr.ResourceType, e.Code)
				answered[resource{rr.ResourceName, rr.ResourceType}] = true
				if creq.skipsWork(e) { // a timed-out alter still applies
					continue outer
				}
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
			c.bcfgs = newBcfgs
			c.persistBrokerConfigsState()

		case kmsg.ConfigResourceTypeTopic:
			if e := c.deny(creq, rr.ResourceName, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationAlterConfigs, faultKey{resource: rr.ResourceName}); e != nil {
				doner(rr.ResourceName, rr.ResourceType, e.Code)
				answered[resource{rr.ResourceName, rr.ResourceType}] = true
				if creq.skipsWork(e) { // a timed-out alter still applies
					continue
				}
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
	c.shareGroups.refreshSweepTicker()
	return resp, nil
}
