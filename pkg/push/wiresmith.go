package push

import "bytes"

// Wiresmith customtype adapter methods. The wiresmith protobuf compiler
// (used for pkg/logproto in the main Loki module) requires custom types to
// implement its Size/Marshal/Unmarshal/Equal/CompareWiresmith contract.
// These delegate to the existing gogo-style implementations and keep the
// wire format unchanged. They are plain Go methods: pkg/push takes no
// dependency on the wiresmith module.

// SizeWiresmith implements the wiresmith customtype contract.
func (m *Stream) SizeWiresmith() int { return m.Size() }

// MarshalWiresmith implements the wiresmith customtype contract.
func (m *Stream) MarshalWiresmith(buf []byte) (int, error) { return m.MarshalTo(buf) }

// UnmarshalWiresmith implements the wiresmith customtype contract.
func (m *Stream) UnmarshalWiresmith(buf []byte) error { return m.Unmarshal(buf) }

// EqualWiresmith implements the wiresmith customtype contract.
func (m *Stream) EqualWiresmith(other any) bool { return m.Equal(other) }

// CompareWiresmith implements the wiresmith customtype contract. Streams
// have no natural order; equality decides 0, otherwise label order, then
// entry count, then the marshaled form provide a stable total order.
//
// The marshaled-form tiebreak matters for correctness, not just style: two
// Streams with equal Labels and equal Entries length but differing entry
// content (or only a differing Hash) used to both fall through to the same
// `return 1`, regardless of which side was the receiver, so
// A.Compare(B)==1 && B.Compare(A)==1 could both hold -- not a valid
// ordering. Comparing the marshaled form (like
// DedicatedColumns.CompareWiresmith in tempo's wiresmith migration) covers
// every field, including Hash, and keeps the result antisymmetric.
func (m *Stream) CompareWiresmith(other any) int {
	o, ok := other.(*Stream)
	if !ok {
		if ov, ok2 := other.(Stream); ok2 {
			o = &ov
		} else {
			return -1
		}
	}
	if m.Equal(o) {
		return 0
	}
	if m.Labels != o.Labels {
		if m.Labels < o.Labels {
			return -1
		}
		return 1
	}
	if len(m.Entries) != len(o.Entries) {
		if len(m.Entries) < len(o.Entries) {
			return -1
		}
		return 1
	}
	a, err := m.Marshal()
	if err != nil {
		return -1
	}
	b, err := o.Marshal()
	if err != nil {
		return 1
	}
	return bytes.Compare(a, b)
}

// SizeWiresmith implements the wiresmith customtype contract.
func (m *LabelAdapter) SizeWiresmith() int { return m.Size() }

// MarshalWiresmith implements the wiresmith customtype contract.
func (m *LabelAdapter) MarshalWiresmith(buf []byte) (int, error) { return m.MarshalTo(buf) }

// UnmarshalWiresmith implements the wiresmith customtype contract.
func (m *LabelAdapter) UnmarshalWiresmith(buf []byte) error { return m.Unmarshal(buf) }

// EqualWiresmith implements the wiresmith customtype contract.
func (m *LabelAdapter) EqualWiresmith(other any) bool {
	o, ok := coerceLabelAdapter(other)
	if !ok {
		return false
	}
	return m.Equal(o)
}

// CompareWiresmith implements the wiresmith customtype contract.
func (m *LabelAdapter) CompareWiresmith(other any) int {
	o, ok := coerceLabelAdapter(other)
	if !ok {
		return -1
	}
	return m.Compare(o)
}

func coerceLabelAdapter(other any) (LabelAdapter, bool) {
	switch o := other.(type) {
	case LabelAdapter:
		return o, true
	case *LabelAdapter:
		if o == nil {
			return LabelAdapter{}, false
		}
		return *o, true
	default:
		return LabelAdapter{}, false
	}
}
