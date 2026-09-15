package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// FindCoordinator: v0-6
//
// Supports coordinator types:
// * 0: Group coordinator
// * 1: Transaction coordinator
// * 2: Share coordinator (KIP-932)
//
// Version notes:
// * v1: CoordinatorType, ThrottleMillis
// * v3: Flexible versions
// * v4: Multiple coordinator keys in single request (KIP-699)
// * v6: Share groups (KIP-932) - coordinator type 2

func init() { regKey(10, 0, 6) }

func (c *Cluster) handleFindCoordinator(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.FindCoordinatorRequest)
	resp := req.ResponseKind().(*kmsg.FindCoordinatorResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	var unknown bool
	if req.CoordinatorType != 0 && req.CoordinatorType != 1 && req.CoordinatorType != 2 {
		unknown = true
	}
	// Share coordinator (type 2) requires FindCoordinator v6+.
	if req.CoordinatorType == 2 && req.Version < 6 {
		unknown = true
	}

	if req.Version <= 3 {
		req.CoordinatorKeys = append(req.CoordinatorKeys, req.CoordinatorKey)
		defer func() {
			resp.ErrorCode = resp.Coordinators[0].ErrorCode
			resp.ErrorMessage = resp.Coordinators[0].ErrorMessage
			resp.NodeID = resp.Coordinators[0].NodeID
			resp.Host = resp.Coordinators[0].Host
			resp.Port = resp.Coordinators[0].Port
		}()
	}

	addc := func(key string) *kmsg.FindCoordinatorResponseCoordinator {
		sc := kmsg.NewFindCoordinatorResponseCoordinator()
		sc.Key = key
		resp.Coordinators = append(resp.Coordinators, sc)
		return &resp.Coordinators[len(resp.Coordinators)-1]
	}

	for _, key := range req.CoordinatorKeys {
		sc := addc(key)
		if unknown {
			sc.ErrorCode = kerr.InvalidRequest.Code
			continue
		}

		// ACL check based on coordinator type
		var allowed bool
		var errCode int16
		switch req.CoordinatorType {
		case 0: // Group
			allowed = c.allowedACL(creq, key, kmsg.ACLResourceTypeGroup, kmsg.ACLOperationDescribe)
			errCode = kerr.GroupAuthorizationFailed.Code
		case 1: // Transaction
			allowed = c.allowedACL(creq, key, kmsg.ACLResourceTypeTransactionalId, kmsg.ACLOperationDescribe)
			errCode = kerr.TransactionalIDAuthorizationFailed.Code
		case 2: // Share (KIP-932): requires CLUSTER CLUSTER_ACTION
			// (matching Java's KafkaApis.handleFindCoordinatorRequest
			// which calls authHelper.authorizeClusterOperation(CLUSTER_ACTION)).
			allowed = c.allowedClusterACL(creq, kmsg.ACLOperationClusterAction)
			errCode = kerr.ClusterAuthorizationFailed.Code
		}
		if !allowed {
			sc.ErrorCode = errCode
			continue
		}

		b := c.coordinator(key)
		sc.NodeID = b.node
		sc.Host, sc.Port = b.hostport()
	}

	return resp, nil
}
