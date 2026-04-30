package handler

import "github.com/chaudum/go-kaff/codec"

// handleDeleteTopics handles DeleteTopics requests (API key 20, versions 0–3).
//
// Deleting a topic also wakes any goroutines currently blocked in a long-poll
// Fetch for that topic's partitions; they will return UNKNOWN_TOPIC_OR_PARTITION.
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	TopicNames(array of string) | TimeoutMs(int32)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v1+)
//	Responses(array):
//	  Name(string) | ErrorCode(int16) | ErrorMessage(nullable string, v1+)
func (rt *Router) handleDeleteTopics(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	numTopics := r.ReadArrayLen()
	names := make([]string, 0, numTopics)
	for i := int32(0); i < numTopics; i++ {
		names = append(names, r.ReadString())
	}
	r.ReadInt32() // TimeoutMs — ignored

	if r.Err() != nil {
		return
	}

	// ── Delete topics ─────────────────────────────────────────────────────────
	type topicResult struct {
		name      string
		errorCode int16
		errorMsg  string
	}

	results := make([]topicResult, 0, len(names))
	for _, name := range names {
		res := topicResult{name: name}
		if err := rt.store.DeleteTopic(name); err != nil {
			res.errorCode = errUnknownTopicOrPartition
			res.errorMsg = err.Error()
		} else {
			rt.log.Info("topic deleted", "topic", name)
		}
		results = append(results, res)
	}

	// ── Write response ────────────────────────────────────────────────────────
	if version >= 1 {
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
	}
}
