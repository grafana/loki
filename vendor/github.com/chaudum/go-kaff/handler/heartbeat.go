package handler

import "github.com/chaudum/go-kaff/codec"

// handleHeartbeat handles Heartbeat requests (API key 12, versions 1–3).
//
// Heartbeats reset the member's session timer.  The response error code tells
// the client whether the group is currently rebalancing.
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	GroupId(string) | GenerationId(int32) | MemberId(string)
//	GroupInstanceId(nullable string, v3+)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v1+) | ErrorCode(int16)
func (rt *Router) handleHeartbeat(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	groupID := r.ReadString()
	generationID := r.ReadInt32()
	memberID := r.ReadString()
	if version >= 3 {
		r.ReadNullableString() // GroupInstanceId — ignored
	}

	if r.Err() != nil {
		return
	}

	group, ok := rt.store.GetGroup(groupID)
	var errCode int16
	if !ok {
		errCode = errUnknownMemberID
	} else {
		errCode = group.Heartbeat(memberID, generationID)
	}

	if version >= 1 {
		w.WriteInt32(0) // ThrottleTimeMs
	}
	w.WriteInt16(errCode)
}
