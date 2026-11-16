package metastore

import (
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// selectorRowScan holds the postings rows that the scanner attributed to the matchers of a stream
// selector in one index object. It does not depend on the time range of a request.
type selectorRowScan struct {
	// bucketRange is the shard bucket range that the scan read. Nil means that the scan read the rows
	// of every bucket. The scan holds only the rows whose bucket span overlaps the range, so a cut by
	// a range that is not inside it misses rows.
	bucketRange *postings.ShardBucketRange

	// bucketRangesChecked is true when the scan checked that every postings section records the
	// shard bucket range of its rows. The rows of a section that does not record it all read as
	// bucket 0, so a cut by bucket is wrong for them.
	bucketRangesChecked bool

	// valueMatcherNames and filterMatcherNames are the label names of the matchers, by matcher
	// index. A filter matcher also matches the streams that lack its label.
	valueMatcherNames  []string
	filterMatcherNames []string

	valueMatcherRows  []matchedPostingsRow
	filterMatcherRows []matchedPostingsRow
}

// matchedPostingsRow is a label row that the scanner attributed to a matcher, and the postings
// section that held it. A row of a value matcher always matches the matcher. A row of a filter
// matcher has the label name of the matcher, and its value can fail the matcher.
type matchedPostingsRow struct {
	postingsSectionIndex int
	postings.LabelRow
}

// hasSectionWithRowsOfEveryValueMatcher reports whether some logs section has a row for each value
// matcher. Without such a section the rows of the scan match nothing, for any range cut from them.
func (rs *selectorRowScan) hasSectionWithRowsOfEveryValueMatcher() bool {
	matchersBySection := map[postings.SectionRef]map[int]struct{}{}
	for _, row := range rs.valueMatcherRows {
		matchers, ok := matchersBySection[row.Section]
		if !ok {
			matchers = map[int]struct{}{}
			matchersBySection[row.Section] = matchers
		}
		matchers[row.Matcher] = struct{}{}
		if len(matchers) == len(rs.valueMatcherNames) {
			return true
		}
	}
	return false
}

// selectorMatches returns the sections whose streams match the selector, from the rows that the scan
// read.
//
// A section can list streams outside the shard bucket range of the scan. This method does not drop
// them.
func (rs *selectorRowScan) selectorMatches() *indexSelectorMatches {
	return rs.selectorMatchesForBucketRange(rs.bucketRange)
}

// selectorMatchesForBucketRange returns the sections whose streams match the selector, using only
// the rows that overlap the shard bucket range. A nil range uses every row. The range must lie
// inside the range of the scan.
func (rs *selectorRowScan) selectorMatchesForBucketRange(bucketRange *postings.ShardBucketRange) *indexSelectorMatches {
	matches := &indexSelectorMatches{}
	if len(rs.valueMatcherNames) == 0 {
		return matches
	}

	accumulators := map[postings.SectionRef]*sectionAccumulator{}

	// Value matchers. The streams of a section are the streams that every matcher matches. The
	// time range of each group of rows becomes a time term of the section.
	for _, group := range groupRows(rs.valueMatcherRows, bucketRange, false) {
		acc, ok := accumulators[group.section]
		if !ok {
			acc = newSectionAccumulator(rs.valueMatcherNames)
			accumulators[group.section] = acc
		}
		acc.timeTerms = append(acc.timeTerms, rawMatcherTimeTerm{group.minUnixNanos, group.maxUnixNanos, group.streamsOfMatchingRows})
		acc.matchedByMatcher[group.matcher].union(group.streamsOfMatchingRows)
	}
	for section, acc := range accumulators {
		acc.matchingStreams = acc.matchedByMatcher[0]
		for _, matched := range acc.matchedByMatcher[1:] {
			acc.matchingStreams = intersection(acc.matchingStreams, matched)
		}
		if acc.matchingStreams.count() == 0 {
			delete(accumulators, section)
		}
	}

	// Filter matchers. They only narrow the sections that the value matchers found. A stream that
	// lacks the label of a filter matcher passes it, because a filter such as env!="dev" also
	// matches the absence of the label.
	if len(rs.filterMatcherNames) > 0 {
		for _, group := range groupRows(rs.filterMatcherRows, bucketRange, true) {
			acc, ok := accumulators[group.section]
			if !ok {
				continue
			}
			if acc.matchedByFilter == nil {
				acc.addFilterMatchers(rs.filterMatcherNames)
			}
			acc.timeTerms = append(acc.timeTerms, rawMatcherTimeTerm{group.minUnixNanos, group.maxUnixNanos, group.streamsOfAllRows})
			acc.presentByFilter[group.matcher].union(group.streamsOfAllRows)
			acc.matchedByFilter[group.matcher].union(group.streamsOfMatchingRows)
		}
		for i := range rs.filterMatcherNames {
			for section, acc := range accumulators {
				if acc.matchedByFilter != nil {
					lacksLabel := difference(acc.matchingStreams, acc.presentByFilter[i])
					acc.matchedByFilter[i].union(lacksLabel)
					acc.matchingStreams = intersection(acc.matchingStreams, acc.matchedByFilter[i])
				}
				if acc.matchingStreams.count() == 0 {
					delete(accumulators, section)
				}
			}
		}
	}

	for section, acc := range accumulators {
		match := sectionSelectorMatch{
			dataObjectPath:    section.ObjectPath,
			logsSectionIndex:  section.SectionIndex,
			streamIDs:         acc.matchingStreams.ids(),
			matcherLabelNames: acc.sortedLabelNames(),
		}

		// A term keeps the streams that it shares with the section. Most terms keep all of them,
		// and then the term stores no stream IDs.
		streamCount := acc.matchingStreams.count()
		for _, raw := range acc.timeTerms {
			kept := intersection(raw.rowStreams, acc.matchingStreams)
			term := matcherTimeTerm{minUnixNanos: raw.minUnixNanos, maxUnixNanos: raw.maxUnixNanos}
			if kept.count() == streamCount {
				term.keepsAllStreams = true
			} else {
				term.keepsAllStreams = false
				term.keptStreamIDs = kept.ids()
			}
			match.timeTerms = append(match.timeTerms, term)
		}
		matches.sections = append(matches.sections, match)
	}

	sort.Slice(matches.sections, func(i, j int) bool {
		a, b := matches.sections[i], matches.sections[j]
		if a.dataObjectPath != b.dataObjectPath {
			return a.dataObjectPath < b.dataObjectPath
		}
		return a.logsSectionIndex < b.logsSectionIndex
	})
	return matches
}

// selectorMatchesForEachBucket returns the selector matches for each single bucket of a layout with
// the given number of buckets, by bucket. It fails unless the scan read every bucket and checked
// that its sections record the shard bucket range of their rows.
func (rs *selectorRowScan) selectorMatchesForEachBucket(buckets uint32) ([]*indexSelectorMatches, error) {
	if rs.bucketRange != nil {
		return nil, fmt.Errorf("the scan read only the shard buckets %d to %d", rs.bucketRange.From, rs.bucketRange.To)
	}
	if !rs.bucketRangesChecked {
		return nil, errors.New("the scan did not check that its sections record the shard bucket range of their rows")
	}

	matches := make([]*indexSelectorMatches, buckets)
	for bucket := range buckets {
		matches[bucket] = rs.selectorMatchesForBucketRange(&postings.ShardBucketRange{From: bucket, To: bucket})
	}
	return matches, nil
}

// rawMatcherTimeTerm is a time term before it is cut to the streams of the section.
type rawMatcherTimeTerm struct {
	minUnixNanos, maxUnixNanos int64
	rowStreams                 streamBitset
}

// sectionAccumulator collects what the rows of one section say about the streams of the selector.
type sectionAccumulator struct {
	// matchingStreams are the streams that match every matcher evaluated so far.
	matchingStreams streamBitset

	labelNames map[string]struct{}
	timeTerms  []rawMatcherTimeTerm

	matchedByMatcher []streamBitset // by value matcher
	presentByFilter  []streamBitset // by filter matcher: streams that have the label
	matchedByFilter  []streamBitset // by filter matcher: streams whose value matches
}

func newSectionAccumulator(valueMatcherNames []string) *sectionAccumulator {
	acc := &sectionAccumulator{
		labelNames:       map[string]struct{}{},
		matchedByMatcher: make([]streamBitset, len(valueMatcherNames)),
	}
	for _, name := range valueMatcherNames {
		acc.labelNames[name] = struct{}{}
	}
	return acc
}

// addFilterMatchers records that the section has a row for a filter matcher. The section then
// reports the label names of every filter matcher.
func (acc *sectionAccumulator) addFilterMatchers(filterMatcherNames []string) {
	acc.presentByFilter = make([]streamBitset, len(filterMatcherNames))
	acc.matchedByFilter = make([]streamBitset, len(filterMatcherNames))

	// The admission of a predicate uses the label names of the section to tell a stream label from
	// structured metadata.
	for _, name := range filterMatcherNames {
		acc.labelNames[name] = struct{}{}
	}
}

func (acc *sectionAccumulator) sortedLabelNames() []string {
	names := make([]string, 0, len(acc.labelNames))
	for name := range acc.labelNames {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// rowGroup is the union of the rows of one matcher in one logs section that one postings section
// holds.
type rowGroup struct {
	section postings.SectionRef
	matcher int

	// streamsOfAllRows are the streams of all the rows of the group. It is only set for filter
	// matchers.
	streamsOfAllRows streamBitset

	// streamsOfMatchingRows are the streams of the rows whose label value matches the matcher.
	streamsOfMatchingRows streamBitset

	minUnixNanos, maxUnixNanos int64
}

// groupRows groups the rows that overlap the shard bucket range, in the order of their first row. A
// nil range keeps every row. With trackPresent the groups also keep the streams of the rows whose
// value does not match.
func groupRows(rows []matchedPostingsRow, bucketRange *postings.ShardBucketRange, trackPresent bool) []*rowGroup {
	// The time terms of a section follow the postings section that held the rows, so the group
	// key has the postings section as well as the logs section.
	type groupKey struct {
		postingsSectionIndex int
		section              postings.SectionRef
		matcher              int
	}

	var (
		groups []*rowGroup
		byKey  = map[groupKey]*rowGroup{}
	)
	for i := range rows {
		row := &rows[i]
		if bucketRange != nil && (row.MaxShardBucket < bucketRange.From || row.MinShardBucket > bucketRange.To) {
			continue
		}

		key := groupKey{row.postingsSectionIndex, row.Section, row.Matcher}
		group, ok := byKey[key]
		if !ok {
			group = &rowGroup{section: row.Section, matcher: row.Matcher, minUnixNanos: row.MinNS, maxUnixNanos: row.MaxNS}
			byKey[key] = group
			groups = append(groups, group)
		}

		group.minUnixNanos = min(group.minUnixNanos, row.MinNS)
		group.maxUnixNanos = max(group.maxUnixNanos, row.MaxNS)
		if trackPresent {
			group.streamsOfAllRows.union(row.StreamIDBitmap)
		}
		if row.Matches {
			group.streamsOfMatchingRows.union(row.StreamIDBitmap)
		}
	}
	return groups
}
