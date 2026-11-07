package metastore

import (
	"slices"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// predicateAdmission is the answer of one index object to one equality predicate. It does not
// depend on the stream selector, on the shard, or on the time range.
type predicateAdmission struct {
	predicateName, predicateValue string

	// bloomHitSections are the sections whose bloom filter for the name may hold the value.
	bloomHitSections sectionKeySet

	// streamLabelSections are the sections where the name is a stream label.
	streamLabelSections sectionKeySet
}

// predicateAdmissions has one admission for each equality predicate of a request. It is empty
// when the request has no equality predicate.
type predicateAdmissions []*predicateAdmission

// sectionKeySet is a set of logs sections.
type sectionKeySet map[SectionKey]struct{}

func (s sectionKeySet) has(match *sectionSelectorMatch) bool {
	_, ok := s[match.sectionKey()]
	return ok
}

// admitSection reports whether the section can hold a row for every predicate. ambiguousNames are
// the names of the predicates that are also stream labels in the section.
func (a predicateAdmissions) admitSection(match *sectionSelectorMatch) (ambiguousNames []string, admitted bool) {
	for _, admission := range a {
		// A predicate on a stream label of the section is always admitted: the bloom filters hold
		// structured metadata only, so they cannot judge a stream label. Any other predicate
		// needs a hit of its bloom filter.
		nameIsStreamLabel := slices.Contains(match.matcherLabelNames, admission.predicateName) || admission.streamLabelSections.has(match)
		switch {
		case nameIsStreamLabel:
			ambiguousNames = append(ambiguousNames, admission.predicateName)
		case !admission.bloomHitSections.has(match):
			return nil, false
		}
	}
	return ambiguousNames, true
}

// newPredicateAdmissions returns the admissions of predicates from the hits that the postings
// scanners report. bloomHits and streamLabelHits hold one map per postings section.
func newPredicateAdmissions(predicates []*labels.Matcher, bloomHits []map[postings.SectionRef]map[postings.PredicateValue]struct{}, streamLabelHits []map[postings.SectionRef]map[string]struct{}) predicateAdmissions {
	admissions := make(predicateAdmissions, 0, len(predicates))
	for _, predicate := range predicates {
		admission := &predicateAdmission{
			predicateName:       predicate.Name,
			predicateValue:      predicate.Value,
			bloomHitSections:    sectionKeySet{},
			streamLabelSections: sectionKeySet{},
		}

		predicateValue := postings.PredicateValue{Name: predicate.Name, Value: predicate.Value}
		for _, hits := range bloomHits {
			for ref, values := range hits {
				if _, ok := values[predicateValue]; ok {
					admission.bloomHitSections[SectionKey{ObjectPath: ref.ObjectPath, SectionIdx: ref.SectionIndex}] = struct{}{}
				}
			}
		}
		for _, hits := range streamLabelHits {
			for ref, names := range hits {
				if _, ok := names[predicate.Name]; ok {
					admission.streamLabelSections[SectionKey{ObjectPath: ref.ObjectPath, SectionIdx: ref.SectionIndex}] = struct{}{}
				}
			}
		}
		admissions = append(admissions, admission)
	}
	return admissions
}
