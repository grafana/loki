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

// ToStream expands nested OTLP-like InternalStreamAdapter structure to logproto.Stream.
// It copies structured metadata from the top levels to each logproto.Entry. If an attribute
// is duplicated (e.g., exists in resource, scope, and entry) then the precedence is
// entry >> scope >> resource.
//
// out is reused: its entries slice is truncated and refilled, so an older result is not a
// snapshot. Copy it if you need to keep one. Entries with no attributes to resolve are copied as
// they are, so their structured metadata is shared with s and must not be written to.
func (s *InternalStreamAdapter) ToStream(out *Stream) {
	out.Labels = s.Labels
	out.Hash = s.Hash
	out.Entries = out.Entries[:0]
	count := s.entryCount()
	if count == 0 {
		return
	}
	out.Entries = slices.Grow(out.Entries, count)

	var sharedAttrs []push.LabelAdapter

	for i := range s.ResourceLogs {
		res := s.ResourceLogs[i]
		for j := range res.ScopeLogs {
			scope := res.ScopeLogs[j]

			hasSharedAttrs := len(res.Attrs) > 0 || len(scope.Attrs) > 0
			if !hasSharedAttrs {
				out.Entries = append(out.Entries, scope.Entries...)
				continue
			}

			sharedAttrs = append(sharedAttrs[:0], scope.Attrs...)
			// only add resource attributes if they are not overridden by scope attrs already
			for _, resAttr := range res.Attrs {
				if !hasName(sharedAttrs, resAttr.Name) {
					sharedAttrs = append(sharedAttrs, resAttr)
				}
			}

			for k := range scope.Entries {
				e := scope.Entries[k]

				md := make([]push.LabelAdapter, 0, len(e.StructuredMetadata)+len(sharedAttrs))
				md = append(md, e.StructuredMetadata...)

				for _, sharedAttr := range sharedAttrs {
					if !hasName(md, sharedAttr.Name) {
						md = append(md, sharedAttr)
					}
				}

				e.StructuredMetadata = md
				out.Entries = append(out.Entries, e)
			}
		}
	}
}

func hasName(attrs []push.LabelAdapter, name string) bool {
	for i := range attrs {
		if attrs[i].Name == name {
			return true
		}
	}
	return false
}
