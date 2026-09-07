package logproto

import (
	"slices"

	"github.com/grafana/loki/pkg/push"
)

// This file holds the hand-written companions to the generated internal push types: the rules
// that must not be restated at each call site, because restating them is how a site comes to
// forget that a resource or a scope attribute applies to an entry.

// entryCount is the number of entries the stream holds, across every group and scope.
func (s *InternalStreamAdapter) entryCount() int {
	n := 0
	for i := range s.ResourceLogs {
		for j := range s.ResourceLogs[i].ScopeLogs {
			n += len(s.ResourceLogs[i].ScopeLogs[j].Entries)
		}
	}
	return n
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

// ToStream flattens the internal stream into out, in the wire format Loki has always used,
// resolving each entry's effective metadata onto it.
//
// It is the expensive direction: every entry beneath a resource or scope that carries
// attributes gets a fresh metadata slice.
func (s *InternalStreamAdapter) ToStream(out *Stream) {
	out.Labels = s.Labels
	out.Hash = s.Hash
	out.Entries = out.Entries[:0]
	count := s.entryCount()
	if count == 0 {
		return
	}
	out.Entries = slices.Grow(out.Entries, count)

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
				md = append(md, e.StructuredMetadata...)
				md = append(md, res.Attrs...)
				md = append(md, scope.Attrs...)

				e.StructuredMetadata = md
				out.Entries = append(out.Entries, e)
			}
		}
	}
}
