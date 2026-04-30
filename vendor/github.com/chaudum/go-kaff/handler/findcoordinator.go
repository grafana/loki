package handler

import "github.com/chaudum/go-kaff/codec"

// handleFindCoordinator handles FindCoordinator requests (API key 10, versions 1–2).
//
// go-kaff is always its own group coordinator, so this handler always returns
// the broker's own node information.
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	Key(string) | KeyType(int8, v1+)
//	  KeyType: 0 = GROUP, 1 = TRANSACTION (we only support 0)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v1+) | ErrorCode(int16) | ErrorMessage(nullable string, v1+)
//	NodeId(int32) | Host(string) | Port(int32)
func (rt *Router) handleFindCoordinator(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	r.ReadString() // Key (group ID or transactional ID) — we don't need it
	if version >= 1 {
		r.ReadInt8() // KeyType — ignored; we are always the coordinator
	}

	if version >= 1 {
		w.WriteInt32(0) // ThrottleTimeMs
	}
	w.WriteInt16(errNone)
	if version >= 1 {
		w.WriteNullableString(nil) // ErrorMessage = null
	}
	w.WriteInt32(rt.broker.NodeID)
	w.WriteString(rt.broker.Host)
	w.WriteInt32(rt.broker.Port)
}
