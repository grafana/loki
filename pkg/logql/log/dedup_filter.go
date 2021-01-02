package log

import (
	"github.com/prometheus/prometheus/pkg/labels"
)

type LineDedupFilter struct {
	groupBy              map[string]interface{}
	baseLabelsFilterList *labels.Labels

	inverted bool
	seen     map[uint64]uint64
}

func NewLineDedupFilter(groups []string, inverted bool) *LineDedupFilter {
	// create a map of groups for O(1) lookups instead of O(n)
	var groupMap = make(map[string]interface{})
	for _, group := range groups {
		groupMap[group] = nil
	}

	return &LineDedupFilter{
		groupBy:  groupMap,
		inverted: inverted,
		seen:     make(map[uint64]uint64),
	}
}

func (l *LineDedupFilter) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	var filterLabels labels.Labels

	// filter base labels
	if baseLabelsFilter := l.computeBaseLabelFilters(lbs); baseLabelsFilter != nil {
		filterLabels = append(filterLabels, *baseLabelsFilter...)
	}

	// filter additional labels
	if additionalLabelsFilter := l.computeAdditionalLabelFilters(lbs); additionalLabelsFilter != nil {
		filterLabels = append(filterLabels, *additionalLabelsFilter...)
	}

	// if no filters can be applied, return the line
	if len(filterLabels) == 0 {
		return line, true
	}

	hash := filterLabels.Hash()
	if _, beenSeen := l.seen[hash]; beenSeen {
		return nil, false
	}

	l.seen[hash]++
	return line, true
}

// computeBaseLabelFilters assumes that the base labels for a target are always the same - so
// we can precompute the desired filters on these labels once and reuse for each log entry
func (l *LineDedupFilter) computeBaseLabelFilters(lbs *LabelsBuilder) *labels.Labels {

	// return precomputed value if present
	if l.baseLabelsFilterList != nil {
		return l.baseLabelsFilterList
	}

	l.baseLabelsFilterList = l.buildFilterList(lbs.base)
	return l.baseLabelsFilterList
}

func (l *LineDedupFilter) computeAdditionalLabelFilters(lbs *LabelsBuilder) *labels.Labels {
	return l.buildFilterList(lbs.add)
}

func (l *LineDedupFilter) buildFilterList(labelList labels.Labels) *labels.Labels {
	var filter labels.Labels

	for _, label := range labelList {
		if _, found := l.groupBy[label.Name]; includeLabel(found, l.inverted) {
			filter = append(filter, label)
		}
	}

	return &filter
}

func includeLabel(found, inverted bool) bool {
	// exclude a label if it has been found but search is inverted, or
	// exclude a label if it has not been found and search is not inverted
	if (found && inverted) || (!found && !inverted) {
		return false
	}

	return true
}