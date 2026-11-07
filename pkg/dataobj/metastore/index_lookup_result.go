package metastore

import (
	"slices"
	"time"
)

// indexLookupResult is what the lookup returns for one index object: everything that the
// evaluation of a request needs to know about it.
type indexLookupResult struct {
	index               IndexEntry
	selectorMatches     *indexSelectorMatches
	predicateAdmissions predicateAdmissions
}

// requestTimeWindow is the time range of a request, in Unix nanoseconds.
type requestTimeWindow struct {
	startUnixNanos, endUnixNanos int64
}

func newRequestTimeWindow(start, end time.Time) requestTimeWindow {
	return requestTimeWindow{startUnixNanos: start.UnixNano(), endUnixNanos: end.UnixNano()}
}

// isUnbounded reports whether the window prunes nothing, which is the case for a request with no
// time range.
func (w requestTimeWindow) isUnbounded() bool {
	return w.startUnixNanos <= 0 && w.endUnixNanos <= 0
}

// sectionsInTimeWindow returns the sections of the index object that pass the predicate
// admissions and the time window. streamRows is the number of streams in those sections.
func (r indexLookupResult) sectionsInTimeWindow(window requestTimeWindow) (sections []DataobjSectionDescriptorWithIndex, streamRows int) {
	for i := range r.selectorMatches.sections {
		match := &r.selectorMatches.sections[i]

		ambiguousNames, admitted := r.predicateAdmissions.admitSection(match)
		if !admitted {
			continue
		}
		streamIDs, startUnixNanos, endUnixNanos, ok := match.streamsInTimeWindow(window)
		if !ok {
			continue
		}

		sections = append(sections, newSectionDescriptorWithIndex(match, streamIDs, startUnixNanos, endUnixNanos, ambiguousNames, r.index.Path))
		streamRows += len(streamIDs)
	}
	return sections, streamRows
}

// newSectionDescriptorWithIndex returns the descriptor of a section for the given streams, time
// range and ambiguous names. The descriptor owns its slices.
func newSectionDescriptorWithIndex(match *sectionSelectorMatch, streamIDs []int64, startUnixNanos, endUnixNanos int64, ambiguousNames []string, indexPath string) DataobjSectionDescriptorWithIndex {
	return DataobjSectionDescriptorWithIndex{
		DataobjSectionDescriptor: &DataobjSectionDescriptor{
			SectionKey: match.sectionKey(),

			// The match is shared between the requests that wait for the same lookup, and the
			// descriptor belongs to one request. The caller of SectionsStream may modify the
			// descriptor after the sequence ends, so it needs its own copy.
			StreamIDs: slices.Clone(streamIDs),

			Start:               time.Unix(0, startUnixNanos),
			End:                 time.Unix(0, endUnixNanos),
			AmbiguousPredicates: ambiguousNames,
		},
		IndexPath: indexPath,
	}
}
