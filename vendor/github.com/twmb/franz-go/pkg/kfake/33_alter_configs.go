package kfake

import (
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(33, 0, 2) }

func (c *Cluster) handleAlterConfigs(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.AlterConfigsRequest)
	resp := req.ResponseKind().(*kmsg.AlterConfigsResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	doner := func(n string, t kmsg.ConfigResourceType, errCode int16) *kmsg.AlterConfigsResponseResource {
		st := kmsg.NewAlterConfigsResponseResource()
		st.ResourceName = n
		st.ResourceType = t
		st.ErrorCode = errCode
		resp.Resources = append(resp.Resources, st)
		return &resp.Resources[len(resp.Resources)-1]
	}

outer:
	for i := range req.Resources {
		rr := &req.Resources[i]
		switch rr.ResourceType {
		case kmsg.ConfigResourceTypeBroker:
			id := int32(-1)
			if rr.ResourceName != "" {
				iid, err := strconv.Atoi(rr.ResourceName)
				id = int32(iid)
				if err != nil || id != b.node {
					doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
					continue outer
				}
			}
			var invalid bool
			for i := range rr.Configs {
				rc := &rr.Configs[i]
				invalid = invalid || !c.setBrokerConfig(rc.Name, rc.Value, true)
			}
			if invalid {
				doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
				continue
			}
			doner(rr.ResourceName, rr.ResourceType, 0)
			if req.ValidateOnly {
				continue
			}
			c.bcfgs = make(map[string]*string)
			for i := range rr.Configs {
				rc := &rr.Configs[i]
				c.setBrokerConfig(rc.Name, rc.Value, false)
			}

		case kmsg.ConfigResourceTypeTopic:
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

		default:
			doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
		}
	}

	return resp, nil
}
