// Package ulid provides a ULID implementation. It wraps around the
// github.com/oklog/ulid/v2 package but supports being used as a custom type in
// a gogoproto generated struct.
package ulid

import (
	"encoding/json"

	"github.com/oklog/ulid/v2"
)

// A ULID is a 16-byte Universally Unique Lexicographically Sortable Identifier.
type ULID ulid.ULID

// Marshal marshals id as a [ProtoULID].
func (id ULID) Marshal() ([]byte, error) {
	pb := ProtoULID{Value: id[:]}
	return pb.Marshal()
}

// MarshalTo marshals id as a [ProtoULID] to the given data slice. The data
// buffer must be at least [ULID.Size] bytes, otherwise MarshalTo panics.
func (id *ULID) MarshalTo(data []byte) (int, error) {
	pb := ProtoULID{Value: id[:]}
	return pb.MarshalTo(data)
}

// Unmarshal unmarshals a [ProtoULID] from the given data slice.
func (id *ULID) Unmarshal(data []byte) error {
	pb := ProtoULID{}
	if err := pb.Unmarshal(data); err != nil {
		return err
	}

	var inner ulid.ULID
	if err := inner.UnmarshalBinary(pb.GetValue()); err != nil {
		return err
	}

	*id = ULID(inner)
	return nil
}

// Size returns the size of the ULID when marshaled as a [ProtoULID].
func (id *ULID) Size() int {
	pb := ProtoULID{Value: id[:]}
	return pb.Size()
}

// MarshalJSON marshals the ULID to a JSON string.
func (id ULID) MarshalJSON() ([]byte, error) {
	text, _ := ulid.ULID(id).MarshalText()
	return json.Marshal(string(text))
}

// UnmarshalJSON unmarshals a JSON string into a ULID.
func (id *ULID) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}

	var inner ulid.ULID
	if err := inner.UnmarshalText([]byte(text)); err != nil {
		return err
	}

	*id = ULID(inner)
	return nil
}

// Compare compares the id to another ULID.
func (id ULID) Compare(other ULID) int {
	return ulid.ULID(id).Compare(ulid.ULID(other))
}

// Equal returns true if the id is equal to another ULID.
func (id ULID) Equal(other ULID) bool {
	return id.Compare(other) == 0
}

// String returns the ULID as a string.
func (id ULID) String() string {
	return ulid.ULID(id).String()
}

// SizeWiresmith returns the size of the ULID payload when marshaled as a
// [ProtoULID], implementing the wiresmith customtype contract. It never
// returns 0 so the field is always emitted, matching the previous
// gogoproto non-nullable customtype behavior.
func (id *ULID) SizeWiresmith() int {
	pb := ProtoULID{Value: id[:]}
	return pb.Size()
}

// MarshalWiresmith marshals id as a [ProtoULID] into buf, implementing the
// wiresmith customtype contract. buf is sized to exactly SizeWiresmith bytes.
func (id *ULID) MarshalWiresmith(buf []byte) (int, error) {
	pb := ProtoULID{Value: id[:]}
	return pb.MarshalTo(buf)
}

// UnmarshalWiresmith unmarshals a [ProtoULID] payload, implementing the
// wiresmith customtype contract.
func (id *ULID) UnmarshalWiresmith(buf []byte) error {
	return id.Unmarshal(buf)
}

// EqualWiresmith implements the wiresmith customtype contract.
func (id *ULID) EqualWiresmith(other any) bool {
	o, ok := coerceULID(other)
	if !ok {
		return false
	}
	return *id == o
}

// CompareWiresmith implements the wiresmith customtype contract. It returns
// -1 on type mismatch so the generated Compare stays total.
func (id *ULID) CompareWiresmith(other any) int {
	o, ok := coerceULID(other)
	if !ok {
		return -1
	}
	return id.Compare(o)
}

// coerceULID accepts the value and pointer shapes the generated code may
// pass to the *Wiresmith comparison methods.
func coerceULID(other any) (ULID, bool) {
	switch o := other.(type) {
	case ULID:
		return o, true
	case *ULID:
		if o == nil {
			return ULID{}, false
		}
		return *o, true
	default:
		return ULID{}, false
	}
}
