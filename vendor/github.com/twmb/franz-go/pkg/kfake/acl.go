package kfake

import (
	"net"
	"strings"

	"github.com/twmb/franz-go/pkg/kmsg"
)

const aclClusterName = "kafka-cluster"

// acl represents a single ACL entry using kmsg types directly.
type acl struct {
	principal    string
	host         string
	resourceType kmsg.ACLResourceType
	resourceName string
	pattern      kmsg.ACLResourcePatternType
	operation    kmsg.ACLOperation
	permission   kmsg.ACLPermissionType
}

// clusterACLs manages ACL storage. No mutex needed since all access
// is serialized through the cluster's run() goroutine.
type clusterACLs struct {
	acls []acl
}

func (a *acl) matchesResource(resourceType kmsg.ACLResourceType, resourceName string) bool {
	if a.resourceType != resourceType {
		return false
	}
	switch a.pattern {
	case kmsg.ACLResourcePatternTypeLiteral:
		return a.resourceName == resourceName || a.resourceName == "*"
	case kmsg.ACLResourcePatternTypePrefixed:
		return strings.HasPrefix(resourceName, a.resourceName)
	default:
		return false
	}
}

func (a *acl) matchesPrincipal(principal string) bool {
	return a.principal == principal || a.principal == "User:*"
}

func (a *acl) matchesHost(host string) bool {
	return a.host == host || a.host == "*"
}

func (a *acl) matchesOp(op kmsg.ACLOperation) bool {
	if a.operation == kmsg.ACLOperationAll || a.operation == op {
		return true
	}
	// Implied permissions only for ALLOW:
	// DESCRIBE implied by READ, WRITE, DELETE, ALTER
	// DESCRIBE_CONFIGS implied by ALTER_CONFIGS
	// Implied permissions only apply to ALLOW ACLs.
	if a.permission != kmsg.ACLPermissionTypeAllow {
		return false
	}
	// KIP-253: Describe is implied by Read, Write, Delete, Alter.
	// DescribeConfigs is implied by AlterConfigs. No other ops have
	// implied permissions.
	switch op {
	case kmsg.ACLOperationDescribe:
		switch a.operation {
		case kmsg.ACLOperationRead, kmsg.ACLOperationWrite, kmsg.ACLOperationDelete, kmsg.ACLOperationAlter:
			return true
		default: // no other ops imply Describe
		}
	case kmsg.ACLOperationDescribeConfigs:
		return a.operation == kmsg.ACLOperationAlterConfigs
	default: // only Describe and DescribeConfigs have implied permissions
	}
	return false
}

func (a *clusterACLs) allowed(principal, host, resourceName string, resourceType kmsg.ACLResourceType, op kmsg.ACLOperation) bool {
	var hasAllow bool
	for i := range a.acls {
		acl := &a.acls[i]
		if !acl.matchesResource(resourceType, resourceName) ||
			!acl.matchesPrincipal(principal) ||
			!acl.matchesHost(host) ||
			!acl.matchesOp(op) {
			continue
		}
		if acl.permission == kmsg.ACLPermissionTypeDeny {
			return false
		}
		hasAllow = true
	}
	return hasAllow
}

func (a *clusterACLs) anyAllowed(principal, host string, resourceType kmsg.ACLResourceType, op kmsg.ACLOperation) bool {
	for i := range a.acls {
		acl := &a.acls[i]
		if acl.resourceType != resourceType ||
			!acl.matchesPrincipal(principal) ||
			!acl.matchesHost(host) ||
			!acl.matchesOp(op) {
			continue
		}
		if acl.permission == kmsg.ACLPermissionTypeAllow {
			return true
		}
	}
	return false
}

func (a *clusterACLs) add(newACL acl) {
	for _, existing := range a.acls {
		if existing == newACL {
			return
		}
	}
	a.acls = append(a.acls, newACL)
}

func (a *clusterACLs) delete(filter aclFilter) []acl {
	var deleted []acl
	kept := a.acls[:0]
	for _, acl := range a.acls {
		if filter.matches(&acl) {
			deleted = append(deleted, acl)
		} else {
			kept = append(kept, acl)
		}
	}
	a.acls = kept
	return deleted
}

func (a *clusterACLs) describe(filter aclFilter) []acl {
	var result []acl
	for i := range a.acls {
		if filter.matches(&a.acls[i]) {
			result = append(result, a.acls[i])
		}
	}
	return result
}

type aclFilter struct {
	resourceType kmsg.ACLResourceType
	resourceName *string
	pattern      kmsg.ACLResourcePatternType
	principal    *string
	host         *string
	operation    kmsg.ACLOperation
	permission   kmsg.ACLPermissionType
}

