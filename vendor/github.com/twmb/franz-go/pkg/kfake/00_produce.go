package kfake

import (
	"hash/crc32"
	"net"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// TODO
// * Leaders
// * Support txns
// * Multiple batches in one produce
// * Compact

func init() { regKey(0, 3, 10) }

func (c *Cluster) handleProduce(b *broker, kreq kmsg.Request) (kmsg.Response, error) {
	var (
		req   = kreq.(*kmsg.ProduceRequest)
		resp  = req.ResponseKind().(*kmsg.ProduceResponse)
		tdone = make(map[string][]kmsg.ProduceResponseTopicPartition)
	)

	if err := checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	donep := func(t string, p kmsg.ProduceRequestTopicPartition, errCode int16) *kmsg.ProduceResponseTopicPartition {
		sp := kmsg.NewProduceResponseTopicPartition()
		sp.Partition = p.Partition
		sp.ErrorCode = errCode
		ps := tdone[t]
		ps = append(ps, sp)
		tdone[t] = ps
		return &ps[len(ps)-1]
	}
	donet := func(t kmsg.ProduceRequestTopic, errCode int16) {
		for _, p := range t.Partitions {
			donep(t.Topic, p, errCode)
		}
	}
	donets := func(errCode int16) {
		for _, t := range req.Topics {
			donet(t, errCode)
		}
	}
	var includeBrokers bool
	toresp := func() kmsg.Response {
		for topic, partitions := range tdone {
			st := kmsg.NewProduceResponseTopic()
			st.Topic = topic
			st.Partitions = partitions
			resp.Topics = append(resp.Topics, st)
		}
		if includeBrokers {
			for _, b := range c.bs {
				sb := kmsg.NewProduceResponseBroker()
				h, p, _ := net.SplitHostPort(b.ln.Addr().String())
				p32, _ := strconv.Atoi(p)
				sb.NodeID = b.node
				sb.Host = h
				sb.Port = int32(p32)
				resp.Brokers = append(resp.Brokers, sb)
			}
		}
		return resp
	}

	if req.TransactionID != nil {
		donets(kerr.TransactionalIDAuthorizationFailed.Code)
		return toresp(), nil
	}
	switch req.Acks {
	case -1, 0, 1:
	default:
		donets(kerr.InvalidRequiredAcks.Code)
		return toresp(), nil
	}

	now := time.Now().UnixMilli()
	for _, rt := range req.Topics {
		for _, rp := range rt.Partitions {
			pd, ok := c.data.tps.getp(rt.Topic, rp.Partition)
			if !ok {
				donep(rt.Topic, rp, kerr.UnknownTopicOrPartition.Code)
				continue
			}
			if pd.leader != b {
				p := donep(rt.Topic, rp, kerr.NotLeaderForPartition.Code)
				p.CurrentLeader.LeaderID = pd.leader.node
				p.CurrentLeader.LeaderEpoch = pd.epoch
				includeBrokers = true
				continue
			}

			var b kmsg.RecordBatch
			if err := b.ReadFrom(rp.Records); err != nil {
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}
			if b.FirstOffset != 0 {
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}
			if int(b.Length) != len(rp.Records)-12 {
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}
			if b.PartitionLeaderEpoch != -1 {
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}
			if b.Magic != 2 {
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}
			if b.CRC != int32(crc32.Checksum(rp.Records[21:], crc32c)) { // crc starts at byte 21
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}
			attrs := uint16(b.Attributes)
			if attrs&0x0007 > 4 {
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}
			logAppendTime := int64(-1)
			if attrs&0x0008 > 0 {
				b.FirstTimestamp = now
				b.MaxTimestamp = now
				logAppendTime = now
			}
			if attrs&0xfff0 != 0 { // TODO txn bit
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}
			if b.LastOffsetDelta != b.NumRecords-1 {
				donep(rt.Topic, rp, kerr.CorruptMessage.Code)
				continue
			}

			seqs, epoch := c.pids.get(b.ProducerID, b.ProducerEpoch, rt.Topic, rp.Partition)
			if be := b.ProducerEpoch; be != -1 {
				if be < epoch {
					donep(rt.Topic, rp, kerr.FencedLeaderEpoch.Code)
					continue
				} else if be > epoch {
					donep(rt.Topic, rp, kerr.UnknownLeaderEpoch.Code)
					continue
				}
			}
			ok, dup := seqs.pushAndValidate(b.FirstSequence, b.NumRecords)
			if !ok {
				donep(rt.Topic, rp, kerr.OutOfOrderSequenceNumber.Code)
				continue
			}
			if dup {
				donep(rt.Topic, rp, 0)
				continue
			}
			baseOffset := pd.highWatermark
			lso := pd.logStartOffset
			pd.pushBatch(len(rp.Records), b)
			sp := donep(rt.Topic, rp, 0)
			sp.BaseOffset = baseOffset
			sp.LogAppendTime = logAppendTime
			sp.LogStartOffset = lso
		}
	}

	if req.Acks == 0 {
		return nil, nil
	}
	return toresp(), nil
}

var crc32c = crc32.MakeTable(crc32.Castagnoli)
