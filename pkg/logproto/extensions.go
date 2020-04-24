package logproto

import "github.com/prometheus/prometheus/pkg/labels"

// Note, this is not very efficient and use should be minimized as it requires label construction on each comparison
type SeriesIdentifiers []SeriesIdentifier

func (ids SeriesIdentifiers) Len() int      { return len(ids) }
func (ids SeriesIdentifiers) Swap(i, j int) { ids[i], ids[j] = ids[j], ids[i] }
func (ids SeriesIdentifiers) Less(i, j int) bool {
	a, b := labels.FromMap(ids[i].Labels), labels.FromMap(ids[j].Labels)
	return labels.Compare(a, b) <= 0
}

type Streams []Stream

func (xs Streams) Len() int           { return len(xs) }
func (xs Streams) Swap(i, j int)      { xs[i], xs[j] = xs[j], xs[i] }
func (xs Streams) Less(i, j int) bool { return xs[i].Labels <= xs[j].Labels }
