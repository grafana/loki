package kfake

import (
	"hash/crc32"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kmsg"
)

// Produce: v3-13
//
// Behavior:
// * Writes are immediate - TimeoutMillis is ignored (no replication to wait on)
// * Only RecordBatch format is supported (v3+), not MessageSets (v0-2)
// * All batch-level validation is performed (CRC, attributes, sequences, etc.)
// * Idempotent and transactional produces are fully supported
//
// Version notes:
// * v3: RecordBatch format, transactions
// * v5: LogStartOffset in response
// * v8: ErrorMessage in response (KIP-467)
// * v9: Flexible versions
// * v12: KIP-890 implicit partition addition for transactions
// * v13: TopicID in request/response (KIP-516)

func init() { regKey(0, 3, 13) }

func (c *Cluster) handleProduce(creq *clientReq) (kmsg.Response, error) {
	var (
		b    = creq.cc.b
		req  = creq.kreq.(*kmsg.ProduceRequest)
		resp = req.ResponseKind().(*kmsg.ProduceResponse)
	)
	type tpid struct {
		t  string
		id [16]byte
	}
	var (
		tdone = make(map[tpid][]kmsg.ProduceResponseTopicPartition)
		id    = func(t kmsg.ProduceRequestTopic) tpid { return tpid{t.Topic, t.TopicID} }
	)

	if err := c.checkReqVersion(req.Key(), req.Version); err != nil {
		return nil, err
	}

	donep := func(t kmsg.ProduceRequestTopic, p kmsg.ProduceRequestTopicPartition, errCode int16, errMsg string) *kmsg.ProduceResponseTopicPartition {
		sp := kmsg.NewProduceResponseTopicPartition()
		sp.Partition = p.Partition
		sp.ErrorCode = errCode
		if req.Version >= 8 && errMsg != "" {
			sp.ErrorMessage = &errMsg
		}
		ps := tdone[id(t)]
		ps = append(ps, sp)
		tdone[id(t)] = ps
		return &ps[len(ps)-1]
	}
	donet := func(t kmsg.ProduceRequestTopic, errCode int16, errMsg string) {
		for _, p := range t.Partitions {
			donep(t, p, errCode, errMsg)
		}
	}
	donets := func(errCode int16, errMsg string) {
		for _, t := range req.Topics {
			donet(t, errCode, errMsg)
		}
	}
	var includeBrokers bool
	toresp := func() kmsg.Response {
		for id, partitions := range tdone {
			st := kmsg.NewProduceResponseTopic()
			st.Topic = id.t
			st.TopicID = id.id
			st.Partitions = partitions
			resp.Topics = append(resp.Topics, st)
		}
		if includeBrokers {
			for _, b := range c.bs {
				sb := kmsg.NewProduceResponseBroker()
				sb.NodeID = b.node
				sb.Host, sb.Port = b.hostport()
				sb.Rack = &brokerRack
				resp.Brokers = append(resp.Brokers, sb)
			}
		}
		return resp
	}

	switch req.Acks {
	case -1, 0, 1:
	default:
		donets(kerr.InvalidRequiredAcks.Code, "Invalid required acks.")
		return toresp(), nil
	}

	now := time.Now().UnixMilli()
	for _, rt := range req.Topics {
		if req.Version >= 13 {
			topic, ok := c.data.id2t[rt.TopicID]
			if !ok {
				donet(rt, kerr.UnknownTopicID.Code, "Unknown topic ID.")
				continue
			}
			rt.Topic = topic
		}
		if !c.allowedACL(creq, rt.Topic, kmsg.ACLResourceTypeTopic, kmsg.ACLOperationWrite) {
			donet(rt, kerr.TopicAuthorizationFailed.Code, kerr.TopicAuthorizationFailed.Message)
			continue
		}
		maxMessageBytes := c.data.maxMessageBytes(rt.Topic)
		for _, rp := range rt.Partitions {
			pd, ok := c.data.tps.getp(rt.Topic, rp.Partition)
			if !ok {
				donep(rt, rp, kerr.UnknownTopicOrPartition.Code, "Unknown topic or partition.")
				continue
			}
			if pd.leader != b {
				p := donep(rt, rp, kerr.NotLeaderForPartition.Code, "Not leader for partition.")
				p.CurrentLeader.LeaderID = pd.leader.node
				p.CurrentLeader.LeaderEpoch = pd.epoch
				includeBrokers = true
				continue
			}

			// Check message size before parsing
			if len(rp.Records) > maxMessageBytes {
				donep(rt, rp, kerr.MessageTooLarge.Code, "The message is too large.")
				continue
			}

			var b kmsg.RecordBatch
			if err := b.ReadFrom(rp.Records); err != nil {
				donep(rt, rp, kerr.CorruptMessage.Code, "Corrupt message.")
				continue
			}
			if b.FirstOffset != 0 {
				donep(rt, rp, kerr.CorruptMessage.Code, "First offset in batch must be zero.")
				continue
			}
			if int(b.Length) != len(rp.Records)-12 {
				donep(rt, rp, kerr.CorruptMessage.Code, "Batch length mismatch.")
				continue
			}
			// PartitionLeaderEpoch is not validated: real Kafka brokers
			// overwrite this field with the current leader epoch. Clients
			// send different values (franz-go sends -1, Sarama sends 0).

			if b.Magic != 2 {
				donep(rt, rp, kerr.CorruptMessage.Code, "Unsupported message format version.")
				continue
			}
			if b.CRC != int32(crc32.Checksum(rp.Records[21:], crc32c)) { // crc starts at byte 21
				donep(rt, rp, kerr.CorruptMessage.Code, "CRC mismatch.")
				continue
			}
			attrs := uint16(b.Attributes)
			if attrs&0x0020 != 0 {
				donep(rt, rp, kerr.InvalidRecord.Code, "Client cannot send control batches.")
				continue
			}
			if attrs&0x0007 > 4 {
				donep(rt, rp, kerr.CorruptMessage.Code, "Invalid compression type.")
				continue
			}
			logAppendTime := int64(-1)
			if attrs&0x0008 > 0 {
				b.FirstTimestamp = now
				b.MaxTimestamp = now
				logAppendTime = now
			}
			if b.LastOffsetDelta != b.NumRecords-1 {
				donep(rt, rp, kerr.CorruptMessage.Code, "Last offset delta mismatch.")
				continue
			}

			txnal := attrs&0x0010 != 0

			// Transactional batches must have a valid producer ID
			if txnal && b.ProducerID < 0 {
				donep(rt, rp, kerr.InvalidProducerIDMapping.Code, "Invalid producer ID mapping for transactional batch.")
				continue
			}

			var errCode int16
			var dup bool
			var baseOffset, lso int64

			if b.ProducerID >= 0 {
				// For KIP-890 (v12+), pass pd to implicitly add
				// the partition to the transaction if not yet added.
				var implicit *partData
				if txnal && req.Version >= 12 {
					implicit = pd
				}
				pidinf, window := c.pids.get(b.ProducerID, rt.Topic, rp.Partition, implicit)

				// For non-transactional idempotent produce with no
				// existing producer state, implicitly create it.
				// Apache Kafka and Redpanda accept the first batch
				// for an unknown (PID, epoch) with any sequence
				// number; this seeds producer state on demand.
				// See twmb/franz-go#1281.
				if !txnal && pidinf == nil && b.ProducerEpoch != -1 {
					pidinf, window = c.pids.getOrCreateNonTx(b.ProducerID, b.ProducerEpoch, rt.Topic, rp.Partition)
				}

				// Reject non-transactional produce during an
				// active transaction.
				if pidinf != nil && pidinf.inTx && !txnal {
					errCode = kerr.InvalidTxnState.Code
				}

				if txnal && window == nil {
					errCode = kerr.InvalidTxnState.Code
				}

				if errCode == 0 {
					switch {
					case window == nil && b.ProducerEpoch != -1:
						errCode = kerr.InvalidTxnState.Code
					case window != nil && b.ProducerEpoch < pidinf.epoch:
						errCode = kerr.InvalidProducerEpoch.Code
					case window != nil && b.ProducerEpoch > pidinf.epoch:
						// KIP-360: the real broker accepts any batchEpoch >= storedEpoch
						// for data batches (ProducerAppendInfo.java:119 -- only rejects
						// batchEpoch < storedEpoch). Non-txn idempotent producers bump
						// locally on produce errors; firstSeq==0 is enforced downstream
						// in pushAndValidate, which returns OOOSN (not InvalidProducerEpoch)
						// on violation -- matching the broker's error-code semantics.
						pidinf.epoch = b.ProducerEpoch
						fallthrough
					default:
						var seqOk bool
						seqOk, dup, baseOffset = window.pushAndValidate(b.ProducerEpoch, b.FirstSequence, b.NumRecords, pd.highWatermark)
						if !seqOk {
							errCode = kerr.OutOfOrderSequenceNumber.Code
						}
					}
				}
				if errCode == 0 && !dup {
					// Validation passed, push the batch.
					baseOffset = pd.highWatermark
					lso = pd.logStartOffset

					if txnal {
						// Track per-partition first offset for AbortedTransactions index.
						ptr := pidinf.txPartFirstOffsets.mkp(rt.Topic, rp.Partition, func() *int64 {
							v := int64(-1)
							return &v
						})
						if *ptr == -1 {
							*ptr = baseOffset
						}
					}

					if c.pushBatch(pd, len(rp.Records), b, txnal) < 0 {
						errCode = kerr.UnknownServerError.Code
					} else if txnal {
						pidinf.txBatchCount++
						// Track bytes for readCommitted watcher accounting at commit time
						bytesPtr := pidinf.txPartBytes.mkp(rt.Topic, rp.Partition, func() *int { return new(int) })
						*bytesPtr += len(rp.Records)
					}
				}
			} else {
				// Non-idempotent produce, no pids validation needed
				baseOffset = pd.highWatermark
				lso = pd.logStartOffset
				if c.pushBatch(pd, len(rp.Records), b, txnal) < 0 {
					errCode = kerr.UnknownServerError.Code
				}
			}

			if errCode != 0 {
				donep(rt, rp, errCode, "")
				continue
			}
			if dup {
				sp := donep(rt, rp, 0, "")
				sp.BaseOffset = baseOffset // original offset from dup detection
				continue
			}

			sp := donep(rt, rp, 0, "")
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
