package log

import (
	"github.com/prometheus/prometheus/pkg/labels"
)

type LineDedupFilter struct {
	labels     map[string]interface{}
	inverted   bool
	hashLookup map[uint64]struct{}
}

func NewLineDedupFilter(labelFilters []string, inverted bool) *LineDedupFilter {
	// create a map of labelFilters for O(1) lookups instead of O(n)
	var filterMap = make(map[string]interface{})
	for _, group := range labelFilters {
		filterMap[group] = nil
	}

	return &LineDedupFilter{
		labels:     filterMap,
		inverted:   inverted,
		hashLookup: make(map[uint64]struct{}),
	}
}

func (l *LineDedupFilter) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	var filterLabels labels.Labels

	for _, label := range append(lbs.base, lbs.add...) {
		if _, found := l.labels[label.Name]; includeLabel(found, l.inverted) {
			filterLabels = append(filterLabels, label)
		}
	}

	// if no filters can be applied, return the line
	if len(filterLabels) == 0 {
		return line, true
	}

	hash := filterLabels.Hash()
	if _, found := l.hashLookup[hash]; found {
		// don't return the line if the same labels have been seen already (i.e. this is a duplicate)
		return nil, false
	}

	l.hashLookup[hash] = struct{}{}
	return line, true
}

func includeLabel(found, inverted bool) bool {
	// exclude a label if it has been found but search is inverted, or
	// exclude a label if it has not been found and search is not inverted
	if (found && inverted) || (!found && !inverted) {
		return false
	}

	return true
}
