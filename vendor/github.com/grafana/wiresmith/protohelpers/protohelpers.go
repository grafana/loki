// Package protohelpers provides encoding helpers for reverse-write protobuf marshaling.
// Based on github.com/planetscale/vtprotobuf/protohelpers (Apache 2.0 license).
package protohelpers

import (
	"fmt"
	"math"
	"math/bits"

	"google.golang.org/protobuf/encoding/protowire"
)

// EncodeVarint writes a varint-encoded uint64 into dAtA ending at offset,
// writing backwards. Returns the new offset (before the encoded varint).
func EncodeVarint(dAtA []byte, offset int, v uint64) int {
	offset -= SizeOfVarint(v)
	base := offset
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset++
	}
	dAtA[offset] = uint8(v)
	return base
}

// SizeOfVarint returns the encoded size of a varint.
func SizeOfVarint(x uint64) int {
	return (bits.Len64(x|1) + 6) / 7
}

// MaxUnmarshalDepth bounds nested-message recursion during Unmarshal.
// Generated UnmarshalWithDepth methods guard `depth > MaxUnmarshalDepth`
// and thread depth+1 through every nested decode (including across
// packages), so a malicious deeply-nested payload fails with an error
// instead of overflowing the stack.
const MaxUnmarshalDepth = 10000

// SkipValue skips one field value of the given wire type at the start of
// dAtA and returns the number of bytes consumed. Used by generated
// unmarshal loops for unknown fields and wire-type mismatches, where the
// tag has already been decoded. Shared here rather than emitted per file
// so that multiple .proto files generated into one Go package don't
// collide on a package-level helper; it is the cold path (never taken for
// schema-known fields), so the cross-package call costs nothing on the
// hot loop.
func SkipValue(dAtA []byte, wireType int, fieldNum int32) (int, error) {
	iNdEx := 0
	l := len(dAtA)
	switch wireType {
	case 0: // varint
		for shift := 0; ; shift++ {
			if shift >= 10 {
				return 0, fmt.Errorf("invalid varint")
			}
			if iNdEx >= l {
				return 0, fmt.Errorf("invalid varint")
			}
			iNdEx++
			if dAtA[iNdEx-1] < 0x80 {
				break
			}
		}
	case 1: // fixed64
		if (iNdEx + 8) > l {
			return 0, fmt.Errorf("truncated fixed64")
		}
		iNdEx += 8
	case 2: // length-delimited
		var length uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, fmt.Errorf("invalid bytes")
			}
			if iNdEx >= l {
				return 0, fmt.Errorf("invalid bytes")
			}
			b := dAtA[iNdEx]
			iNdEx++
			length |= uint64(b&0x7F) << shift
			// 10th-byte overflow: only bit 0 of the payload is legal at
			// shift==63; anything else overflows uint64. Continuation
			// bytes fall through to the shift>=64 guard, so the check
			// only needs to run on the break.
			if b < 0x80 {
				if shift == 63 && b > 1 {
					return 0, fmt.Errorf("invalid bytes")
				}
				break
			}
		}
		// Guard against int truncation on 32-bit platforms: a uint64
		// length above MaxInt would silently wrap to a small positive int
		// and bypass the iNdEx>l bound check below.
		if length > uint64(math.MaxInt) {
			return 0, fmt.Errorf("invalid bytes")
		}
		iNdEx += int(length)
		if iNdEx < 0 || iNdEx > l {
			return 0, fmt.Errorf("invalid bytes")
		}
	case 3: // start group
		_, n := protowire.ConsumeGroup(protowire.Number(fieldNum), dAtA[iNdEx:])
		if n < 0 {
			return 0, fmt.Errorf("invalid group")
		}
		iNdEx += n
	case 5: // fixed32
		if (iNdEx + 4) > l {
			return 0, fmt.Errorf("truncated fixed32")
		}
		iNdEx += 4
	default:
		return 0, fmt.Errorf("unknown wire type %d", wireType)
	}
	return iNdEx, nil
}
