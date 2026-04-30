package handler

import (
	"github.com/chaudum/go-kaff/codec"
	"github.com/chaudum/go-kaff/store"
)

// handleJoinGroup handles JoinGroup requests (API key 11, versions 1–5).
//
// The handler blocks until the join barrier fires (all previously-stable group
// members have re-joined, or the rebalanceTimeoutMs expires) or the broker is
// stopped.
//
// ── Request format ─────────────────────────────────────────────────────────────
//
//	GroupId(string) | SessionTimeoutMs(int32)
//	RebalanceTimeoutMs(int32, v1+) | MemberId(string)
//	GroupInstanceId(nullable string, v5+)
//	ProtocolType(string) | Protocols(array): Name(string) | Metadata(bytes)
//
// ── Response format ────────────────────────────────────────────────────────────
//
//	ThrottleTimeMs(int32, v2+) | ErrorCode(int16) | GenerationId(int32)
//	ProtocolName(string) | Leader(string) | MemberId(string)
//	Members(array): MemberId(string) | [GroupInstanceId(nullable string, v5+)] | Metadata(bytes)
//
// The Members array is populated only for the elected group leader; other
// members receive an empty array.
func (rt *Router) handleJoinGroup(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	version := hdr.ApiVersion

	// ── Parse request ─────────────────────────────────────────────────────────
	groupID := r.ReadString()
	sessionTimeoutMs := r.ReadInt32()
	rebalanceTimeoutMs := int32(30_000) // default if v0
	if version >= 1 {
		rebalanceTimeoutMs = r.ReadInt32()
	}
	memberID := r.ReadString()
	if version >= 5 {
		r.ReadNullableString() // GroupInstanceId — static membership; ignored
	}
	protocolType := r.ReadString()

	numProtocols := r.ReadArrayLen()
	protocols := make([]store.MemberProtocol, 0, numProtocols)
	for i := int32(0); i < numProtocols; i++ {
		name := r.ReadString()
		metadata := r.ReadBytes()
		protocols = append(protocols, store.MemberProtocol{Name: name, Metadata: metadata})
	}

	if r.Err() != nil {
		return
	}

	// ── Join the group ────────────────────────────────────────────────────────
	group := rt.store.GetOrCreateGroup(groupID)
	newMemberID, ch := group.JoinGroup(memberID, hdr.ClientID, protocolType, sessionTimeoutMs, rebalanceTimeoutMs, protocols)

	var result store.JoinGroupResult
	select {
	case result = <-ch:
	case <-rt.ctx.Done():
		return
	}

	// ── Write response ────────────────────────────────────────────────────────
	if version >= 2 {
		w.WriteInt32(0) // ThrottleTimeMs
	}
	w.WriteInt16(result.ErrorCode)
	w.WriteInt32(result.GenerationID)
	w.WriteString(result.Protocol) // ProtocolName
	w.WriteString(result.LeaderID)
	w.WriteString(newMemberID)

	w.WriteArrayLen(int32(len(result.Members)))
	for _, m := range result.Members {
		w.WriteString(m.ID)
		if version >= 5 {
			w.WriteNullableString(nil) // GroupInstanceId = null
		}
		// Write the metadata for the chosen protocol.
		var metadata []byte
		for _, p := range m.Protocols {
			if p.Name == result.Protocol {
				metadata = p.Metadata
				break
			}
		}
		w.WriteBytes(metadata)
	}
}
