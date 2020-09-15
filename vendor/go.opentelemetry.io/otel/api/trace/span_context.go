// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trace

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
)

const (
	// FlagsSampled is a bitmask with the sampled bit set. A SpanContext
	// with the sampling bit set means the span is sampled.
	FlagsSampled = byte(0x01)
	// FlagsDeferred is a bitmask with the deferred bit set. A SpanContext
	// with the deferred bit set means the sampling decision has been
	// defered to the receiver.
	FlagsDeferred = byte(0x02)
	// FlagsDebug is a bitmask with the debug bit set.
	FlagsDebug = byte(0x04)

	ErrInvalidHexID errorConst = "trace-id and span-id can only contain [0-9a-f] characters, all lowercase"

	ErrInvalidTraceIDLength errorConst = "hex encoded trace-id must have length equals to 32"
	ErrNilTraceID           errorConst = "trace-id can't be all zero"

	ErrInvalidSpanIDLength errorConst = "hex encoded span-id must have length equals to 16"
	ErrNilSpanID           errorConst = "span-id can't be all zero"
)

type errorConst string

func (e errorConst) Error() string {
	return string(e)
}

// ID is a unique identity of a trace.
type ID [16]byte

var nilTraceID ID
var _ json.Marshaler = nilTraceID

// IsValid checks whether the trace ID is valid. A valid trace ID does
// not consist of zeros only.
func (t ID) IsValid() bool {
	return !bytes.Equal(t[:], nilTraceID[:])
}

// MarshalJSON implements a custom marshal function to encode TraceID
// as a hex string.
func (t ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.String())
}

// String returns the hex string representation form of a TraceID
func (t ID) String() string {
	return hex.EncodeToString(t[:])
}

// SpanID is a unique identify of a span in a trace.
type SpanID [8]byte

var nilSpanID SpanID
var _ json.Marshaler = nilSpanID

// IsValid checks whether the span ID is valid. A valid span ID does
// not consist of zeros only.
func (s SpanID) IsValid() bool {
	return !bytes.Equal(s[:], nilSpanID[:])
}

// MarshalJSON implements a custom marshal function to encode SpanID
// as a hex string.
func (s SpanID) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// String returns the hex string representation form of a SpanID
func (s SpanID) String() string {
	return hex.EncodeToString(s[:])
}

// IDFromHex returns a TraceID from a hex string if it is compliant
// with the w3c trace-context specification.
// See more at https://www.w3.org/TR/trace-context/#trace-id
func IDFromHex(h string) (ID, error) {
	t := ID{}
	if len(h) != 32 {
		return t, ErrInvalidTraceIDLength
	}

	if err := decodeHex(h, t[:]); err != nil {
		return t, err
	}

	if !t.IsValid() {
		return t, ErrNilTraceID
	}
	return t, nil
}

// SpanIDFromHex returns a SpanID from a hex string if it is compliant
// with the w3c trace-context specification.
// See more at https://www.w3.org/TR/trace-context/#parent-id
func SpanIDFromHex(h string) (SpanID, error) {
	s := SpanID{}
	if len(h) != 16 {
		return s, ErrInvalidSpanIDLength
	}

	if err := decodeHex(h, s[:]); err != nil {
		return s, err
	}

	if !s.IsValid() {
		return s, ErrNilSpanID
	}
	return s, nil
}

func decodeHex(h string, b []byte) error {
	for _, r := range h {
		switch {
		case 'a' <= r && r <= 'f':
			continue
		case '0' <= r && r <= '9':
			continue
		default:
			return ErrInvalidHexID
		}
	}

	decoded, err := hex.DecodeString(h)
	if err != nil {
		return err
	}

	copy(b, decoded)
	return nil
}

// SpanContext contains basic information about the span - its trace
// ID, span ID and trace flags.
type SpanContext struct {
	TraceID    ID
	SpanID     SpanID
	TraceFlags byte
}

// EmptySpanContext is meant for internal use to return invalid span
// context during error conditions.
func EmptySpanContext() SpanContext {
	return SpanContext{}
}

// IsValid checks if the span context is valid. A valid span context
// has a valid trace ID and a valid span ID.
func (sc SpanContext) IsValid() bool {
	return sc.HasTraceID() && sc.HasSpanID()
}

// HasTraceID checks if the span context has a valid trace ID.
func (sc SpanContext) HasTraceID() bool {
	return sc.TraceID.IsValid()
}

// HasSpanID checks if the span context has a valid span ID.
func (sc SpanContext) HasSpanID() bool {
	return sc.SpanID.IsValid()
}

// isDeferred returns if the deferred bit is set in the trace flags.
func (sc SpanContext) isDeferred() bool {
	return sc.TraceFlags&FlagsDeferred == FlagsDeferred
}

// isDebug returns if the debug bit is set in the trace flags.
func (sc SpanContext) isDebug() bool {
	return sc.TraceFlags&FlagsDebug == FlagsDebug
}

// IsSampled returns if the sampling bit is set in the trace flags.
func (sc SpanContext) IsSampled() bool {
	return sc.TraceFlags&FlagsSampled == FlagsSampled
}
