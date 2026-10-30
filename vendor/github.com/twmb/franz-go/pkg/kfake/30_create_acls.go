package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// CreateACLs: v0-3
//
// Behavior:
// * Creates ACLs
// * Requires ALTER on CLUSTER
//
// Version notes:
// * v1: PatternType field added
// * v2: Flexible versions

func init() { regKey(30, 0, 3) }

func (c *Cluster) handleCreateACLs(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.CreateACLsRequest)
	resp := req.ResponseKind().(*kmsg.CreateACLsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	clusterAllowed := c.allowedClusterACL(creq, kmsg.ACLOperationAlter)

	for _, cr := range req.Creations {
		result := kmsg.CreateACLsResponseResult{}
		if !clusterAllowed {
			result.ErrorCode = kerr.ClusterAuthorizationFailed.Code
			result.ErrorMessage = kmsg.StringPtr(kerr.ClusterAuthorizationFailed.Message)
			resp.Results = append(resp.Results, result)
			continue
		}

		if err := validateACLCreation(cr); err != "" {
			result.ErrorCode = kerr.InvalidRequest.Code
			result.ErrorMessage = kmsg.StringPtr(err)
			resp.Results = append(resp.Results, result)
			continue
		}

		c.acls.add(acl{
			principal:    cr.Principal,
			host:         cr.Host,
			resourceType: cr.ResourceType,
			resourceName: cr.ResourceName,
			pattern:      cr.ResourcePatternType,
			operation:    cr.Operation,
			permission:   cr.PermissionType,
		})
		resp.Results = append(resp.Results, result)
	}

	c.persistACLsState()
	return resp, nil
}

func validateACLCreation(cr kmsg.CreateACLsRequestCreation) string {
	switch {
	case cr.ResourceType < kmsg.ACLResourceTypeTopic || cr.ResourceType > kmsg.ACLResourceTypeTransactionalId:
		return "invalid resource type"
	case cr.ResourcePatternType != kmsg.ACLResourcePatternTypeLiteral && cr.ResourcePatternType != kmsg.ACLResourcePatternTypePrefixed:
		return "invalid pattern type"
	case cr.Operation < kmsg.ACLOperationAll || cr.Operation > kmsg.ACLOperationIdempotentWrite:
		return "invalid operation"
	case cr.PermissionType != kmsg.ACLPermissionTypeDeny && cr.PermissionType != kmsg.ACLPermissionTypeAllow:
		return "invalid permission type"
	}
	return ""
}
