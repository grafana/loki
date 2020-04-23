// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storepb

import (
	"strings"
	"unsafe"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/store/storepb/prompb"
)

var PartialResponseStrategyValues = func() []string {
	var s []string
	for k := range PartialResponseStrategy_value {
		s = append(s, k)
	}
	return s
}()

func NewWarnSeriesResponse(err error) *SeriesResponse {
	return &SeriesResponse{
		Result: &SeriesResponse_Warning{
			Warning: err.Error(),
		},
	}
}

func NewSeriesResponse(series *Series) *SeriesResponse {
	return &SeriesResponse{
		Result: &SeriesResponse_Series{
			Series: series,
		},
	}
}

// CompareLabels compares two sets of labels.
func CompareLabels(a, b []Label) int {
	l := len(a)
	if len(b) < l {
		l = len(b)
	}
	for i := 0; i < l; i++ {
		if d := strings.Compare(a[i].Name, b[i].Name); d != 0 {
			return d
		}
		if d := strings.Compare(a[i].Value, b[i].Value); d != 0 {
			return d
		}
	}
	// If all labels so far were in common, the set with fewer labels comes first.
	return len(a) - len(b)
}

type emptySeriesSet struct{}

func (emptySeriesSet) Next() bool                 { return false }
func (emptySeriesSet) At() ([]Label, []AggrChunk) { return nil, nil }
func (emptySeriesSet) Err() error                 { return nil }

// EmptySeriesSet returns a new series set that contains no series.
func EmptySeriesSet() SeriesSet {
	return emptySeriesSet{}
}

// MergeSeriesSets returns a new series set that is the union of the input sets.
func MergeSeriesSets(all ...SeriesSet) SeriesSet {
	switch len(all) {
	case 0:
		return emptySeriesSet{}
	case 1:
		return all[0]
	}
	h := len(all) / 2

	return newMergedSeriesSet(
		MergeSeriesSets(all[:h]...),
		MergeSeriesSets(all[h:]...),
	)
}

// SeriesSet is a set of series and their corresponding chunks.
// The set is sorted by the label sets. Chunks may be overlapping or expected of order.
type SeriesSet interface {
	Next() bool
	At() ([]Label, []AggrChunk)
	Err() error
}

// mergedSeriesSet takes two series sets as a single series set.
type mergedSeriesSet struct {
	a, b SeriesSet

	lset         []Label
	chunks       []AggrChunk
	adone, bdone bool
}

// newMergedSeriesSet takes two series sets as a single series set.
// Series that occur in both sets should have disjoint time ranges.
// If the ranges overlap b samples are appended to a samples.
// If the single SeriesSet returns same series within many iterations,
// merge series set will not try to merge those.
func newMergedSeriesSet(a, b SeriesSet) *mergedSeriesSet {
	s := &mergedSeriesSet{a: a, b: b}
	// Initialize first elements of both sets as Next() needs
	// one element look-ahead.
	s.adone = !s.a.Next()
	s.bdone = !s.b.Next()

	return s
}

func (s *mergedSeriesSet) At() ([]Label, []AggrChunk) {
	return s.lset, s.chunks
}

func (s *mergedSeriesSet) Err() error {
	if s.a.Err() != nil {
		return s.a.Err()
	}
	return s.b.Err()
}

func (s *mergedSeriesSet) compare() int {
	if s.adone {
		return 1
	}
	if s.bdone {
		return -1
	}
	lsetA, _ := s.a.At()
	lsetB, _ := s.b.At()
	return CompareLabels(lsetA, lsetB)
}

func (s *mergedSeriesSet) Next() bool {
	if s.adone && s.bdone || s.Err() != nil {
		return false
	}

	d := s.compare()

	// Both sets contain the current series. Chain them into a single one.
	if d > 0 {
		s.lset, s.chunks = s.b.At()
		s.bdone = !s.b.Next()
	} else if d < 0 {
		s.lset, s.chunks = s.a.At()
		s.adone = !s.a.Next()
	} else {
		// Concatenate chunks from both series sets. They may be expected of order
		// w.r.t to their time range. This must be accounted for later.
		lset, chksA := s.a.At()
		_, chksB := s.b.At()

		s.lset = lset
		// Slice reuse is not generally safe with nested merge iterators.
		// We err on the safe side an create a new slice.
		s.chunks = make([]AggrChunk, 0, len(chksA)+len(chksB))
		s.chunks = append(s.chunks, chksA...)
		s.chunks = append(s.chunks, chksB...)

		s.adone = !s.a.Next()
		s.bdone = !s.b.Next()
	}
	return true
}

// LabelsToPromLabels converts Thanos proto labels to Prometheus labels in type safe manner.
func LabelsToPromLabels(lset []Label) labels.Labels {
	ret := make(labels.Labels, len(lset))
	for i, l := range lset {
		ret[i] = labels.Label{Name: l.Name, Value: l.Value}
	}
	return ret
}

// LabelsToPromLabelsUnsafe converts Thanos proto labels to Prometheus labels in type unsafe manner.
// It reuses the same memory. Caller should abort using passed []Labels.
//
// NOTE: This depends on order of struct fields etc, so use with extreme care.
func LabelsToPromLabelsUnsafe(lset []Label) labels.Labels {
	return *(*[]labels.Label)(unsafe.Pointer(&lset))
}

// PromLabelsToLabels converts Prometheus labels to Thanos proto labels in type safe manner.
func PromLabelsToLabels(lset labels.Labels) []Label {
	ret := make([]Label, len(lset))
	for i, l := range lset {
		ret[i] = Label{Name: l.Name, Value: l.Value}
	}
	return ret
}

// PromLabelsToLabelsUnsafe converts Prometheus labels to Thanos proto labels in type unsafe manner.
// It reuses the same memory. Caller should abort using passed labels.Labels.
//
// // NOTE: This depends on order of struct fields etc, so use with extreme care.
func PromLabelsToLabelsUnsafe(lset labels.Labels) []Label {
	return *(*[]Label)(unsafe.Pointer(&lset))
}

// PrompbLabelsToLabels converts Prometheus labels to Thanos proto labels in type safe manner.
func PrompbLabelsToLabels(lset []prompb.Label) []Label {
	ret := make([]Label, len(lset))
	for i, l := range lset {
		ret[i] = Label{Name: l.Name, Value: l.Value}
	}
	return ret
}

// PrompbLabelsToLabelsUnsafe converts Prometheus proto labels to Thanos proto labels in type unsafe manner.
// It reuses the same memory. Caller should abort using passed labels.Labels.
//
// // NOTE: This depends on order of struct fields etc, so use with extreme care.
func PrompbLabelsToLabelsUnsafe(lset []prompb.Label) []Label {
	return *(*[]Label)(unsafe.Pointer(&lset))
}

func LabelsToString(lset []Label) string {
	var s []string
	for _, l := range lset {
		s = append(s, l.String())
	}
	return "[" + strings.Join(s, ",") + "]"
}

func LabelSetsToString(lsets []LabelSet) string {
	s := []string{}
	for _, ls := range lsets {
		s = append(s, LabelsToString(ls.Labels))
	}
	return strings.Join(s, "")
}
