package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterPartitionAssignments: v0-1
//
// KIP-455: Admin API to reassign partitions.
//
// Behavior:
// * ACL: ALTER on CLUSTER
// * If Replicas is null, this is a cancel request and returns NO_REASSIGNMENT_IN_PROGRESS
//   (kfake never has reassignments in progress)
// * Since kfake is single-node, we don't actually perform reassignment, just validate
//
// Version notes:
// * v1: AllowReplicationFactorChange

func init() { regKey(45, 0, 1) }

func (c *Cluster) handleAlterPartitionAssignments(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.AlterPartitionAssignmentsRequest)
	resp := req.ResponseKind().(*kmsg.AlterPartitionAssignmentsResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if !c.allowedClusterACL(creq, kmsg.ACLOperationAlter) {
		resp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
		return resp, nil
	}

	for _, rt := range req.Topics {
		st := kmsg.NewAlterPartitionAssignmentsResponseTopic()
		st.Topic = rt.Topic

		t, ok := c.data.tps.gett(rt.Topic)
		for _, rp := range rt.Partitions {
			sp := kmsg.NewAlterPartitionAssignmentsResponseTopicPartition()
			sp.Partition = rp.Partition

			if !ok {
				sp.ErrorCode = kerr.UnknownTopicOrPartition.Code
			} else if _, pok := t[rp.Partition]; !pok {
				sp.ErrorCode = kerr.UnknownTopicOrPartition.Code
			} else if rp.Replicas == nil {
				// Null replicas means cancel reassignment; kfake never has
				// any reassignments in progress.
				sp.ErrorCode = kerr.NoReassignmentInProgress.Code
			}
			// If replicas is non-nil, we "accept" the reassignment request
			// but do nothing since kfake doesn't actually support multi-broker
			// reassignment. ErrorCode stays 0 (success).

			st.Partitions = append(st.Partitions, sp)
		}

		resp.Topics = append(resp.Topics, st)
	}

	return resp, nil
}