func (f *aclFilter) matches(a *acl) bool {
	if f.resourceType != kmsg.ACLResourceTypeAny && f.resourceType != a.resourceType {
		return false
	}
	if f.resourceName != nil && *f.resourceName != a.resourceName {
		return false
	}
	if f.pattern != kmsg.ACLResourcePatternTypeAny {
		if f.pattern == kmsg.ACLResourcePatternTypeMatch {
			if a.pattern != kmsg.ACLResourcePatternTypeLiteral && a.pattern != kmsg.ACLResourcePatternTypePrefixed {
				return false
			}
		} else if f.pattern != a.pattern {
			return false
		}
	}
	if f.principal != nil && *f.principal != a.principal {
		return false
	}
	if f.host != nil && *f.host != a.host {
		return false
	}
	if f.operation != kmsg.ACLOperationAny && f.operation != a.operation {
		return false
	}
	if f.permission != kmsg.ACLPermissionTypeAny && f.permission != a.permission {
		return false
	}
	return true
}

// Cluster ACL helper methods

func (c *Cluster) isSuperuser(user string) bool {
	if c.cfg.superusers == nil {
		return false
	}
	_, ok := c.cfg.superusers[user]
	return ok
}

func principal(user string) string {
	if user == "" {
		return "User:ANONYMOUS"
	}
	return "User:" + user
}

func (creq *clientReq) clientHost() string {
	addr := creq.cc.conn.RemoteAddr()
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String()
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

func (c *Cluster) allowedACL(creq *clientReq, resource string, resourceType kmsg.ACLResourceType, op kmsg.ACLOperation) bool {
	if !c.cfg.enableACLs {
		return true
	}
	user := creq.cc.user
	if c.isSuperuser(user) {
		return true
	}
	return c.acls.allowed(principal(user), creq.clientHost(), resource, resourceType, op)
}

func (c *Cluster) allowedClusterACL(creq *clientReq, op kmsg.ACLOperation) bool {
	return c.allowedACL(creq, aclClusterName, kmsg.ACLResourceTypeCluster, op)
}

func (c *Cluster) anyAllowedACL(creq *clientReq, resourceType kmsg.ACLResourceType, op kmsg.ACLOperation) bool {
	if !c.cfg.enableACLs {
		return true
	}
	user := creq.cc.user
	if c.isSuperuser(user) {
		return true
	}
	return c.acls.anyAllowed(principal(user), creq.clientHost(), resourceType, op)
}

// Authorized operations support for IncludeAuthorizedOperations fields.
// Returns a bitfield where each bit position corresponds to kmsg.ACLOperation(i).

var (
	clusterOps = []kmsg.ACLOperation{
		kmsg.ACLOperationRead,
		kmsg.ACLOperationWrite,
		kmsg.ACLOperationCreate,
		kmsg.ACLOperationDelete,
		kmsg.ACLOperationAlter,
		kmsg.ACLOperationDescribe,
		kmsg.ACLOperationClusterAction,
		kmsg.ACLOperationDescribeConfigs,
		kmsg.ACLOperationAlterConfigs,
		kmsg.ACLOperationIdempotentWrite,
	}
	topicOps = []kmsg.ACLOperation{
		kmsg.ACLOperationRead,
		kmsg.ACLOperationWrite,
		kmsg.ACLOperationCreate,
		kmsg.ACLOperationDelete,
		kmsg.ACLOperationAlter,
		kmsg.ACLOperationDescribe,
		kmsg.ACLOperationDescribeConfigs,
		kmsg.ACLOperationAlterConfigs,
	}
	groupOps = []kmsg.ACLOperation{
		kmsg.ACLOperationRead,
		kmsg.ACLOperationDelete,
		kmsg.ACLOperationDescribe,
	}
)

func (c *Cluster) clusterAuthorizedOps(creq *clientReq) int32 {
	return c.authorizedOps(creq, aclClusterName, kmsg.ACLResourceTypeCluster, clusterOps)
}

func (c *Cluster) topicAuthorizedOps(creq *clientReq, topic string) int32 {
	return c.authorizedOps(creq, topic, kmsg.ACLResourceTypeTopic, topicOps)
}

func (c *Cluster) groupAuthorizedOps(creq *clientReq, group string) int32 {
	return c.authorizedOps(creq, group, kmsg.ACLResourceTypeGroup, groupOps)
}

func (c *Cluster) authorizedOps(creq *clientReq, resource string, resourceType kmsg.ACLResourceType, ops []kmsg.ACLOperation) int32 {
	var bitfield int32
	for _, op := range ops {
		if c.allowedACL(creq, resource, resourceType, op) {
			bitfield |= 1 << int32(op)
		}
	}
	return bitfield
}
