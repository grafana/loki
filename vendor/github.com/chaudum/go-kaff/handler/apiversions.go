package handler

import "github.com/chaudum/go-kaff/codec"

// supportedVersions is the static capability table returned by ApiVersions.
// It lists every API key go-kaff supports and the version range for each.
// This table is extended in later phases as more handlers are added.
//
// References:
//   - https://kafka.apache.org/protocol.html#protocol_api_keys
var supportedVersions = []apiVersionEntry{
	{apiKeyProduce,         3, 8},
	{apiKeyFetch,           4, 11},
	{apiKeyListOffsets,     1, 5},
	{apiKeyMetadata,        1, 9},
	{apiKeyOffsetCommit,    2, 7},
	{apiKeyOffsetFetch,     1, 5},
	{apiKeyFindCoordinator, 1, 2},
	{apiKeyJoinGroup,       1, 5},
	{apiKeyHeartbeat,       1, 3},
	{apiKeyLeaveGroup,      1, 2},
	{apiKeySyncGroup,       1, 3}, // v4 uses compact body/header in kgo; stay at v3
	{apiKeyDescribeGroups,  0, 3},
	{apiKeyListGroups,      0, 3},
	{apiKeyApiVersions,     0, 3},
	{apiKeyCreateTopics,    0, 4},
	{apiKeyDeleteTopics,    0, 3},
}

type apiVersionEntry struct {
	key, min, max int16
}

// handleApiVersions handles ApiVersions requests (API key 18, versions 0–3).
//
// Wire format — request body:
//
//	v0-v2: (empty after header)
//	v3+:   ClientSoftwareName(compact string) | ClientSoftwareVersion(compact string) | _tagged_fields
//
// Wire format — response body:
//
//	v0:   ErrorCode(int16) | ApiKeys(array) | (no throttle)
//	v1-2: ErrorCode(int16) | ApiKeys(array) | ThrottleTimeMs(int32)
//	v3:   ErrorCode(int16) | ApiKeys(compact array, each with _tagged_fields) | ThrottleTimeMs(int32) | _tagged_fields
//
// Note: the response header for ApiVersions intentionally omits the tagged-
// fields byte even for v3 (flexible), to enable backward-compatible version
// negotiation — see Router.ServeConn.
func (rt *Router) handleApiVersions(hdr RequestHeader, r *codec.Reader, w *codec.Writer) {
	flexible := isFlexible(hdr.ApiKey, hdr.ApiVersion)

	// Consume request body: v3+ has ClientSoftwareName, ClientSoftwareVersion, and
	// tagged_fields (all informational; never needed for a correct response).
	// Some clients (e.g. franz-go v1.21) send a shorter body than the full spec
	// requires, so we absorb any parse error rather than dropping the connection.
	if flexible {
		_ = r.ReadCompactString() // ClientSoftwareName
		_ = r.ReadCompactString() // ClientSoftwareVersion
		r.ReadTaggedFields()
		r.ResetErr() // body fields are optional; do not fail the connection
	}

	// ErrorCode = 0 (None).
	w.WriteInt16(errNone)

	// ApiKeys array.
	if flexible {
		w.WriteCompactArrayLen(int32(len(supportedVersions)))
	} else {
		w.WriteArrayLen(int32(len(supportedVersions)))
	}
	for _, e := range supportedVersions {
		w.WriteInt16(e.key)
		w.WriteInt16(e.min)
		w.WriteInt16(e.max)
		if flexible {
			w.WriteTaggedFields() // per-entry tagged fields
		}
	}

	// ThrottleTimeMs (v1+).
	if hdr.ApiVersion >= 1 {
		w.WriteInt32(0)
	}

	// Response body tagged fields (v3+).
	if flexible {
		w.WriteTaggedFields()
	}
}
