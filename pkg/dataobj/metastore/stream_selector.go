package metastore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// SectionStreams holds the matching streams within one logs section
type SectionStreams struct {
	Section        postings.SectionRef
	StreamBitmap   []byte // bit i set => stream i matches
	MinTimestamp   int64  // unix nanos; 0 means unknown
	MaxTimestamp   int64  // unix nanos; 0 means unknown
	AmbiguousNames []string
}

// streamSelector reads the rows of a LogQL stream selector, and the answers to the equality
// predicates, from one or more postings sections via postings.Scanner.
type streamSelector struct {
	matchers []*labels.Matcher
	// equalPredicates holds the equality predicates of the request.
	equalPredicates []*labels.Matcher
	start, end      time.Time

	// bucketRange prunes the label rows to read. A nil range does not filter by bucket range.
	bucketRange *postings.ShardBucketRange

	compiledMatchers []postings.CompiledMatcher
	compiledFilters  []postings.CompiledMatcher
	scanners         []*postings.Scanner
	stats            *statsTracker
	opened           bool
}

func newStreamSelector(matchers, predicates []*labels.Matcher, start, end time.Time, bucketRange *postings.ShardBucketRange) *streamSelector {
	// Only an equality can prune a section, because a bloom filter proves that a value is absent
	// and nothing else.
	var equal []*labels.Matcher
	for _, p := range predicates {
		if p != nil && p.Type == labels.MatchEqual {
			equal = append(equal, p)
		}
	}
	return &streamSelector{matchers: matchers, equalPredicates: equal, start: start, end: end, bucketRange: bucketRange}
}

func (s *streamSelector) open(ctx context.Context, sections []*postings.Section, maxConcurrency int) error {
	if s.opened {
		return nil
	}
	if len(s.matchers) == 0 && len(s.equalPredicates) == 0 {
		s.opened = true
		return nil
	}

	if len(s.matchers) > 0 {
		filters, matchers := s.splitFiltersAndMatchers()
		if len(matchers) == 0 {
			return fmt.Errorf("stream selector requires at least one non-filter matcher")
		}

		var err error
		s.compiledMatchers, err = compileAll(matchers)
		if err != nil {
			return err
		}
		s.compiledFilters, err = compileAll(filters)
		if err != nil {
			return err
		}
	}

	s.stats = newStatsTracker(len(sections))
	s.scanners = make([]*postings.Scanner, len(sections))

	labelStats := make([]*sectionStatsTracker, len(sections))
	bloomStats := make([]*sectionStatsTracker, len(sections))
	for i := range sections {
		labelStats[i] = s.stats.SectionLabelTracker(i)
		bloomStats[i] = s.stats.SectionBloomTracker(i)
	}

	g, groupCtx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency) // Each ScannerReader opens 4 readers, so share the concurrent limit
	for i, section := range sections {
		goRecovering(groupCtx, g, func() error {
			readers, err := postings.NewScannerReaders(
				section,
				s.compiledMatchers,
				s.compiledFilters,
				s.equalPredicates,
				labelStats[i],
				bloomStats[i],
				s.bucketRange,
			)
			if err != nil {
				return fmt.Errorf("creating postings scanner readers: %w", err)
			}
			if err := readers.Open(groupCtx); err != nil {
				return fmt.Errorf("opening postings scanner readers: %w", err)
			}
			s.scanners[i] = postings.NewScanner(section, readers)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		_ = s.close()
		return err
	}

	s.opened = true
	return nil
}

func (s *streamSelector) close() error {
	var errs []error
	for _, scanner := range s.scanners {
		if scanner != nil {
			errs = append(errs, scanner.Close())
		}
	}
	s.scanners = nil
	s.opened = false
	return errors.Join(errs...)
}

// reportStats records the postings page statistics of the reads so far in the xcap region of ctx.
func (s *streamSelector) reportStats(ctx context.Context) {
	if s.stats != nil {
		s.stats.Report(ctx)
	}
}

// scanRows reads the postings rows of the matchers of the selector. It does not depend on the time
// range or on the equality predicates.
func (s *streamSelector) scanRows(ctx context.Context) (*selectorRowScan, error) {
	if !s.opened {
		return nil, fmt.Errorf("stream selector not opened")
	}

	rowScan := &selectorRowScan{
		bucketRange:        s.bucketRange,
		valueMatcherNames:  matcherNames(s.compiledMatchers),
		filterMatcherNames: matcherNames(s.compiledFilters),
	}
	if len(s.compiledMatchers) == 0 {
		return rowScan, nil
	}

	for i, scanner := range s.scanners {
		rows, err := scanner.MatchLabelRows(ctx, s.compiledMatchers)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			rowScan.valueMatcherRows = append(rowScan.valueMatcherRows, matchedPostingsRow{postingsSectionIndex: i, LabelRow: row})
		}
	}

	// A filter matcher only narrows the sections that the value matchers found, so its rows are of
	// no use when no section has rows for every value matcher.
	if len(s.compiledFilters) == 0 || !rowScan.hasSectionWithRowsOfEveryValueMatcher() {
		return rowScan, nil
	}
	for i, scanner := range s.scanners {
		rows, err := scanner.LabelNameRows(ctx, s.compiledFilters)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			rowScan.filterMatcherRows = append(rowScan.filterMatcherRows, matchedPostingsRow{postingsSectionIndex: i, LabelRow: row})
		}
	}
	return rowScan, nil
}

