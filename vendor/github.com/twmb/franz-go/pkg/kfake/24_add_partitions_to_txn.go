package kfake

import (
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// AddPartitionsToTxn: v0-5
//
// Behavior:
// * Registers partitions as part of an ongoing transaction
// * Must be called before producing to partitions (pre-KIP-890 clients)
// * KIP-890 clients (v4+) can skip this and use implicit partition addition
//
// Version notes:
// * v3: Flexible versions
// * v4: Batched transactions from brokers, VerifyOnly (KIP-890)
// * v5: Epoch bumping support (KIP-890)

func init() { regKey(24, 0, 5) }

func (c *Cluster) handleAddPartitionsToTxn(creq *clientReq) (kmsg.Response, error) {
	req := creq.kreq.(*kmsg.AddPartitionsToTxnRequest)
	resp := req.ResponseKind().(*kmsg.AddPartitionsToTxnResponse)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	// v0-3 carry one transaction at the top level. v4+ batch them and,
	// as in Kafka, come only from brokers: they need CLUSTER_ACTION and
	// skip the per-transaction WRITE checks.
	txns := req.Transactions
	if req.Version < 4 {
		t := kmsg.NewAddPartitionsToTxnRequestTransaction()
		t.TransactionalID = req.TransactionalID
		t.ProducerID = req.ProducerID
		t.ProducerEpoch = req.ProducerEpoch
		for _, rt := range req.Topics {
			tt := kmsg.NewAddPartitionsToTxnRequestTransactionTopic()
			tt.Topic = rt.Topic
			tt.Partitions = rt.Partitions
			t.Topics = append(t.Topics, tt)
		}
		txns = []kmsg.AddPartitionsToTxnRequestTransaction{t}
	} else if !c.allowedClusterACL(creq, kmsg.ACLOperationClusterAction) {
		resp.ErrorCode = kerr.ClusterAuthorizationFailed.Code
		return resp, nil
	}

	for i := range txns {
		st := c.pids.doAddPartitions(creq, &txns[i], req.Version < 4)
		if req.Version >= 4 {
			resp.Transactions = append(resp.Transactions, st)
			continue
		}
		for _, tt := range st.Topics {
			rt := kmsg.NewAddPartitionsToTxnResponseTopic()
			rt.Topic = tt.Topic
			for _, tp := range tt.Partitions {
				rp := kmsg.NewAddPartitionsToTxnResponseTopicPartition()
				rp.Partition = tp.Partition
				rp.ErrorCode = tp.ErrorCode
				rt.Partitions = append(rt.Partitions, rp)
			}
			resp.Topics = append(resp.Topics, rt)
		}
	}
	return resp, nil
}
