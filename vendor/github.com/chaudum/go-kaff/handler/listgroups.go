package handler

import "github.com/chaudum/go-kaff/codec"

// handleListGroups handles ListGroups requests (API key 16, versions 0–3).
//
// Returns all known consumer groups regardless of their state.
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	v0–v3: (empty body)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v1+) | ErrorCode(int16)
//	Groups(array): GroupId(string) | ProtocolType(string)
func (rt *Router) handleListGroups(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	// No request body for v0–v3.

	if r.Err() != nil {
		return
	}

	groups := rt.store.ListGroups()

	if version >= 1 {
		w.WriteInt32(0) // ThrottleTimeMs
	}
	w.WriteInt16(errNone) // top-level ErrorCode

	w.WriteArrayLen(int32(len(groups)))
	for _, g := range groups {
		info := g.Info()
		w.WriteString(info.ID)
		pt := info.ProtocolType
		if pt == "" {
			pt = "consumer"
		}
		w.WriteString(pt)
	}
}
