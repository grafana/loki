package kfake

import (
	"sort"
	"strings"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterClientQuotas: v0-1
//
// KIP-546: Client quota configuration APIs.
//
// Behavior:
// * ACL: ALTER_CONFIGS on CLUSTER
// * Stores quota configurations (not enforced by kfake)
// * Supports set and remove operations
//
// Version notes:
// * v0: Initial version
// * v1: Flexible versions

func init() { regKey(49, 0, 1) }

func (c *Cluster) handleAlterClientQuotas(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.AlterClientQuotasRequest)
	resp := req.ResponseKind().(*kmsg.AlterClientQuotasResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if !c.allowedClusterACL(creq, kmsg.ACLOperationAlterConfigs) {
		for _, entry := range req.Entries {
			re := kmsg.NewAlterClientQuotasResponseEntry()
			re.ErrorCode = kerr.ClusterAuthorizationFailed.Code
			for _, e := range entry.Entity {
				ee := kmsg.NewAlterClientQuotasResponseEntryEntity()
				ee.Type = e.Type
				ee.Name = e.Name
				re.Entity = append(re.Entity, ee)
			}
			resp.Entries = append(resp.Entries, re)
		}
		return resp, nil
	}

	for _, entry := range req.Entries {
		re := kmsg.NewAlterClientQuotasResponseEntry()
		for _, e := range entry.Entity {
			ee := kmsg.NewAlterClientQuotasResponseEntryEntity()
			ee.Type = e.Type
			ee.Name = e.Name
			re.Entity = append(re.Entity, ee)
		}

		if !req.ValidateOnly {
			// Build quota key and entity
			qe := makeQuotaEntity(entry.Entity)
			key := qe.key()

			// Get or create quota entry
			quota, exists := c.quotas[key]
			if !exists {
				quota = quotaEntry{
					entity: qe,
					values: make(map[string]float64),
				}
			}

			// Apply operations
			for _, op := range entry.Ops {
				if op.Remove {
					delete(quota.values, op.Key)
				} else {
					quota.values[op.Key] = op.Value
				}
			}

			// Store or delete quota entry
			if len(quota.values) == 0 {
				delete(c.quotas, key)
			} else {
				c.quotas[key] = quota
			}
		}

		resp.Entries = append(resp.Entries, re)
	}

	if !req.ValidateOnly {
		c.persistQuotasState()
	}
	return resp, nil
}

// quotaEntityComponent represents a single entity component (type + name).
type quotaEntityComponent struct {
	entityType string
	name       *string
}

// quotaEntity is an ordered list of entity components.
type quotaEntity []quotaEntityComponent

// quotaEntry stores quota values for an entity.
type quotaEntry struct {
	entity quotaEntity
	values map[string]float64
}

func makeQuotaEntity(entities []kmsg.AlterClientQuotasRequestEntryEntity) quotaEntity {
	qe := make(quotaEntity, len(entities))
	for i, e := range entities {
		qe[i] = quotaEntityComponent{
			entityType: e.Type,
			name:       e.Name,
		}
	}
	// Sort by entity type for consistent key generation
	sort.Slice(qe, func(i, j int) bool {
		return qe[i].entityType < qe[j].entityType
	})
	return qe
}

// key generates a unique string key for the quota entity.
func (qe quotaEntity) key() string {
	var parts []string
	for _, e := range qe {
		name := "<default>"
		if e.name != nil {
			name = *e.name
		}
		parts = append(parts, e.entityType+"="+name)
	}
	return strings.Join(parts, ",")
}
