package metastore

import (
	"errors"
	"fmt"
	"math"

	"github.com/prometheus/prometheus/model/labels"
)

// indexLookupCacheVersion is the version of the cached entries. It is part of the key prefix and of
// each value, and a reader drops a value with another version.
//
// Change it when the layout of a value changes. Change it also when a change to the selector
// matching, the shard bucket cut, the time terms or the predicate admissions changes what an entry
// holds. Entries that the older code stored would keep their old content until they expire.
const indexLookupCacheVersion = 1

// errCachedEntryMismatch is the error for a cached value that is not the entry that a reader asked
// for, or that a reader cannot read.
var errCachedEntryMismatch = errors.New("the cached entry is not the one that was asked for")

// encodeSelectorMatches returns the value that stores the selector matches under key.
func encodeSelectorMatches(key string, matches *indexSelectorMatches) ([]byte, error) {
	var (
		paths      = newStringDictionary()
		labelNames = newStringDictionary()
		cached     = &CachedSelectorMatches{
			Version:  indexLookupCacheVersion,
			Key:      key,
			Sections: make([]*CachedSectionSelectorMatch, 0, len(matches.sections)),
		}
	)

	for i := range matches.sections {
		match := &matches.sections[i]

		section := &CachedSectionSelectorMatch{
			DataObjectPath:   paths.indexOf(match.dataObjectPath),
			LogsSectionIndex: match.logsSectionIndex,
			StreamIdDeltas:   encodeStreamIDDeltas(match.streamIDs),
		}
		for _, name := range match.matcherLabelNames {
			section.MatcherLabelNames = append(section.MatcherLabelNames, labelNames.indexOf(name))
		}
		for _, term := range match.timeTerms {
			cachedTerm := &CachedMatcherTimeTerm{
				MinUnixNanos:    term.minUnixNanos,
				MaxUnixNanos:    term.maxUnixNanos,
				KeepsAllStreams: term.keepsAllStreams,
			}
			if !term.keepsAllStreams {
				cachedTerm.KeptStreamIdDeltas = encodeStreamIDDeltas(term.keptStreamIDs)
			}
			section.TimeTerms = append(section.TimeTerms, cachedTerm)
		}
		cached.Sections = append(cached.Sections, section)
	}
	cached.DataObjectPaths = paths.values
	cached.MatcherLabelNames = labelNames.values

	return cached.Marshal()
}

// decodeSelectorMatches returns the selector matches that value stores under key. It returns an
// error when the value is not a value of that entry, or when it is corrupt.
func decodeSelectorMatches(key string, value []byte) (*indexSelectorMatches, error) {
	var cached CachedSelectorMatches
	if err := cached.Unmarshal(value); err != nil {
		return nil, fmt.Errorf("decoding the cached selector matches: %w", err)
	}
	if cached.Version != indexLookupCacheVersion || cached.Key != key {
		return nil, errCachedEntryMismatch
	}

	matches := &indexSelectorMatches{sections: make([]sectionSelectorMatch, 0, len(cached.Sections))}
	for _, section := range cached.Sections {
		path, err := dictionaryValue(cached.DataObjectPaths, section.DataObjectPath)
		if err != nil {
			return nil, err
		}
		streamIDs, err := decodeStreamIDDeltas(section.StreamIdDeltas)
		if err != nil {
			return nil, err
		}

		match := sectionSelectorMatch{dataObjectPath: path, logsSectionIndex: section.LogsSectionIndex, streamIDs: streamIDs}
		for _, nameIndex := range section.MatcherLabelNames {
			name, err := dictionaryValue(cached.MatcherLabelNames, nameIndex)
			if err != nil {
				return nil, err
			}
			match.matcherLabelNames = append(match.matcherLabelNames, name)
		}
		for _, term := range section.TimeTerms {
			decoded := matcherTimeTerm{minUnixNanos: term.MinUnixNanos, maxUnixNanos: term.MaxUnixNanos, keepsAllStreams: term.KeepsAllStreams}
			if !term.KeepsAllStreams {
				decoded.keptStreamIDs, err = decodeStreamIDDeltas(term.KeptStreamIdDeltas)
				if err != nil {
					return nil, err
				}
			}
			match.timeTerms = append(match.timeTerms, decoded)
		}
		matches.sections = append(matches.sections, match)
	}
	return matches, nil
}

