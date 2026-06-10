package protohelpers

import (
	"fmt"
	"math"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

// SizeStdDuration returns the wire size of d encoded as a Duration payload
// (the bytes inside the outer length-delimited header). Returns 0 for the
// Go-zero `time.Duration(0)`, which is treated as "not set" by the
// (wiresmith.options.stdduration) contract — same proto3 default-suppression
// shape SizeStdTime uses for `time.Time{}`.
//
// Skips zero seconds and zero nanos individually, matching proto3 default-
// suppression rules on the inner Duration fields.
func SizeStdDuration(d time.Duration) int {
	if d == 0 {
		return 0
	}
	seconds := int64(d / time.Second)
	nanos := int32(d % time.Second)
	n := 0
	if seconds != 0 {
		n += 1 + SizeOfVarint(uint64(seconds))
	}
	if nanos != 0 {
		n += 1 + SizeOfVarint(uint64(nanos))
	}
	return n
}

// EncodeStdDuration writes the Duration payload for d in reverse-write order
// into dAtA, ending at offset. Caller must reserve SizeStdDuration(d) bytes
// before offset and must check `d != 0` before invoking — EncodeStdDuration
// does not gate the envelope itself.
//
// nanos (field 2) is written first so the resulting bytes appear in
// ascending tag order — the canonical proto3 emit order — once the caller
// flips the slice around via the reverse-write convention. Sign of seconds
// and nanos always matches because they come from integer division/modulo
// of the same int64 value, so the spec's "matching signs" invariant holds
// without an explicit check here.
func EncodeStdDuration(dAtA []byte, offset int, d time.Duration) int {
	seconds := int64(d / time.Second)
	nanos := int32(d % time.Second)
	if nanos != 0 {
		offset = EncodeVarint(dAtA, offset, uint64(nanos))
		offset--
		dAtA[offset] = 0x10
	}
	if seconds != 0 {
		offset = EncodeVarint(dAtA, offset, uint64(seconds))
		offset--
		dAtA[offset] = 0x08
	}
	return offset
}

// DecodeStdDuration parses a Duration payload (the bytes inside the outer
// length-delimited header) and returns the corresponding time.Duration.
// Used by (wiresmith.options.stdduration) field unmarshalers.
//
// Saturates on overflow: proto Duration can express up to ~10000 years
// while time.Duration is int64 nanoseconds and tops out at ~292 years. A
// payload whose seconds*1e9 + nanos doesn't fit returns math.MaxInt64 or
// math.MinInt64 rather than wrapping silently — matches
// `(*durationpb.Duration).AsDuration()` in google.golang.org/protobuf.
//
// Unknown inner fields and wire-type mismatches on the two known fields
// are skipped via protowire.ConsumeFieldValue — a Duration encoded by a
// future schema with extra inner fields stays decodable, in keeping with
// the rest of the generated unmarshal's tolerance to forward-compatible
// additions.
func DecodeStdDuration(b []byte) (time.Duration, error) {
	var seconds int64
	var nanos int32
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		if num < 1 {
			return 0, fmt.Errorf("proto: invalid field number")
		}
		b = b[n:]
		switch {
		case num == 1 && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return 0, protowire.ParseError(n)
			}
			seconds = int64(v)
			b = b[n:]
		case num == 2 && typ == protowire.VarintType:
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return 0, protowire.ParseError(n)
			}
			nanos = int32(v)
			b = b[n:]
		default:
			n := protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return 0, protowire.ParseError(n)
			}
			b = b[n:]
		}
	}
	// Saturation mirrors (*durationpb.Duration).AsDuration: detect overflow
	// via the round-trip check on seconds, then again on the nanos addition.
	// Sign-mismatch (seconds positive but resulting d negative, or vice
	// versa) is treated as overflow toward the seconds' sign — matches the
	// official runtime's behaviour for malformed input.
	d := time.Duration(seconds) * time.Second
	overflow := d/time.Second != time.Duration(seconds)
	d += time.Duration(nanos)
	if d > 0 && (seconds < 0 || nanos < 0) {
		overflow = true
	} else if d < 0 && (seconds > 0 || nanos > 0) {
		overflow = true
	}
	if overflow {
		switch {
		case seconds < 0:
			return time.Duration(math.MinInt64), nil
		case seconds > 0:
			return time.Duration(math.MaxInt64), nil
		}
	}
	return d, nil
}
