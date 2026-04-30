package handler

import "github.com/chaudum/go-kaff/codec"

// handleSyncGroup handles SyncGroup requests (API key 14, versions 1–4).
//
// The group leader sends its computed partition assignments; non-leaders send
// an empty assignments array.  The handler blocks until the leader has
// delivered assignments (or the broker is stopped) and then returns the
// calling member's slice.
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	GroupId(string) | GenerationId(int32) | MemberId(string)
//	GroupInstanceId(nullable string, v3+)
//	Assignments(array): MemberId(string) | Assignment(bytes)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v1+) | ErrorCode(int16) | Assignment(bytes)
func (rt *Router) handleSyncGroup(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	// ── Parse request ─────────────────────────────────────────────────────────
	groupID := r.ReadString()
	generationID := r.ReadInt32()
	memberID := r.ReadString()
	if version >= 3 {
		r.ReadNullableString() // GroupInstanceId — ignored
	}

	numAssignments := r.ReadArrayLen()
	assignments := make(map[string][]byte, numAssignments)
	for i := int32(0); i < numAssignments; i++ {
		mid := r.ReadString()
		data := r.ReadBytes()
		assignments[mid] = data
	}

	if r.Err() != nil {
		return
	}

	// Non-leaders send an empty array; pass nil so the group can distinguish
	// "leader with zero assignments" from "non-leader with no assignments".
	var assignMap map[string][]byte
	if len(assignments) > 0 {
		assignMap = assignments
	}

	// ── Sync with the group ───────────────────────────────────────────────────
	group := rt.store.GetOrCreateGroup(groupID)
	ch := group.SyncGroup(memberID, generationID, assignMap)

	select {
	case res := <-ch:
		if version >= 1 {
			w.WriteInt32(0) // ThrottleTimeMs
		}
		w.WriteInt16(res.ErrorCode)
		w.WriteBytes(res.Assignment)
	case <-rt.ctx.Done():
	}
}
