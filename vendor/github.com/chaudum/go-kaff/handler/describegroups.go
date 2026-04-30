package handler

import "github.com/chaudum/go-kaff/codec"

// handleDescribeGroups handles DescribeGroups requests (API key 15, versions 0–3).
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	Groups(array of string) | IncludeAuthorizedOperations(bool, v3+)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v1+)
//	Groups(array):
//	  ErrorCode(int16) | GroupId(string) | GroupState(string)
//	  ProtocolType(string) | Protocol(string)
//	  Members(array):
//	    MemberId(string) | ClientId(string) | ClientHost(string)
//	    MemberMetadata(bytes) | MemberAssignment(bytes)
func (rt *Router) handleDescribeGroups(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	numGroups := r.ReadArrayLen()
	groupIDs := make([]string, 0, numGroups)
	for i := int32(0); i < numGroups; i++ {
		groupIDs = append(groupIDs, r.ReadString())
	}
	if version >= 3 {
		r.ReadBool() // IncludeAuthorizedOperations — ACLs not implemented
	}

	if r.Err() != nil {
		return
	}

	if version >= 1 {
		w.WriteInt32(0) // ThrottleTimeMs
	}

	w.WriteArrayLen(int32(len(groupIDs)))
	for _, gid := range groupIDs {
		g, ok := rt.store.GetGroup(gid)
		if !ok {
			// Return a placeholder entry for unknown groups.
			w.WriteInt16(errNone)
			w.WriteString(gid)
			w.WriteString("Dead") // Kafka returns "Dead" for unknown groups
			w.WriteString("")     // ProtocolType
			w.WriteString("")     // Protocol
			w.WriteArrayLen(0)    // Members
			continue
		}

		info := g.Info()

		w.WriteInt16(errNone)
		w.WriteString(info.ID)
		w.WriteString(info.State.String()) // e.g. "Stable"
		pt := info.ProtocolType
		if pt == "" {
			pt = "consumer"
		}
		w.WriteString(pt)
		w.WriteString(info.Protocol)

		w.WriteArrayLen(int32(len(info.Members)))
		for _, m := range info.Members {
			w.WriteString(m.MemberID)
			w.WriteString(m.ClientID)
			w.WriteString(m.ClientHost) // empty string; connection info not available
			w.WriteBytes(m.Metadata)
			w.WriteBytes(m.Assignment)
		}
	}
}
