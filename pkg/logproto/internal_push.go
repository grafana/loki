package logproto

import (
	"slices"

	"github.com/grafana/loki/pkg/push"
)

// EachGroup visits scopes in resource/scope order with their shared attributes and entries.
// Entries may be updated in place; attribute slices may be shared with other groups.
func (s *InternalStreamAdapter) EachGroup(fn func(resourceAttrs, scopeAttrs []push.LabelAdapter, entries []push.Entry)) {
	for i := range s.ResourceLogs {
		resource := &s.ResourceLogs[i]
		for j := range resource.ScopeLogs {
			scope := &resource.ScopeLogs[j]
			fn(resource.Attrs, scope.Attrs, scope.Entries)
		}
	}
}

// EachEntryWithShared visits entries in resource/scope order with their shared attributes.
// The entry pointer and attribute slices refer to the stream's storage.
func (s *InternalStreamAdapter) EachEntryWithShared(fn func(entry *push.Entry, resourceAttrs, scopeAttrs []push.LabelAdapter)) {
	s.EachGroup(func(resourceAttrs, scopeAttrs []push.LabelAdapter, entries []push.Entry) {
		for i := range entries {
			fn(&entries[i], resourceAttrs, scopeAttrs)
		}
	})
}

// EntryCount returns the total entry count across all resources and scopes.
func (s *InternalStreamAdapter) EntryCount() int {
	n := 0
	s.EachGroup(func(_, _ []push.LabelAdapter, entries []push.Entry) {
		n += len(entries)
	})
	return n
}

// FromPushRequest wraps each flat stream in one resource and scope with no shared attributes.
// Entry slices are shared with req; wrapper storage is allocated once per request.
func FromPushRequest(req *PushRequest) InternalPushRequest {
	streams := make([]InternalStreamAdapter, len(req.Streams))
	resources := make([]ResourceLogs, len(req.Streams))
	scopes := make([]ScopeLogs, len(req.Streams))
	for i, stream := range req.Streams {
		scopes[i].Entries = stream.Entries
		// Cap each group slice so appends cannot overwrite another stream's groups.
		resources[i].ScopeLogs = scopes[i : i+1 : i+1]
		streams[i] = InternalStreamAdapter{
			Labels:       stream.Labels,
			Hash:         stream.Hash,
			ResourceLogs: resources[i : i+1 : i+1],
		}
	}
	return InternalPushRequest{Streams: streams, Format: req.Format}
}

// FromStream wraps a flat stream in one resource and scope with no shared attributes.
// The entries are shared with s.
func FromStream(s Stream) InternalStreamAdapter {
	return InternalStreamAdapter{
		Labels: s.Labels,
		Hash:   s.Hash,
		ResourceLogs: []ResourceLogs{{
			ScopeLogs: []ScopeLogs{{Entries: s.Entries}},
		}},
	}
}

// FlatView returns a flat stream for read-only use. It shares entries and metadata when
// there is one resource and scope with no shared attributes; otherwise it uses ToStream.
// Do not modify the returned entries or their metadata. Use ToStream for a separate entry slice.
func (s *InternalStreamAdapter) FlatView() Stream {
	if len(s.ResourceLogs) == 1 && len(s.ResourceLogs[0].ScopeLogs) == 1 {
		resource := &s.ResourceLogs[0]
		scope := &resource.ScopeLogs[0]
		if len(resource.Attrs) == 0 && len(scope.Attrs) == 0 {
			return Stream{Labels: s.Labels, Hash: s.Hash, Entries: scope.Entries}
		}
	}

	var out Stream
	s.ToStream(&out)
	return out
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
	count := s.EntryCount()
	if count == 0 {
		return
	}
	out.Entries = slices.Grow(out.Entries, count)

	// Reuse scratch space across groups.
	var sharedAttrs []push.LabelAdapter

	s.EachGroup(func(resourceAttrs, scopeAttrs []push.LabelAdapter, entries []push.Entry) {
		if len(resourceAttrs) == 0 && len(scopeAttrs) == 0 {
			out.Entries = append(out.Entries, entries...)
			return
		}

		sharedAttrs = append(sharedAttrs[:0], scopeAttrs...)
		// only add resource attributes if they are not overridden by scope attrs already
		for _, resAttr := range resourceAttrs {
			if !hasName(sharedAttrs, resAttr.Name) {
				sharedAttrs = append(sharedAttrs, resAttr)
			}
		}

		for k := range entries {
			e := entries[k]

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
	})
}

func hasName(attrs []push.LabelAdapter, name string) bool {
	for i := range attrs {
		if attrs[i].Name == name {
			return true
		}
	}
	return false
}
