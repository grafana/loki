// Package handler implements the Kafka request router and per-API handlers.
package handler

import "github.com/chaudum/go-kaff/codec"

// ── API keys ──────────────────────────────────────────────────────────────────

const (
	apiKeyProduce         int16 = 0
	apiKeyFetch           int16 = 1
	apiKeyListOffsets     int16 = 2
	apiKeyMetadata        int16 = 3
	apiKeyOffsetCommit    int16 = 8
	apiKeyOffsetFetch     int16 = 9
	apiKeyFindCoordinator int16 = 10
	apiKeyJoinGroup       int16 = 11
	apiKeyHeartbeat       int16 = 12
	apiKeyLeaveGroup      int16 = 13
	apiKeySyncGroup       int16 = 14
	apiKeyDescribeGroups  int16 = 15
	apiKeyListGroups      int16 = 16
	apiKeyApiVersions     int16 = 18
	apiKeyCreateTopics    int16 = 19
	apiKeyDeleteTopics    int16 = 20
)

// ── Kafka error codes ─────────────────────────────────────────────────────────

const (
	errNone                    int16 = 0
	errOffsetOutOfRange        int16 = 1
	errUnknownTopicOrPartition int16 = 3
	// Phase 3 — consumer group errors.
	errCoordinatorNotAvailable int16 = 15
	errNotCoordinator          int16 = 16
	errIllegalGeneration       int16 = 22
	errUnknownMemberID         int16 = 23
	errRebalanceInProgress     int16 = 25
	errUnsupportedVersion      int16 = 35
	errTopicAlreadyExists      int16 = 36
)

// ── Flexible version thresholds ───────────────────────────────────────────────

// flexibleAt maps each ApiKey to the first ApiVersion that uses the compact
// "flexible" encoding (KIP-482 / tagged fields).
//
// For these API versions the request header is HeaderV2 (includes tagged
// fields after ClientId) and the response header is HeaderV1 (includes tagged
// fields after CorrelationId) — EXCEPT for ApiVersions (key 18) which
// deliberately omits tagged fields from the response header even when
// flexible, to allow version negotiation with older brokers.
var flexibleAt = map[int16]int16{
	apiKeyProduce:         9,
	apiKeyFetch:           12,
	apiKeyListOffsets:     6,
	apiKeyMetadata:        9,
	apiKeyOffsetCommit:    8,
	apiKeyOffsetFetch:     7,
	apiKeyFindCoordinator: 3,
	apiKeyJoinGroup:       6,
	apiKeyHeartbeat:       4,
	apiKeyLeaveGroup:      4,
	apiKeySyncGroup:       5,
	apiKeyDescribeGroups:  5,
	apiKeyListGroups:      4,
	apiKeyApiVersions:     3,
	apiKeyCreateTopics:    5,
	apiKeyDeleteTopics:    4,
}

// isFlexible reports whether the given (ApiKey, ApiVersion) pair uses
// flexible (compact) encoding.
func isFlexible(apiKey, apiVersion int16) bool {
	min, known := flexibleAt[apiKey]
	return known && apiVersion >= min
}

// ── Request header ────────────────────────────────────────────────────────────

// RequestHeader contains the fields common to every Kafka request frame.
type RequestHeader struct {
	ApiKey        int16
	ApiVersion    int16
	CorrelationID int32
	ClientID      string
}

// decodeRequestHeader reads a full request header from r.
//
// The header layout depends on whether the (ApiKey, ApiVersion) pair uses
// flexible encoding:
//
//	Non-flexible (header v0/v1):
//	  ApiKey(16) | ApiVersion(16) | CorrelationId(32) | ClientId(string/int16)
//
//	Flexible (header v2):
//	  ApiKey(16) | ApiVersion(16) | CorrelationId(32) | ClientId(compact string) | _tagged_fields
//
// ApiKey and ApiVersion are always fixed-width, so we can read them before
// deciding which encoding to use for the rest of the header.
//
// All Kafka clients (including franz-go and sarama) use the non-flexible
// (header v1) int16-prefixed ClientId string for ALL request versions,
// including flexible ones.  For flexible API versions the header also ends
// with a TAG_BUFFER byte (always 0x00 for no tags), which must be consumed
// so the reader is positioned at the start of the request body.
//
// ApiVersions (key 18) is special-cased: it always uses a bare header v1
// with no tagged fields, even for v3+ (KIP-482 bootstrap exception).
//
// Compatibility with hand-crafted test frames that use
// WriteCompactNullableString(nil)+WriteTaggedFields() (= 0x00 0x00):
// ReadNullableString reads 0x0000 = int16(0) = empty string, consuming both
// bytes so no tagged-fields byte is left to read.  Test builders for flexible
// Metadata/etc. requests use WriteNullableString(nil) so that the layout
// matches what real clients send.
func decodeRequestHeader(r *codec.Reader) RequestHeader {
	var h RequestHeader
	h.ApiKey = r.ReadInt16()
	h.ApiVersion = r.ReadInt16()
	h.CorrelationID = r.ReadInt32()
	s := r.ReadNullableString() // int16-prefixed ClientId for all clients
	if s != nil {
		h.ClientID = *s
	}
	// Consume the header TAG_BUFFER that flexible clients append after ClientId.
	// ApiVersions always uses bare header v1 (no tagged fields).
	if isFlexible(h.ApiKey, h.ApiVersion) && h.ApiKey != apiKeyApiVersions {
		r.ReadTaggedFields()
	}
	return h
}