// encodePredicateAdmission returns the value that stores the admission under key.
func encodePredicateAdmission(key string, admission *predicateAdmission) ([]byte, error) {
	paths := newStringDictionary()
	cached := &CachedPredicateAdmission{
		Version:             indexLookupCacheVersion,
		Key:                 key,
		BloomHitSections:    encodeSectionRefs(admission.bloomHitSections, paths),
		StreamLabelSections: encodeSectionRefs(admission.streamLabelSections, paths),
	}
	cached.DataObjectPaths = paths.values
	return cached.Marshal()
}

// decodePredicateAdmission returns the admission of predicate that value stores under key. It
// returns an error when the value is not a value of that entry, or when it is corrupt.
func decodePredicateAdmission(key string, value []byte, predicate *labels.Matcher) (*predicateAdmission, error) {
	var cached CachedPredicateAdmission
	if err := cached.Unmarshal(value); err != nil {
		return nil, fmt.Errorf("decoding the cached predicate admission: %w", err)
	}
	if cached.Version != indexLookupCacheVersion || cached.Key != key {
		return nil, errCachedEntryMismatch
	}

	bloomHitSections, err := decodeSectionRefs(cached.BloomHitSections, cached.DataObjectPaths)
	if err != nil {
		return nil, err
	}
	streamLabelSections, err := decodeSectionRefs(cached.StreamLabelSections, cached.DataObjectPaths)
	if err != nil {
		return nil, err
	}
	return &predicateAdmission{
		predicateName:       predicate.Name,
		predicateValue:      predicate.Value,
		bloomHitSections:    bloomHitSections,
		streamLabelSections: streamLabelSections,
	}, nil
}

func encodeSectionRefs(sections sectionKeySet, paths *stringDictionary) []*CachedSectionRef {
	refs := make([]*CachedSectionRef, 0, len(sections))
	for section := range sections {
		refs = append(refs, &CachedSectionRef{DataObjectPath: paths.indexOf(section.ObjectPath), LogsSectionIndex: section.SectionIdx})
	}
	return refs
}

func decodeSectionRefs(refs []*CachedSectionRef, paths []string) (sectionKeySet, error) {
	sections := make(sectionKeySet, len(refs))
	for _, ref := range refs {
		path, err := dictionaryValue(paths, ref.DataObjectPath)
		if err != nil {
			return nil, err
		}
		sections[SectionKey{ObjectPath: path, SectionIdx: ref.LogsSectionIndex}] = struct{}{}
	}
	return sections, nil
}

// encodeStreamIDDeltas returns the ascending stream IDs as differences to the previous ID. Small
// differences take one byte in a varint.
func encodeStreamIDDeltas(streamIDs []int64) []uint64 {
	deltas := make([]uint64, len(streamIDs))
	var previous int64
	for i, id := range streamIDs {
		deltas[i] = uint64(id - previous)
		previous = id
	}
	return deltas
}

// decodeStreamIDDeltas returns the stream IDs of the differences. It returns an error when the IDs
// overflow, which only a corrupt value does.
func decodeStreamIDDeltas(deltas []uint64) ([]int64, error) {
	if len(deltas) == 0 {
		return nil, nil
	}

	streamIDs := make([]int64, len(deltas))
	var previous int64
	for i, delta := range deltas {
		if delta > math.MaxInt64 || previous > math.MaxInt64-int64(delta) {
			return nil, errCachedEntryMismatch
		}
		previous += int64(delta)
		streamIDs[i] = previous
	}
	return streamIDs, nil
}

// stringDictionary assigns a position to each distinct string, in the order of first use.
type stringDictionary struct {
	values    []string
	positions map[string]uint32
}

func newStringDictionary() *stringDictionary {
	return &stringDictionary{positions: map[string]uint32{}}
}

func (d *stringDictionary) indexOf(value string) uint32 {
	position, ok := d.positions[value]
	if !ok {
		position = uint32(len(d.values))
		d.positions[value] = position
		d.values = append(d.values, value)
	}
	return position
}

func dictionaryValue(values []string, position uint32) (string, error) {
	if int64(position) >= int64(len(values)) {
		return "", errCachedEntryMismatch
	}
	return values[position], nil
}
