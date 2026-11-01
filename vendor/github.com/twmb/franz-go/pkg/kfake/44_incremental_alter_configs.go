package kfake

import (
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// IncrementalAlterConfigs: v0-1
//
// Supported resource types:
// * BROKER (2)
// * TOPIC (4)
//
// Supported operations:
// * SET (0)
// * DELETE (1)
// * APPEND (2)
// * SUBTRACT (3)
//
// Version notes:
// * v0: Initial version
// * v1: Flexible versions

func init() { regKey(44, 0, 1) }

func (c *Cluster) handleIncrementalAlterConfigs(creq *clientReq) (kmsg.Response, error) {
	var (
		b    = creq.cc.b
		req  = creq.kreq.(*kmsg.IncrementalAlterConfigsRequest)
		resp = req.ResponseKind().(*kmsg.IncrementalAlterConfigsResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	doner := func(n string, t kmsg.ConfigResourceType, errCode int16) {
		st := kmsg.NewIncrementalAlterConfigsResponseResource()
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
			old := c.loadBcfgs()
			dup := make(map[string]*string, len(old))
			for k, v := range old {
				dup[k] = v
			}
			var invalid bool
			for i := range rr.Configs {
				rc := &rr.Configs[i]
				switch rc.Op {
				case kmsg.IncrementalAlterConfigOpSet:
					if !validateBrokerConfig(rc.Name, rc.Value) {
						invalid = true
					}
					dup[rc.Name] = rc.Value
				case kmsg.IncrementalAlterConfigOpDelete:
					delete(dup, rc.Name)
				case kmsg.IncrementalAlterConfigOpAppend:
					if !isListConfig(rc.Name) {
						invalid = true
					} else {
						dup[rc.Name] = configListAppend(dup[rc.Name], rc.Value)
					}
				case kmsg.IncrementalAlterConfigOpSubtract:
					if !isListConfig(rc.Name) {
						invalid = true
					} else {
						dup[rc.Name] = configListSubtract(dup[rc.Name], rc.Value)
					}
				default:
					invalid = true
				}
			}
			if invalid {
				doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
				continue
			}
			doner(rr.ResourceName, rr.ResourceType, 0)
			if req.ValidateOnly {
				continue
			}
			c.storeBcfgs(dup)
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
				switch rc.Op {
				case kmsg.IncrementalAlterConfigOpSet:
					invalid = invalid || !c.data.setTopicConfig(rr.ResourceName, rc.Name, rc.Value, true)
				case kmsg.IncrementalAlterConfigOpDelete:
				case kmsg.IncrementalAlterConfigOpAppend, kmsg.IncrementalAlterConfigOpSubtract:
					if !isListConfig(rc.Name) {
						invalid = true
					}
				default:
					invalid = true
				}
			}
			if invalid {
				doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
				continue
			}
			doner(rr.ResourceName, rr.ResourceType, 0)
			if req.ValidateOnly {
				continue
			}
			for i := range rr.Configs {
				rc := &rr.Configs[i]
				switch rc.Op {
				case kmsg.IncrementalAlterConfigOpSet:
					c.data.setTopicConfig(rr.ResourceName, rc.Name, rc.Value, false)
				case kmsg.IncrementalAlterConfigOpDelete:
					delete(c.data.tcfgs[rr.ResourceName], rc.Name)
				case kmsg.IncrementalAlterConfigOpAppend:
					current := c.data.tcfgs[rr.ResourceName][rc.Name]
					c.data.setTopicConfig(rr.ResourceName, rc.Name, configListAppend(current, rc.Value), false)
				case kmsg.IncrementalAlterConfigOpSubtract:
					current := c.data.tcfgs[rr.ResourceName][rc.Name]
					c.data.setTopicConfig(rr.ResourceName, rc.Name, configListSubtract(current, rc.Value), false)
				}
			}
			c.persistTopicsState()

		case kmsg.ConfigResourceTypeGroupConfig:
			// Group configs are scalar (e.g. share.auto.offset.reset);
			// the protocol's Append/Subtract ops are list-valued and
			// not meaningful here. Reject the request if any config
			// uses an unsupported op or an unknown config name.
			//
			// Per-group config names are UNPREFIXED -- the "group."
			// prefix is only for broker-level defaults. Real Kafka
			// returns INVALID_CONFIG for unknown names.
			var invalid bool
			for i := range rr.Configs {
				switch rr.Configs[i].Op {
				case kmsg.IncrementalAlterConfigOpSet, kmsg.IncrementalAlterConfigOpDelete:
				default:
					invalid = true
				}
				if !validGroupConfigs[rr.Configs[i].Name] {
					invalid = true
				}
			}
			if invalid {
				doner(rr.ResourceName, rr.ResourceType, kerr.InvalidConfig.Code)
				continue
			}
			doner(rr.ResourceName, rr.ResourceType, 0)
			if req.ValidateOnly {
				continue
			}
			if c.groupConfigs == nil {
				c.groupConfigs = make(map[string]map[string]*string)
			}
			gc := c.groupConfigs[rr.ResourceName]
			if gc == nil {
				gc = make(map[string]*string)
				c.groupConfigs[rr.ResourceName] = gc
			}
			for i := range rr.Configs {
				rc := &rr.Configs[i]
				switch rc.Op {
				case kmsg.IncrementalAlterConfigOpSet:
					gc[rc.Name] = rc.Value
				case kmsg.IncrementalAlterConfigOpDelete:
					delete(gc, rc.Name)
				case kmsg.IncrementalAlterConfigOpAppend, kmsg.IncrementalAlterConfigOpSubtract:
					// rejected above
				}
			}

		default:
			doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
		}
	}

	c.refreshCompactTicker()
	return resp, nil
}
