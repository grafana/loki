package metastore

// indexSelectorMatches are the sections of one index object whose streams match a stream selector.
// They do not depend on the time range or on the equality predicates. They depend on the index
// object, the tenant, the stream matchers and the shard bucket range they were cut for.
type indexSelectorMatches struct {
	sections []sectionSelectorMatch
}

// sectionSelectorMatch is the match of a stream selector in one logs section.
type sectionSelectorMatch struct {
	dataObjectPath   string
	logsSectionIndex int64

	// streamIDs are the streams that match every matcher, in ascending order.
	streamIDs []int64

	// matcherLabelNames are the label names of the value matchers. They also hold the names of the
	// filter matchers when the section has a row for at least one of them. The admission of an
	// equality predicate uses them to tell a stream label from structured metadata.
	matcherLabelNames []string

	timeTerms []matcherTimeTerm
}

// matcherTimeTerm is what the rows of one matcher in one postings section add to a section when
// the time range of the request overlaps the time range of those rows.
type matcherTimeTerm struct {
	minUnixNanos, maxUnixNanos int64

	// keepsAllStreams is true when the term keeps every stream of the section. keptStreamIDs is
	// empty then.
	keepsAllStreams bool

	// keptStreamIDs are the streams that a term keeps when keepsAllStreams is false, in ascending
	// order. They are a subset of the streams of the section, and can be empty.
	keptStreamIDs []int64
}

// sectionKey returns the key that identifies the section among the sections of all data objects.
func (m *sectionSelectorMatch) sectionKey() SectionKey {
	return SectionKey{ObjectPath: m.dataObjectPath, SectionIdx: m.logsSectionIndex}
}

// streamsInTimeWindow returns the streams of the section that the time window keeps, and the time
// range of the terms that kept them. ok is false when the window drops the section.
//
// The returned streams are shared with the section, and the caller must not modify them.
func (m *sectionSelectorMatch) streamsInTimeWindow(window requestTimeWindow) (streamIDs []int64, startUnixNanos, endUnixNanos int64, ok bool) {
	// A request without a time range prunes nothing and reports no time range.
	if window.isUnbounded() {
		return m.streamIDs, 0, 0, len(m.streamIDs) > 0
	}

	// A term counts when its time range overlaps the window. The section stays when at least one
	// term counts and the terms that count keep at least one stream. The streams of the section are
	// the streams that those terms keep.
	//
	// A term whose range does not overlap the window adds neither streams nor time range. So a
	// filter matcher whose rows lie in another part of the day cannot keep a section alive.
	var (
		allStreams bool
		keptUnion  []int64
	)
	for _, term := range m.timeTerms {
		if term.maxUnixNanos < window.startUnixNanos || term.minUnixNanos > window.endUnixNanos {
			continue
		}

		if !ok {
			startUnixNanos, endUnixNanos, ok = term.minUnixNanos, term.maxUnixNanos, true
		} else {
			startUnixNanos = min(startUnixNanos, term.minUnixNanos)
			endUnixNanos = max(endUnixNanos, term.maxUnixNanos)
		}

		switch {
		case term.keepsAllStreams:
			allStreams = true
		case !allStreams:
			keptUnion = mergeSortedStreamIDs(keptUnion, term.keptStreamIDs)
		}
	}
	if !ok {
		return nil, 0, 0, false
	}
	if allStreams {
		return m.streamIDs, startUnixNanos, endUnixNanos, len(m.streamIDs) > 0
	}
	return keptUnion, startUnixNanos, endUnixNanos, len(keptUnion) > 0
}

// mergeSortedStreamIDs returns the sorted union of two sorted lists of stream IDs. It may return
// one of its arguments.
func mergeSortedStreamIDs(a, b []int64) []int64 {
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	merged := make([]int64, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] < b[j]:
			merged = append(merged, a[i])
			i++
		case a[i] > b[j]:
			merged = append(merged, b[j])
			j++
		default:
			merged = append(merged, a[i])
			i++
			j++
		}
	}
	merged = append(merged, a[i:]...)
	return append(merged, b[j:]...)
}
