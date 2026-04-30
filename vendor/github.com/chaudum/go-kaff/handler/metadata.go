package handler

import "github.com/chaudum/go-kaff/codec"

// handleMetadata handles Metadata requests (API key 3, versions 1–9).
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	v1-v3:  Topics(array of string, null = all) [no AllowAutoTopicCreation]
//	v4-v7:  Topics(array of string, null = all) | AllowAutoTopicCreation(bool)
//	v8:     Topics(array of string, null = all) | AllowAutoTopicCreation(bool)
//	                                            | IncludeClusterAuthorizedOps(bool)
//	                                            | IncludeTopicAuthorizedOps(bool)
//	v9:     Topics(compact nullable array of {Name(compact string) + _tagged_fields})
//	                                            | AllowAutoTopicCreation(bool)
//	                                            | IncludeClusterAuthorizedOps(bool) [v8-v10 only]
//	                                            | IncludeTopicAuthorizedOps(bool)
//	                                            | _tagged_fields
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	v1:     Brokers | ControllerId | Topics
//	v2:     Brokers | ClusterId | ControllerId | Topics
//	v3-v8:  ThrottleTimeMs | Brokers | ClusterId | ControllerId | Topics
//	v9:     ThrottleTimeMs | Brokers(compact) | ClusterId(compact) | ControllerId
//	                       | Topics(compact) | _tagged_fields
//
// Per-version additions to sub-structures:
//   - v1:  broker.Rack, topic.IsInternal, response.ControllerId
//   - v2:  response.ClusterId
//   - v3:  response.ThrottleTimeMs
//   - v5:  partition.OfflineReplicas
//   - v7:  partition.LeaderEpoch
//   - v8:  topic.AuthorizedOperations, response.AuthorizedOperations (removed in v11)
//   - v9:  flexible encoding throughout
func (rt *Router) handleMetadata(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	flexible := isFlexible(hdr.ApiKey, hdr.ApiVersion)

	// ── Parse request ─────────────────────────────────────────────────────────

	// requestedTopics: nil = all topics, []string{} = none, populated = specific.
	var requestedTopics []string // nil means "all"

	if flexible {
		// v9+: compact nullable array; 0 (uvarint) = null = all topics.
		n := r.ReadCompactArrayLen()
		if n >= 0 {
			requestedTopics = make([]string, n)
			for i := range requestedTopics {
				requestedTopics[i] = r.ReadCompactString() // topic name (v9 uses string, not nullable)
				r.ReadTaggedFields()                       // per-topic tagged fields
			}
		}
		// n == -1 (null) → requestedTopics stays nil (= all topics)
	} else {
		// v1-v8: regular nullable int32 array; -1 = null = all topics.
		n := r.ReadArrayLen()
		if n >= 0 {
			requestedTopics = make([]string, n)
			for i := range requestedTopics {
				requestedTopics[i] = r.ReadString()
			}
		}
	}

	// AllowAutoTopicCreation (v4+): consume the client flag but always honour the
	// broker's own AutoCreateTopics setting.  go-kaff is a test broker; its
	// config takes precedence over what any individual client requests.
	if hdr.ApiVersion >= 4 {
		_ = r.ReadBool() // consume AllowAutoTopicCreation
	}
	allowAutoCreate := rt.broker.AutoCreateTopics
	if hdr.ApiVersion >= 8 && hdr.ApiVersion <= 10 {
		_ = r.ReadBool() // IncludeClusterAuthorizedOperations
	}
	if hdr.ApiVersion >= 8 {
		_ = r.ReadBool() // IncludeTopicAuthorizedOperations
	}
	if flexible {
		r.ReadTaggedFields()
	}

	// ── Resolve topics ────────────────────────────────────────────────────────

	type topicResult struct {
		name      string
		numParts  int32
		errorCode int16
	}

	var results []topicResult

	if requestedTopics == nil {
		// Null topics → return all known topics.
		for _, t := range rt.store.ListTopics() {
			results = append(results, topicResult{name: t.Name(), numParts: t.NumPartitions()})
		}
	} else {
		for _, name := range requestedTopics {
			t, exists := rt.store.GetTopic(name)
			if !exists {
				if allowAutoCreate && rt.broker.AutoCreateTopics {
					created, _ := rt.store.GetOrCreateTopic(name, rt.broker.DefaultPartitions, rt.broker.MaxBytesPerPartition)
					results = append(results, topicResult{name: created.Name(), numParts: created.NumPartitions()})
				} else {
					results = append(results, topicResult{
						name:      name,
						errorCode: errUnknownTopicOrPartition,
					})
				}
			} else {
				results = append(results, topicResult{name: t.Name(), numParts: t.NumPartitions()})
			}
		}
	}

	// ── Write response ────────────────────────────────────────────────────────

	version := hdr.ApiVersion
	b := rt.broker
	clusterID := "go-kaff"

	// ThrottleTimeMs (v3+).
	if version >= 3 {
		w.WriteInt32(0)
	}

	// Brokers array (always exactly one broker).
	if flexible {
		w.WriteCompactArrayLen(1)
	} else {
		w.WriteArrayLen(1)
	}
	w.WriteInt32(b.NodeID)
	if flexible {
		w.WriteCompactString(b.Host)
	} else {
		w.WriteString(b.Host)
	}
	w.WriteInt32(b.Port)
	if version >= 1 { // Rack (nullable, always null)
		if flexible {
			w.WriteCompactNullableString(nil)
		} else {
			w.WriteNullableString(nil)
		}
	}
	if flexible {
		w.WriteTaggedFields() // broker entry tagged fields
	}

	// ClusterId (v2+).
	if version >= 2 {
		if flexible {
			w.WriteCompactNullableString(&clusterID)
		} else {
			w.WriteNullableString(&clusterID)
		}
	}

	// ControllerId (v1+).
	if version >= 1 {
		w.WriteInt32(b.NodeID)
	}

	// Topics array.
	if flexible {
		w.WriteCompactArrayLen(int32(len(results)))
	} else {
		w.WriteArrayLen(int32(len(results)))
	}
	for _, res := range results {
		w.WriteInt16(res.errorCode)

		if flexible {
			w.WriteCompactString(res.name)
		} else {
			w.WriteString(res.name)
		}

		// IsInternal (v1+) — go-kaff has no internal topics.
		if version >= 1 {
			w.WriteBool(false)
		}

		// Partitions array — empty when the topic has an error.
		numParts := res.numParts
		if res.errorCode != errNone {
			numParts = 0
		}
		if flexible {
			w.WriteCompactArrayLen(numParts)
		} else {
			w.WriteArrayLen(numParts)
		}
		for p := int32(0); p < numParts; p++ {
			w.WriteInt16(errNone) // partition error_code
			w.WriteInt32(p)       // partition_index
			w.WriteInt32(b.NodeID) // leader_id

			// LeaderEpoch (v7+).
			if version >= 7 {
				w.WriteInt32(-1) // -1 = no epoch tracked
			}

			// ReplicaNodes: [nodeID].
			if flexible {
				w.WriteCompactArrayLen(1)
			} else {
				w.WriteArrayLen(1)
			}
			w.WriteInt32(b.NodeID)

			// IsrNodes: [nodeID].
			if flexible {
				w.WriteCompactArrayLen(1)
			} else {
				w.WriteArrayLen(1)
			}
			w.WriteInt32(b.NodeID)

			// OfflineReplicas: [] (v5+).
			if version >= 5 {
				if flexible {
					w.WriteCompactArrayLen(0)
				} else {
					w.WriteArrayLen(0)
				}
			}

			if flexible {
				w.WriteTaggedFields() // partition tagged fields
			}
		}

		// TopicAuthorizedOperations (v8+, removed at v11; we only go to v9).
		// INT32_MIN signals "no authorized operations" (ACLs disabled).
		if version >= 8 {
			w.WriteInt32(-2147483648)
		}

		if flexible {
			w.WriteTaggedFields() // topic tagged fields
		}
	}

	// ClusterAuthorizedOperations (v8–v10).
	if version >= 8 && version <= 10 {
		w.WriteInt32(-2147483648)
	}

	// Response body tagged fields (v9+).
	if flexible {
		w.WriteTaggedFields()
	}
}
