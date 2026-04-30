package handler

import "github.com/chaudum/go-kaff/codec"

// handleLeaveGroup handles LeaveGroup requests (API key 13, versions 1–2).
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	GroupId(string) | MemberId(string)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v1+) | ErrorCode(int16)
func (rt *Router) handleLeaveGroup(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	groupID := r.ReadString()
	memberID := r.ReadString()

	if r.Err() != nil {
		return
	}

	group, ok := rt.store.GetGroup(groupID)
	var errCode int16
	if !ok {
		errCode = errUnknownMemberID
	} else {
		errCode = group.LeaveGroup(memberID)
	}

	if version >= 1 {
		w.WriteInt32(0) // ThrottleTimeMs
	}
	w.WriteInt16(errCode)
}
