package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribeClientQuotas: v0-1
//
// KIP-546: Client quota configuration APIs.
//
// Behavior:
// * ACL: DESCRIBE_CONFIGS on CLUSTER
// * Returns stored quota configurations matching the filter
// * Quotas are stored but not enforced by kfake
//
// Version notes:
// * v0: Initial version
// * v1: Flexible versions

func init() { regKey(48, 0, 1) }

func (c *Cluster) handleDescribeClientQuotas(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DescribeClientQuotasRequest)
	resp := req.ResponseKind().(*kmsg.DescribeClientQuotasResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if !c.allowedClusterACL(creq, kmsg.ACLOperationDescribeConfigs) {
		resp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
		return resp, nil
	}

	for _, quota := range c.quotas {
		if matchesQuotaFilter(quota, req.Components, req.Strict) {
			entry := kmsg.NewDescribeClientQuotasResponseEntry()
			for _, e := range quota.entity {
				ee := kmsg.NewDescribeClientQuotasResponseEntryEntity()
				ee.Type = e.entityType
				ee.Name = e.name
				entry.Entity = append(entry.Entity, ee)
			}
			for k, v := range quota.values {
				ev := kmsg.NewDescribeClientQuotasResponseEntryValue()
				ev.Key = k
				ev.Value = v
				entry.Values = append(entry.Values, ev)
			}
			resp.Entries = append(resp.Entries, entry)
		}
	}

	return resp, nil
}

func matchesQuotaFilter(quota quotaEntry, components []kmsg.DescribeClientQuotasRequestComponent, strict bool) bool {
	if len(components) == 0 {
		return true
	}

	// Build a map of entity types in this quota
	entityMap := make(map[string]*string)
	for _, e := range quota.entity {
		entityMap[e.entityType] = e.name
	}

	// Check each filter component
	for _, comp := range components {
		name, exists := entityMap[comp.EntityType]

		switch comp.MatchType {
		case 0: // Exact match on name
			if !exists {
				return false
			}
			if comp.Match == nil {
				// Match default: name must be nil
				if name != nil {
					return false
				}
			} else {
				// Match specific: name must equal comp.Match
				if name == nil || *name != *comp.Match {
					return false
				}
			}
		case 1: // Default match (name is null)
			if !exists || name != nil {
				return false
			}
		case 2: // Any specified (name is non-null)
			if !exists || name == nil {
				return false
			}
		default:
			return false
		}
	}

	// In strict mode, quota must not have entity types not in the filter
	if strict {
		filterTypes := make(map[string]struct{})
		for _, comp := range components {
			filterTypes[comp.EntityType] = struct{}{}
		}
		for _, e := range quota.entity {
			if _, ok := filterTypes[e.entityType]; !ok {
				return false
			}
		}
	}

	return true
}
