package handler

import (
	"strings"

	"github.com/chaudum/go-kaff/codec"
)

// handleCreateTopics handles CreateTopics requests (API key 19, versions 0–4).
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	Topics(array):
//	  Name(string) | NumPartitions(int32) | ReplicationFactor(int16)
//	  Assignments(array): PartitionIndex(int32) | BrokerIds(array of int32)
//	  Configs(array):     Name(string) | Value(nullable string)
//	TimeoutMs(int32) | ValidateOnly(bool, v1+)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v2+)
//	Topics(array):
//	  Name(string) | ErrorCode(int16) | ErrorMessage(nullable string, v1+)
//	  NumPartitions(int32, v4+) | ReplicationFactor(int16, v4+)
//	  Configs(nullable array, v4+) — always null (go-kaff has no topic configs)
func (rt *Router) handleCreateTopics(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	// ── Parse request ─────────────────────────────────────────────────────────
	type topicReq struct {
		name          string
		numPartitions int32
	}

	numTopics := r.ReadArrayLen()
	reqs := make([]topicReq, 0, numTopics)
	for i := int32(0); i < numTopics; i++ {
		name := r.ReadString()
		numParts := r.ReadInt32()
		r.ReadInt16() // ReplicationFactor — always 1 for go-kaff; ignored

		// Assignments (consume and discard).
		nAssign := r.ReadArrayLen()
		for j := int32(0); j < nAssign; j++ {
			r.ReadInt32()              // PartitionIndex
			nBrokers := r.ReadArrayLen()
			for k := int32(0); k < nBrokers; k++ {
				r.ReadInt32()
			}
		}

		// Configs (consume and discard).
		nCfg := r.ReadArrayLen()
		for j := int32(0); j < nCfg; j++ {
			r.ReadString()         // Name
			r.ReadNullableString() // Value
		}

		reqs = append(reqs, topicReq{name: name, numPartitions: numParts})
	}

	r.ReadInt32() // TimeoutMs — ignored (all operations are synchronous)
	if version >= 1 {
		r.ReadBool() // ValidateOnly — we always apply; no dry-run mode
	}

	if r.Err() != nil {
		return
	}

	// ── Create topics ─────────────────────────────────────────────────────────
	type topicResult struct {
		name      string
		numParts  int32
		errorCode int16
		errorMsg  string
	}

	results := make([]topicResult, 0, len(reqs))
	for _, req := range reqs {
		res := topicResult{name: req.name}

		if strings.TrimSpace(req.name) == "" {
			res.errorCode = errUnknownTopicOrPartition
			res.errorMsg = "Topic name cannot be empty"
			results = append(results, res)
			continue
		}

		numParts := req.numPartitions
		if numParts <= 0 {
			numParts = rt.broker.DefaultPartitions
		}

		err := rt.store.CreateTopic(req.name, numParts, rt.broker.MaxBytesPerPartition)
		if err != nil {
			res.errorCode = errTopicAlreadyExists
			res.errorMsg = err.Error()
		} else {
			res.numParts = numParts
			rt.log.Info("topic created", "topic", req.name, "partitions", numParts)
		}
		results = append(results, res)
	}

	// ── Write response ────────────────────────────────────────────────────────
	if version >= 2 {
		w.WriteInt32(0) // ThrottleTimeMs
	}

	w.WriteArrayLen(int32(len(results)))
	for _, res := range results {
		w.WriteString(res.name)
		w.WriteInt16(res.errorCode)
		if version >= 1 {
			if res.errorMsg != "" {
				s := res.errorMsg
				w.WriteNullableString(&s)
			} else {
				w.WriteNullableString(nil)
			}
		}
		if version >= 4 {
			if res.errorCode == errNone {
				w.WriteInt32(res.numParts) // NumPartitions
				w.WriteInt16(1)            // ReplicationFactor = 1
			} else {
				w.WriteInt32(-1)
				w.WriteInt16(-1)
			}
			w.WriteArrayLen(-1) // Configs = null
		}
	}
}
