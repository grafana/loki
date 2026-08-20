package logproto

import (
	"slices"
	"time"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// This file holds the hand-written companions to the generated internal push types.
//
// The write path works on these types directly: there is one internal representation, not
// an abstraction over two. What lives here is only the handful of rules that must not be
// restated at each call site, because restating them is how a site comes to forget that a
// resource or a scope attribute applies to an entry:
//
//   - an entry's effective metadata is its own pairs plus the resource's plus the scope's
//   - accounted size counts each shared set once, not once per entry beneath it
//   - dropping entries prunes the scopes and groups left empty
//
// Everything else — iterating to read a line, appending a detected level — is a plain loop
// at the call site, which keeps the cost visible and needs nothing from this file.

// excludedFromSize are the structured metadata labels a tenant is not charged for, because
// Loki adds them rather than the tenant sending them. It mirrors
// util.ExcludedStructuredMetadataLabels, which cannot be used here without pkg/logproto
// depending on pkg/util.
var excludedFromSize = [...]string{constants.LevelLabel}

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

// UnexpandedSize is what the tenant sent: every line, every entry's own metadata, and each
// shared attribute set counted once.
//
// This is the basis for rate limiting, discard accounting and usage, because it is the
// volume the tenant is answerable for. It is smaller than what the flattened form measures
// whenever a resource or scope carries attributes.
func (s *InternalStreamAdapter) UnexpandedSize() int {
	size := 0
	for i := range s.ResourceLogs {
		res := &s.ResourceLogs[i]
		size += attrsSize(res.Attrs)
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]
			size += attrsSize(scope.Attrs)
			for k := range scope.Entries {
				e := &scope.Entries[k]
				size += len(e.Line) + attrsSize(e.StructuredMetadata)
			}
		}
	}
	return size
}

// ExpandedSize is what the flattened form measures: each shared set counted once for every
// entry beneath it.
//
// Shard-count decisions and the rate store use this, so that deferring the expansion does
// not quietly change how traffic is spread across ingesters. For a stream whose resources
// and scopes carry no attributes the two sizes are equal, which is the case for every
// stream that arrived over the native push API.
func (s *InternalStreamAdapter) ExpandedSize() int {
	size := 0
	for i := range s.ResourceLogs {
		res := &s.ResourceLogs[i]
		resSize := attrsSize(res.Attrs)
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]
			shared := resSize + attrsSize(scope.Attrs)
			for k := range scope.Entries {
				e := &scope.Entries[k]
				size += len(e.Line) + attrsSize(e.StructuredMetadata) + shared
			}
		}
	}
	return size
}

// AppendEffectiveMetadata appends everything that applies to an entry — its own pairs, then
// its resource's, then its scope's — to dst and returns the result.
//
// Reading only an entry's own StructuredMetadata silently misses attributes that arrived on
// the resource or the scope, so any site that inspects metadata must come through here.
// dst is reused, so a loop over a stream pays no allocation per entry.
//
// The pairs are neither sorted nor deduplicated: the distributor puts them through a
// labels.Builder, which does both.
func AppendEffectiveMetadata(dst, resAttrs, scopeAttrs []push.LabelAdapter, e *push.Entry) []push.LabelAdapter {
	dst = append(dst, e.StructuredMetadata...)
	dst = append(dst, resAttrs...)
	return append(dst, scopeAttrs...)
}

// EffectiveMetadataSize is the accounted size of an entry's effective metadata, without
// building the list.
func EffectiveMetadataSize(resAttrs, scopeAttrs []push.LabelAdapter, e *push.Entry) int {
	return attrsSize(e.StructuredMetadata) + attrsSize(resAttrs) + attrsSize(scopeAttrs)
}

// attrsSize is the accounting rule in one place: the bytes of each name and value, skipping
// the labels Loki adds on the tenant's behalf.
func attrsSize(attrs []push.LabelAdapter) int {
	size := 0
	for i := range attrs {
		if slices.Contains(excludedFromSize[:], attrs[i].Name) {
			continue
		}
		size += len(attrs[i].Name) + len(attrs[i].Value)
	}
	return size
}

