package push

import "fmt"

// EffectiveStructuredMetadata returns the structured metadata that applies to an entry once
// the shared structured metadata sets of its stream have been merged in: the entry's own
// attributes first, then the resource attributes, then the scope attributes. Resolve resource
// and scope for an entry with Stream.SharedFor.
//
// That is exactly the order the OTLP push path appends in when it expands resource and scope
// attributes onto every entry itself, so materializing the pool here produces the very same
// list of pairs, in the very same order, as an entry whose stream was ingested with expansion
// enabled. Byte identical, duplicate names included.
//
// Duplicate names therefore keep resolving the way they do today, warts and all: the read path
// collapses repeated names down to the last pair, and since the shared attributes come after
// the entry's own ones a name carried by both resolves to the shared value - as long as the
// sort the pairs go through on the way is stable, which it is up to 12 pairs and is not beyond
// that. Preserving that is deliberate. Giving the entry's own attributes precedence, which is
// what OpenTelemetry prescribes and what a deterministic order would let us do, is a behavior
// change on its own and is deferred to follow-up work rather than smuggled in here.
//
// The entry is never modified. Appending the shared labels to Entry.StructuredMetadata in
// place is not safe: entries are handed to the WAL, replication and tailer paths that read
// them concurrently, and append may write into the spare capacity of a slice those paths
// still reference.
//
// The result must be treated as read-only. When there is nothing to merge it aliases the one
// non-empty part instead of copying, so overwriting an element of it would corrupt the entry
// or, worse, a set of the stream-wide pool that other entries also reference. The alias is
// returned with its capacity clamped to its length, so a later append to the result is forced
// to allocate a copy instead of writing into the spare capacity of the aliased slice.
func EffectiveStructuredMetadata(resource, scope, own LabelsAdapter) LabelsAdapter {
	total := len(resource) + len(scope) + len(own)
	if total == 0 {
		return nil
	}

	// Exactly one part holds everything: alias it rather than copying.
	switch total {
	case len(own):
		return own[:len(own):len(own)]
	case len(resource):
		return resource[:len(resource):len(resource)]
	case len(scope):
		return scope[:len(scope):len(scope)]
	}

	merged := make(LabelsAdapter, 0, total)
	merged = append(merged, own...)
	merged = append(merged, resource...)
	merged = append(merged, scope...)
	return merged
}

// SharedFor resolves the shared structured metadata sets an entry of this stream references.
// Either or both may be empty when the entry references no set.
//
// A reference that points past the end of the stream's pool is treated as "no set". That can
// only happen if the producer built the stream wrong, and silently dropping the shared
// metadata of one entry is a better outcome on the ingest path than failing a whole push, so
// the read helpers never error. Consumers that want to catch such a producer bug should call
// ValidateSharedRefs once, when they receive the stream, instead of checking per entry.
//
// The returned lists alias the stream's pool and must be treated as read-only: every entry
// referencing the same set gets the same backing array.
func (s *Stream) SharedFor(e *Entry) (resource, scope LabelsAdapter) {
	return s.sharedSet(e.SharedResourceRef), s.sharedSet(e.SharedScopeRef)
}

// sharedSet resolves a 1-based reference into the stream's pool, returning nil for the 0
// "none" reference and for any out of range reference.
func (s *Stream) sharedSet(ref uint32) LabelsAdapter {
	if ref == 0 || uint64(ref) > uint64(len(s.SharedStructuredMetadataSets)) {
		return nil
	}
	return s.SharedStructuredMetadataSets[ref-1].Attrs
}

// ValidateSharedRefs reports whether every shared structured metadata reference of every
// entry of the stream resolves against the stream's own pool. References are context
// dependent, so a stream whose entries were built against a different pool, or whose pool was
// dropped along the way, is only detectable here.
//
// It is meant to be called once per stream by consumers that would rather reject a malformed
// stream than silently ingest entries missing their shared metadata; SharedFor itself stays
// non-failing. See SharedFor.
func (s *Stream) ValidateSharedRefs() error {
	sets := uint64(len(s.SharedStructuredMetadataSets))
	for i := range s.Entries {
		e := &s.Entries[i]
		if uint64(e.SharedResourceRef) > sets {
			return fmt.Errorf("entry %d references shared structured metadata set %d as its resource set, but the stream carries %d sets", i, e.SharedResourceRef, sets)
		}
		if uint64(e.SharedScopeRef) > sets {
			return fmt.Errorf("entry %d references shared structured metadata set %d as its scope set, but the stream carries %d sets", i, e.SharedScopeRef, sets)
		}
	}
	return nil
}

// StripSharedStructuredMetadata drops the shared structured metadata pool of the stream and the
// references of all of its entries, leaving a stream whose entries carry all their structured
// metadata themselves.
//
// The pool is internal to Loki's OTLP ingest pipeline, where it is populated only for tenants that
// have the deferred expansion of resource and scope attributes enabled. Ingress paths reachable by
// external clients call this so that a client cannot opt itself into deferred semantics whatever
// its tenant is configured for: an entry referencing a pooled set is metered once per stream but
// read back as if the set had been copied onto every entry, so accepting such a stream from
// outside would under-charge the tenant by a factor of its entry count.
//
// Nothing is reported: an unexpected field on an ingest path is dropped, not rejected, the same way
// an unknown proto field would be.
func (s *Stream) StripSharedStructuredMetadata() {
	s.SharedStructuredMetadataSets = nil
	for i := range s.Entries {
		s.Entries[i].SharedResourceRef = 0
		s.Entries[i].SharedScopeRef = 0
	}
}
