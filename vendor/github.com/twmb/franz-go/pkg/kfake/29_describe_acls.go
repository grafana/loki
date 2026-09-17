package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// DescribeACLs: v0-3
//
// Behavior:
// * Returns ACLs matching the filter
// * Requires DESCRIBE on CLUSTER
//
// Version notes:
// * v1: PatternType field added
// * v2: Flexible versions

func init() { regKey(29, 0, 3) }

func (c *Cluster) handleDescribeACLs(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.DescribeACLsRequest)
	resp := req.ResponseKind().(*kmsg.DescribeACLsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if !c.allowedClusterACL(creq, kmsg.ACLOperationDescribe) {
		resp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
		return resp, nil
	}

	filter := aclFilter{
		resourceType: req.ResourceType,
		pattern:      req.ResourcePatternType,
		operation:    req.Operation,
		permission:   req.PermissionType,
		resourceName: req.ResourceName,
		principal:    req.Principal,
		host:         req.Host,
	}

	matching := c.acls.describe(filter)

	// Group by resource type + name + pattern
	type resourceKey struct {
		resourceType kmsg.ACLResourceType
		resourceName string
		pattern      kmsg.ACLResourcePatternType
	}
	grouped := make(map[resourceKey][]acl)
	for _, a := range matching {
		key := resourceKey{a.resourceType, a.resourceName, a.pattern}
		grouped[key] = append(grouped[key], a)
	}

	for key, acls := range grouped {
		resource := kmsg.DescribeACLsResponseResource{
			ResourceType:        key.resourceType,
			ResourceName:        key.resourceName,
			ResourcePatternType: key.pattern,
		}
		for _, a := range acls {
			resource.ACLs = append(resource.ACLs, kmsg.DescribeACLsResponseResourceACL{
				Principal:      a.principal,
				Host:           a.host,
				Operation:      a.operation,
				PermissionType: a.permission,
			})
		}
		resp.Resources = append(resp.Resources, resource)
	}

	return resp, nil
}
