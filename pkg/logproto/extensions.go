package logproto

import "github.com/prometheus/prometheus/pkg/labels"

type SeriesIdentifiers []SeriesIdentifier

func (ids SeriesIdentifiers) Len() int      { return len(ids) }
func (ids SeriesIdentifiers) Swap(i, j int) { ids[i], ids[j] = ids[j], ids[i] }
func (ids SeriesIdentifiers) Less(i, j int) bool {
	a, b := labels.FromMap(ids[i].Labels), labels.FromMap(ids[j].Labels)
	return labels.Compare(a, b) <= 0
}