// Filter keeps the entries for which keep returns true and removes the rest, reporting how
// many it removed.
//
// A scope or a group left holding no entries is pruned, so an empty group never reaches the
// wire. keep receives the resource and scope the entry sits under so that it can consult the
// entry's effective metadata.
func (s *InternalStreamAdapter) Filter(keep func(res *ResourceLogs, scope *ScopeLogs, e *push.Entry) bool) (dropped int) {
	groups := 0
	for i := range s.ResourceLogs {
		res := &s.ResourceLogs[i]

		scopes := 0
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]

			kept := 0
			for k := range scope.Entries {
				if !keep(res, scope, &scope.Entries[k]) {
					dropped++
					continue
				}
				if kept != k {
					scope.Entries[kept] = scope.Entries[k]
				}
				kept++
			}
			scope.Entries = scope.Entries[:kept]

			if kept == 0 {
				continue // an empty scope carries nothing
			}
			if scopes != j {
				res.ScopeLogs[scopes] = res.ScopeLogs[j]
			}
			scopes++
		}
		res.ScopeLogs = res.ScopeLogs[:scopes]

		if scopes == 0 {
			continue
		}
		if groups != i {
			s.ResourceLogs[groups] = s.ResourceLogs[i]
		}
		groups++
	}
	s.ResourceLogs = s.ResourceLogs[:groups]

	return dropped
}

// Divide assigns every entry to one of parts and returns those parts, each keeping the
// nesting of the original.
//
// assign is called exactly once per entry, in containment order, with a running index and
// the entry, so it may accumulate state. An entry assigned an index outside [0, parts) is
// discarded.
//
// A part receives only the groups and scopes that some of its entries came from, carrying
// the same attributes, so no part holds an empty group. The result always has exactly parts
// elements, in index order, so a caller can map a part back to what it means — shard 3 must
// carry the shard label 3 even if shard 2 came out empty.
func (s *InternalStreamAdapter) Divide(parts int, assign func(idx int, e *push.Entry) int) []InternalStreamAdapter {
	if parts <= 0 {
		return nil
	}

	out := make([]InternalStreamAdapter, parts)
	// Which source group and scope each part's open group came from, so that entries from
	// one source scope collect together instead of opening a group each.
	lastRes := make([]int, parts)
	lastScope := make([]int, parts)
	for p := range out {
		out[p].Labels, out[p].Hash = s.Labels, s.Hash
		lastRes[p], lastScope[p] = -1, -1
	}

	idx := 0
	for i := range s.ResourceLogs {
		res := &s.ResourceLogs[i]
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]
			for k := range scope.Entries {
				entry := &scope.Entries[k]

				part := assign(idx, entry)
				idx++
				if part < 0 || part >= parts {
					continue
				}
				dst := &out[part]

				switch {
				case lastRes[part] != i:
					dst.ResourceLogs = append(dst.ResourceLogs, ResourceLogs{
						Attrs:     res.Attrs,
						ScopeLogs: []ScopeLogs{{Attrs: scope.Attrs}},
					})
					lastRes[part], lastScope[part] = i, j

				case lastScope[part] != j:
					group := &dst.ResourceLogs[len(dst.ResourceLogs)-1]
					group.ScopeLogs = append(group.ScopeLogs, ScopeLogs{Attrs: scope.Attrs})
					lastScope[part] = j
				}

				group := &dst.ResourceLogs[len(dst.ResourceLogs)-1]
				target := &group.ScopeLogs[len(group.ScopeLogs)-1]
				target.Entries = append(target.Entries, *entry)
			}
		}
	}
	return out
}

// SortByTimestamp orders entries by timestamp within each scope. It is stable, so entries
// sharing a timestamp keep their arrival order.
//
// This is not a global sort: an entry cannot leave the resource and scope whose attributes
// apply to it, so a stream carrying several groups is ordered only within each of them. For
// a stream of one group and one scope — which is every native push — it is a full sort.
func (s *InternalStreamAdapter) SortByTimestamp() {
	for i := range s.ResourceLogs {
		for j := range s.ResourceLogs[i].ScopeLogs {
			slices.SortStableFunc(s.ResourceLogs[i].ScopeLogs[j].Entries, func(a, b push.Entry) int {
				return a.Timestamp.Compare(b.Timestamp)
			})
		}
	}
}

// RewriteSharedAttrs replaces every group's and scope's attributes with the result of fn.
//
// It is copy-on-write by contract: fn must not write through the slice it is given, and
// returns either that same slice or a new one. The parser hands the same resource's
// attributes to every stream that resource fed, so writing through would change the value
// for streams that are not being processed.
//
// Rewriting once per group rather than once per entry is not only cheaper than the flattened
// form's per-entry work, it is the only correct way: the set is shared, so it cannot be
// edited in place while any entry still refers to it.
func (s *InternalStreamAdapter) RewriteSharedAttrs(fn func(attrs []push.LabelAdapter) []push.LabelAdapter) {
	for i := range s.ResourceLogs {
		res := &s.ResourceLogs[i]
		res.Attrs = fn(res.Attrs)
		for j := range res.ScopeLogs {
			res.ScopeLogs[j].Attrs = fn(res.ScopeLogs[j].Attrs)
		}
	}
}

