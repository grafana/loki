package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(34, 0, 2) }

func (c *Cluster) handleAlterReplicaLogDirs(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.AlterReplicaLogDirsRequest)
	resp := req.ResponseKind().(*kmsg.AlterReplicaLogDirsResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	tidx := make(map[string]int)
	donet := func(t string, errCode int16) *kmsg.AlterReplicaLogDirsResponseTopic {
		if i, ok := tidx[t]; ok {
			return &resp.Topics[i]
		}
		tidx[t] = len(resp.Topics)
		st := kmsg.NewAlterReplicaLogDirsResponseTopic()
		st.Topic = t
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donep := func(t string, p int32, errCode int16) *kmsg.AlterReplicaLogDirsResponseTopicPartition {
		sp := kmsg.NewAlterReplicaLogDirsResponseTopicPartition()
		sp.Partition = p
		sp.ErrorCode = errCode
		st := donet(t, 0)
		st.Partitions = append(st.Partitions, sp)
		return &st.Partitions[len(st.Partitions)-1]
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
