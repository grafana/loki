package wirepb

import (
	"bytes"

	"github.com/grafana/dskit/httpgrpc"
)

// HeaderAdapter bridges the gogo-generated httpgrpc.Header type into
// wiresmith-generated code: wiresmith requires message-typed custom types to
// implement its Size/Marshal/Unmarshal/Equal/CompareWiresmith contract, which
// these methods satisfy by delegating to the gogo-generated implementation.
// The wire format is identical to embedding httpgrpc.Header directly.
type HeaderAdapter httpgrpc.Header

// SizeWiresmith implements the wiresmith customtype contract.
func (h *HeaderAdapter) SizeWiresmith() int {
	return (*httpgrpc.Header)(h).Size()
}

// MarshalWiresmith implements the wiresmith customtype contract.
func (h *HeaderAdapter) MarshalWiresmith(buf []byte) (int, error) {
	return (*httpgrpc.Header)(h).MarshalTo(buf)
}

// UnmarshalWiresmith implements the wiresmith customtype contract.
func (h *HeaderAdapter) UnmarshalWiresmith(buf []byte) error {
	return (*httpgrpc.Header)(h).Unmarshal(buf)
}

// EqualWiresmith implements the wiresmith customtype contract.
func (h *HeaderAdapter) EqualWiresmith(other any) bool {
	o, ok := coerceHeaderAdapter(other)
	if !ok {
		return false
	}
	return (*httpgrpc.Header)(h).Equal((*httpgrpc.Header)(o))
}

// CompareWiresmith implements the wiresmith customtype contract. It returns
// -1 on type mismatch so the generated Compare stays total.
//
// Ordering is derived from httpgrpc.Header's own gogo-generated Marshal
// rather than a hand-enumerated Key-then-Values walk. httpgrpc.Header has no
// generated Compare of its own to delegate to (gogo's compare plugin was
// never enabled for this message), so a Key/Values walk was the only option
// when this was first written -- but it silently stops covering the type
// the day dskit's httpgrpc.Header gains a field, unlike EqualWiresmith
// above, which delegates to httpgrpc.Header.Equal and picks up new fields
// automatically (wiresmith-yp37). Marshal-then-bytes.Compare tracks the
// vendored struct the same way Equal does. Matches
// DedicatedColumns.CompareWiresmith in tempo's wiresmith migration and
// Stream.CompareWiresmith in loki's pkg/push (wiresmith-6azr), which hit the
// same hand-enumeration fragility.
func (h *HeaderAdapter) CompareWiresmith(other any) int {
	o, ok := coerceHeaderAdapter(other)
	if !ok {
		return -1
	}
	a, err := (*httpgrpc.Header)(h).Marshal()
	if err != nil {
		return -1
	}
	b, err := (*httpgrpc.Header)(o).Marshal()
	if err != nil {
		return 1
	}
	return bytes.Compare(a, b)
}

func coerceHeaderAdapter(other any) (*HeaderAdapter, bool) {
	switch o := other.(type) {
	case HeaderAdapter:
		return &o, true
	case *HeaderAdapter:
		return o, o != nil
	default:
		return nil, false
	}
}

// HeadersToAdapters converts a gogo-shaped httpgrpc header slice into the
// wiresmith customtype representation. nil elements are skipped.
func HeadersToAdapters(hs []*httpgrpc.Header) []HeaderAdapter {
	if hs == nil {
		return nil
	}
	out := make([]HeaderAdapter, 0, len(hs))
	for _, h := range hs {
		if h == nil {
			continue
		}
		out = append(out, HeaderAdapter(*h))
	}
	return out
}

// AdaptersToHeaders converts the wiresmith customtype representation back to
// the gogo-shaped httpgrpc header slice.
func AdaptersToHeaders(hs []HeaderAdapter) []*httpgrpc.Header {
	if hs == nil {
		return nil
	}
	out := make([]*httpgrpc.Header, len(hs))
	for i := range hs {
		h := httpgrpc.Header(hs[i])
		out[i] = &h
	}
	return out
}
