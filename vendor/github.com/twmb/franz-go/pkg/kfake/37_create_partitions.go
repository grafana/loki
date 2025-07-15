package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

func init() { regKey(37, 0, 3) }

func (c *Cluster) handleCreatePartitions(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	req := kreq.(*kmsg.CreatePartitionsRequest)
	resp := req.ResponseKind().(*kmsg.CreatePartitionsResponse)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	donet := func(t string, errCode int16) *kmsg.CreatePartitionsResponseTopic {
		st := kmsg.NewCreatePartitionsResponseTopic()
		st.Topic = t
		st.ErrorCode = errCode
		resp.Topics = append(resp.Topics, st)
		return &resp.Topics[len(resp.Topics)-1]
	}
	donets := func(errCode int16) {
		for _, rt := range req.Topics {
			donet(rt.Topic, errCode)
		}
	}

	if b != c.controller {
		donets(kerr.NotController.Code)
		return resp, nil
	}

	uniq := make(map[string]struct{})
	for _, rt := range req.Topics {
		if _, ok := uniq[rt.Topic]; ok {
			donets(kerr.InvalidRequest.Code)
			return resp, nil
		}
		uniq[rt.Topic] = struct{}{}
	}

	for _, rt := range req.Topics {
		t, ok := c.data.tps.gett(rt.Topic)
		if !ok {
			donet(rt.Topic, kerr.UnknownTopicOrPartition.Code)
			continue
		}
		if len(rt.Assignment) > 0 {
			donet(rt.Topic, kerr.InvalidReplicaAssignment.Code)
			continue
		}
		if rt.Count < int32(len(t)) {
			donet(rt.Topic, kerr.InvalidPartitions.Code)
			continue
		}
		for i := int32(len(t)); i < rt.Count; i++ {
			c.data.tps.mkp(rt.Topic, i, c.newPartData)
		}
		donet(rt.Topic, 0)
	}

	return resp, nil
}