// TruncateLines shortens any line longer than maxLen so that the result, suffix included, is
// at most maxLen bytes. It reports how many lines it changed and how many bytes it removed.
//
// A line is left untouched when maxLen is not positive, or when the suffix alone would fill
// the whole budget. Cutting on a byte boundary can split a multi-byte rune; that is what the
// distributor has always done and is not changed here.
func (s *InternalStreamAdapter) TruncateLines(maxLen int, suffix string) (truncated, bytesRemoved int) {
	if maxLen <= 0 {
		return 0, 0
	}
	keep := maxLen - len(suffix)
	if keep <= 0 {
		return 0, 0
	}

	for i := range s.ResourceLogs {
		for j := range s.ResourceLogs[i].ScopeLogs {
			entries := s.ResourceLogs[i].ScopeLogs[j].Entries
			for k := range entries {
				if len(entries[k].Line) <= maxLen {
					continue
				}
				bytesRemoved += len(entries[k].Line) - keep
				entries[k].Line = entries[k].Line[:keep] + suffix
				truncated++
			}
		}
	}
	return truncated, bytesRemoved
}

// EnforceTimestampOrder nudges an entry forward by a nanosecond when it collides with the
// previous entry's timestamp and carries different content, reporting how many it moved.
//
// Two entries with the same timestamp and the same line are meant to be de-duplicated
// further down, so only distinct content is separated. The "previous accepted" timestamp is
// seeded from the first entry and only advances on the branch that does not adjust, which is
// what lets a whole run colliding on one timestamp spread out a nanosecond at a time.
//
// This walks the stream in containment order, which is the order the entries will be
// written. It is not a global sort: an entry cannot move between scopes.
func (s *InternalStreamAdapter) EnforceTimestampOrder() (adjusted int) {
	var (
		prev   *push.Entry
		prevTs time.Time
	)
	for i := range s.ResourceLogs {
		for j := range s.ResourceLogs[i].ScopeLogs {
			entries := s.ResourceLogs[i].ScopeLogs[j].Entries
			for k := range entries {
				cur := &entries[k]
				if prev == nil {
					prev, prevTs = cur, cur.Timestamp
					continue
				}

				ts := cur.Timestamp
				if prev.Line != cur.Line && (ts.Equal(prevTs) || ts.Equal(prev.Timestamp)) {
					cur.Timestamp = prev.Timestamp.Add(time.Nanosecond)
					adjusted++
				} else {
					prevTs = ts
				}
				prev = cur
			}
		}
	}
	return adjusted
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
// This is what the send path uses while the config that enables the new encoding is off, and
// what any consumer that has not been converted still needs. It is the expensive direction:
// every entry beneath a resource or scope that carries attributes gets a fresh metadata
// slice.
func (s *InternalStreamAdapter) ToStream() Stream {
	out := Stream{
		Labels:  s.Labels,
		Hash:    s.Hash,
		Entries: make([]push.Entry, 0, s.EntryCount()),
	}

	for i := range s.ResourceLogs {
		res := &s.ResourceLogs[i]
		for j := range res.ScopeLogs {
			scope := &res.ScopeLogs[j]

			// Nothing was lifted out of these entries, so they can go across untouched.
			// Every native push takes this path.
			if len(res.Attrs) == 0 && len(scope.Attrs) == 0 {
				out.Entries = append(out.Entries, scope.Entries...)
				continue
			}

			for k := range scope.Entries {
				e := scope.Entries[k]
				md := make([]push.LabelAdapter, 0, len(e.StructuredMetadata)+len(res.Attrs)+len(scope.Attrs))
				// Concatenated, not sorted: this is the exact inverse of the nesting, so
				// it reproduces byte for byte what the flattened form carried before the
				// attributes were lifted out. Ordering the stored value is sanitisation's
				// job, downstream, as it always was.
				e.StructuredMetadata = AppendEffectiveMetadata(md, res.Attrs, scope.Attrs, &scope.Entries[k])
				out.Entries = append(out.Entries, e)
			}
		}
	}
	return out
}