// scanPredicateAdmissions reads the answer of the postings sections to each equality predicate of
// the selector. It does not depend on the matchers, on the shard or on the time range.
func (s *streamSelector) scanPredicateAdmissions(ctx context.Context) (predicateAdmissions, error) {
	if !s.opened {
		return nil, fmt.Errorf("stream selector not opened")
	}
	if len(s.equalPredicates) == 0 {
		return nil, nil
	}

	bloomHits := make([]map[postings.SectionRef]map[postings.PredicateValue]struct{}, len(s.scanners))
	streamLabelHits := make([]map[postings.SectionRef]map[string]struct{}, len(s.scanners))
	g, groupCtx := errgroup.WithContext(ctx)
	for i := range s.scanners {
		goRecovering(groupCtx, g, func() error {
			bloom, streamLabels, err := s.scanners[i].MatcherHits(groupCtx, s.equalPredicates)
			if err != nil {
				return err
			}
			bloomHits[i] = bloom
			streamLabelHits[i] = streamLabels
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return newPredicateAdmissions(s.equalPredicates, bloomHits, streamLabelHits), nil
}

// selectStreams returns the streams of each logs section that match the selector, the equality
// predicates and the time range of the selector. The caller owns opening and closing the selector.
func (s *streamSelector) selectStreams(ctx context.Context) ([]SectionStreams, error) {
	if !s.opened {
		return nil, fmt.Errorf("stream selector not opened")
	}
	if len(s.matchers) == 0 {
		return nil, nil
	}
	defer s.reportStats(ctx)

	rowScan, err := s.scanRows(ctx)
	if err != nil {
		return nil, err
	}
	admissions, err := s.scanPredicateAdmissions(ctx)
	if err != nil {
		return nil, err
	}

	result := indexLookupResult{
		selectorMatches:     rowScan.selectorMatches(),
		predicateAdmissions: admissions,
	}
	sections, _ := result.sectionsInTimeWindow(newRequestTimeWindow(s.start, s.end))

	streams := make([]SectionStreams, 0, len(sections))
	for _, section := range sections {
		streams = append(streams, SectionStreams{
			Section:        postings.SectionRef{ObjectPath: section.ObjectPath, SectionIndex: section.SectionIdx},
			StreamBitmap:   streamBitsetOf(section.StreamIDs),
			MinTimestamp:   section.Start.UnixNano(),
			MaxTimestamp:   section.End.UnixNano(),
			AmbiguousNames: section.AmbiguousPredicates,
		})
	}
	return streams, nil
}

func matcherNames(cms []postings.CompiledMatcher) []string {
	names := make([]string, len(cms))
	for i, cm := range cms {
		names[i] = cm.Matcher().Name
	}
	return names
}

// splitFiltersAndMatchers splits matchers into filters (empty-matching, also
// select streams lacking the label name) and matchers (value-selecting, seed the
// result). Mirrors LogQL's syntax.SplitFiltersAndMatchers, treating a `.*` regex as
// a value-selecting matcher.
func (s *streamSelector) splitFiltersAndMatchers() (filters, matchers []*labels.Matcher) {
	for _, m := range s.matchers {
		if m == nil {
			continue
		}
		if isFilter(m) {
			filters = append(filters, m)
		} else {
			matchers = append(matchers, m)
		}
	}
	return filters, matchers
}

// isFilter reports whether a matcher is treated as a filter: one that also
// matches streams lacking its label name. Mirrors syntax.SplitFiltersAndMatchers: a
// matcher that matches "" is a filter, except a `.*` regex which selects every
// stream and is a value-selecting matcher.
func isFilter(m *labels.Matcher) bool {
	if m.Type == labels.MatchRegexp && m.Value == ".*" {
		return false
	}
	return m.Matches("")
}

// compileAll compiles every matcher once for reuse across sections.
func compileAll(matchers []*labels.Matcher) ([]postings.CompiledMatcher, error) {
	out := make([]postings.CompiledMatcher, 0, len(matchers))
	for _, m := range matchers {
		cm, err := postings.CompileMatcher(m)
		if err != nil {
			return nil, err
		}
		out = append(out, cm)
	}
	return out, nil
}

type statsTracker struct {
	bySectionLabelReads map[int]*sectionStatsTracker
	bySectionBloomReads map[int]*sectionStatsTracker
}

func newStatsTracker(sectionsCount int) *statsTracker {
	return &statsTracker{
		bySectionLabelReads: make(map[int]*sectionStatsTracker, sectionsCount),
		bySectionBloomReads: make(map[int]*sectionStatsTracker, sectionsCount),
	}
}

func (s *statsTracker) SectionLabelTracker(sID int) *sectionStatsTracker {
	st, ok := s.bySectionLabelReads[sID]
	if !ok {
		st = &sectionStatsTracker{}
		s.bySectionLabelReads[sID] = st
	}
	return st
}

func (s *statsTracker) SectionBloomTracker(sID int) *sectionStatsTracker {
	st, ok := s.bySectionBloomReads[sID]
	if !ok {
		st = &sectionStatsTracker{}
		s.bySectionBloomReads[sID] = st
	}
	return st
}

func (s *statsTracker) Report(ctx context.Context) {
	region := xcap.RegionFromContext(ctx)

	// "column_name" column stats
	var (
		labelColumnNameTotalPages    = uint64(0)
		labelColumnNameRelevantPages = uint64(0)
		bloomColumnNameTotalPages    = uint64(0)
		bloomColumnNameRelevantPages = uint64(0)
	)

	for _, st := range s.bySectionLabelReads {
		labelColumnNameTotalPages += st.columnNamePages.Total
		labelColumnNameRelevantPages += st.columnNamePages.Relevant
	}

	for _, st := range s.bySectionBloomReads {
		bloomColumnNameTotalPages += st.columnNamePages.Total
		bloomColumnNameRelevantPages += st.columnNamePages.Relevant
	}

	region.Record(StatPostingsLabelColumnNameTotalPages.Observe(int64(labelColumnNameTotalPages)))
	region.Record(StatPostingsLabelColumnNameRelevantPages.Observe(int64(labelColumnNameRelevantPages)))
	region.Record(StatPostingsBloomColumnNameTotalPages.Observe(int64(bloomColumnNameTotalPages)))
	region.Record(StatPostingsBloomColumnNameRelevantPages.Observe(int64(bloomColumnNameRelevantPages)))
}

type sectionStatsTracker struct {
	columnNamePages dataset.ColumnReadPageStats
}

func (st *sectionStatsTracker) OnColumnPredicateBuilt(c dataset.Column, pages dataset.ColumnReadPageStats) {
	if c.ColumnDesc().Type.Logical != postings.ColumnTypeColumnName.String() {
		return
	}
	st.columnNamePages.Total = pages.Total
	st.columnNamePages.Relevant += pages.Relevant
}
