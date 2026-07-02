package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AlterReplicaLogDirs: v0-2
//
// Behavior:
// * Sets the log directory for partitions
// * kfake stores the directory but doesn't actually move data
//
// Version notes:
// * v1: ThrottleMillis
// * v2: Flexible versions

func init() { regKey(34, 0, 2) }

func (c *Cluster) handleAlterReplicaLogDirs(creq *clientReq) (kmsg.Response, error) {
	var (
		req  = creq.kreq.(*kmsg.AlterReplicaLogDirsRequest)
		resp = req.ResponseKind().(*kmsg.AlterReplicaLogDirsResponse)
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	if !c.allowedClusterACL(creq, kmsg.ACLOperationAlter) {
		// Return cluster authorization failed for all partitions
		for _, rd := range req.Dirs {
			for _, t := range rd.Topics {
				st := kmsg.NewAlterReplicaLogDirsResponseTopic()
				st.Topic = t.Topic
				for _, p := range t.Partitions {
					sp := kmsg.NewAlterReplicaLogDirsResponseTopicPartition()
					sp.Partition = p
					sp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
					st.Partitions = append(st.Partitions, sp)
				}
				resp.Topics = append(resp.Topics, st)
			}
		}
		return resp, nil
	}

	tidx := make(map[string]int)
	donet := func(t string) *kmsg.AlterReplicaLogDirsResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		tidx[t] = len(resp.Topics)
		st := kmsg.NewAlterReplicaLogDirsResponseTopic()
		st.Topic = t
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, p int32, errCode int16) {
		sp := kmsg.NewAlterReplicaLogDirsResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st := donet(t)
		st.Partitions = append(st.Partitions, sp)
	}

	for _, rd := range req.Dirs {
		for _, t := range rd.Topics {
			for _, p := range t.Partitions {
				d, ok := c.data.tps.getp(t.Topic, p)
				if !ok {
					donep(t.Topic, p, kerr.UnknownTopicOrPartition.Code)
					continue
				}
				d.dir = rd.Dir
				donep(t.Topic, p, 0)
			}
		}
	}

	return resp, nil
}
