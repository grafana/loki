package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DeleteACLs: v0-3
//
// Behavior:
// * Deletes ACLs matching filters
// * Returns deleted ACLs
// * Requires ALTER on CLUSTER
//
// Version notes:
// * v1: PatternType field added
// * v2: Flexible versions

func init() { regKey(31, 0, 3) }

func (c *Cluster) handleDeleteACLs(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DeleteACLsRequest)
	resp := req.ResponseKind().(*kmsg.DeleteACLsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	clusterAllowed := c.allowedClusterACL(creq, kmsg.ACLOperationAlter)

	for _, rf := range req.Filters {
		result := kmsg.DeleteACLsResponseResult{}
		if !clusterAllowed {
			result.ErrorCode = kerr.ClusterAuthorizationFailed.Code
			result.ErrorMessage = kmsg.StringPtr(kerr.ClusterAuthorizationFailed.Message)
			resp.Results = append(resp.Results, result)
			continue
		}

		filter := aclFilter{
			resourceType: rf.ResourceType,
			pattern:      rf.ResourcePatternType,
			operation:    rf.Operation,
			permission:   rf.PermissionType,
			resourceName: rf.ResourceName,
			principal:    rf.Principal,
			host:         rf.Host,
		}

		for _, a := range c.acls.delete(filter) {
			result.MatchingACLs = append(result.MatchingACLs, kmsg.DeleteACLsResponseResultMatchingACL{
				ResourceType:        a.resourceType,
				ResourceName:        a.resourceName,
				ResourcePatternType: a.pattern,
				Principal:           a.principal,
				Host:                a.host,
				Operation:           a.operation,
				PermissionType:      a.permission,
			})
		}
		resp.Results = append(resp.Results, result)
	}

	c.persistACLsState()
	return resp, nil
}
