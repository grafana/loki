package kfake

import (
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(32, 0, 4) }

func (c *Cluster) handleDescribeConfigs(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.DescribeConfigsRequest)
	resp := req.ResponseKind().(*kmsg.DescribeConfigsResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	doner := func(n string, t kmsg.ConfigResourceType, errCode int16) *kmsg.DescribeConfigsResponseResource {
		st := kmsg.NewDescribeConfigsResponseResource()
		st.ResourceName = n
		st.ResourceType = t
		st.ErrorCode = errCode
		resp.Resources = append(resp.Resources, st)
		return &resp.Resources[len(resp.Resources)-1]
	}

	rfn := func(r *kmsg.DescribeConfigsResponseResource) func(k string, v *string, src kmsg.ConfigSource, sensitive bool) {
		nameIdxs := make(map[string]int)
		return func(k string, v *string, src kmsg.ConfigSource, sensitive bool) {
			rc := kmsg.NewDescribeConfigsResponseResourceConfig()
			rc.Name = k
			rc.Value = v
			rc.Source = src
			rc.ReadOnly = rc.Source == kmsg.ConfigSourceStaticBrokerConfig
			rc.IsDefault = rc.Source == kmsg.ConfigSourceDefaultConfig || rc.Source == kmsg.ConfigSourceStaticBrokerConfig
			rc.IsSensitive = sensitive

			// We walk configs from static to default to dynamic,
			// if this config already exists previously, we move
			// the previous config to a synonym and update the
			// previous config.
			if idx, ok := nameIdxs[k]; ok {
				prior := r.Configs[idx]
				syn := kmsg.NewDescribeConfigsResponseResourceConfigConfigSynonym()
				syn.Name = prior.Name
				syn.Value = prior.Value
				syn.Source = prior.Source
				rc.ConfigSynonyms = append([]kmsg.DescribeConfigsResponseResourceConfigConfigSynonym{syn}, prior.ConfigSynonyms...)
				r.Configs[idx] = rc
				return
			}
			nameIdxs[k] = len(r.Configs)
			r.Configs = append(r.Configs, rc)
		}
	}
	filter := func(rr *kmsg.DescribeConfigsRequestResource, r *kmsg.DescribeConfigsResponseResource) {
		if rr.ConfigNames == nil {
			return
		}
		names := make(map[string]struct{})
		for _, name := range rr.ConfigNames {
			names[name] = struct{}{}
		}
		keep := r.Configs[:0]
		for _, rc := range r.Configs {
			if _, ok := names[rc.Name]; ok {
				keep = append(keep, rc)
			}
		}
		r.Configs = keep
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
			r := doner(rr.ResourceName, rr.ResourceType, 0)
			c.brokerConfigs(id, rfn(r))
			filter(rr, r)

		case kmsg.ConfigResourceTypeTopic:
			if _, ok := c.data.tps.gett(rr.ResourceName); !ok {
				doner(rr.ResourceName, rr.ResourceType, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			r := doner(rr.ResourceName, rr.ResourceType, 0)
			c.data.configs(rr.ResourceName, rfn(r))
			filter(rr, r)

		default:
			doner(rr.ResourceName, rr.ResourceType, kerr.InvalidRequest.Code)
		}
	}

	return resp, nil
}
