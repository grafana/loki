package wirepb

import (
	"strings"

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
func (h *HeaderAdapter) CompareWiresmith(other any) int {
	o, ok := coerceHeaderAdapter(other)
	if !ok {
		return -1
	}
	if c := strings.Compare(h.Key, o.Key); c != 0 {
		return c
	}
	if c := len(h.Values) - len(o.Values); c != 0 {
		if c < 0 {
			return -1
		}
		return 1
	}
	for i := range h.Values {
		if c := strings.Compare(h.Values[i], o.Values[i]); c != 0 {
			return c
		}
	}
	return 0
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
