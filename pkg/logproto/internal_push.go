package logproto

import (
	"github.com/grafana/loki/pkg/push"
)

// This file holds the hand-written companions to the generated internal push types: the rules
// that must not be restated at each call site, because restating them is how a site comes to
// forget that a resource or a scope attribute applies to an entry.

// EntryCount is the number of entries the stream holds, across every group and scope.
func (s *InternalStreamAdapter) EntryCount() int {
	n := 0
	for i := range s.ResourceLogs {
		for j := range s.ResourceLogs[i].ScopeLogs {
			n += len(s.ResourceLogs[i].ScopeLogs[j].Entries)
		}
	}
	return n
}

// AppendEffectiveMetadata appends everything that applies to an entry — its own pairs, then
// its resource's, then its scope's — to dst and returns the result.
//
// Reading only an entry's own StructuredMetadata silently misses attributes that arrived on
// the resource or the scope, which is the mistake this exists to prevent.
//
// The order, and the duplicate a repeated name leaves behind, are exactly what the OTLP parse
// site produces when it expands attributes onto entries itself. Reproducing it is what makes
// a nested record and its flat equivalent store identical bytes.
func AppendEffectiveMetadata(dst, resAttrs, scopeAttrs []push.LabelAdapter, e *push.Entry) []push.LabelAdapter {
	dst = append(dst, e.StructuredMetadata...)
	dst = append(dst, resAttrs...)
	return append(dst, scopeAttrs...)
}

// FromStream wraps a flat stream as an internal one, in a single group and scope with no
// shared attributes.
//
// This is the native push path: a logproto.Stream carries every attribute on every entry
// already, so there is nothing to lift out. It costs one group and one scope header per
// stream and no per-entry work, because the entries slice is taken as it is.
func FromStream(s Stream) InternalStreamAdapter {
	return InternalStreamAdapter{
		Labels: s.Labels,
		Hash:   s.Hash,
		ResourceLogs: []ResourceLogs{{
			ScopeLogs: []ScopeLogs{{Entries: s.Entries}},
		}},
	}
}

// ToStream flattens the internal stream back into the wire format Loki has always used,
// resolving each entry's effective metadata onto it.
//
// It is the expensive direction: every entry beneath a resource or scope that carries
// attributes gets a fresh metadata slice.
func (s *InternalStreamAdapter) ToStream() Stream {
	out := Stream{
		Labels: s.Labels,
		Hash:   s.Hash,
	}

	count := s.EntryCount()
	if count == 0 {
		// Left nil rather than an empty slice, because the flat form unmarshals to nil when
		// it carries no entries and the two must be indistinguishable.
		return out
	}
	out.Entries = make([]push.Entry, 0, count)

	for i := range s.ResourceLogs {
		res := &s.ResourceLogs[i]
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]

			nothingLifted := len(res.Attrs) == 0 && len(scope.Attrs) == 0
			if nothingLifted {
				out.Entries = append(out.Entries, scope.Entries...)
				continue
			}

			for k := range scope.Entries {
				e := scope.Entries[k]
				md := make([]push.LabelAdapter, 0, len(e.StructuredMetadata)+len(res.Attrs)+len(scope.Attrs))
				e.StructuredMetadata = AppendEffectiveMetadata(md, res.Attrs, scope.Attrs, &scope.Entries[k])
				out.Entries = append(out.Entries, e)
			}
		}
	}
	return out
}
