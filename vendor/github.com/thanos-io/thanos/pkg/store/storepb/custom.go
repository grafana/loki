// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storepb

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/store/labelpb"
)

var PartialResponseStrategyValues = func() []string {
	var s []string
	for k := range PartialResponseStrategy_value {
		s = append(s, k)
	}
	sort.Strings(s)
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

func NewHintsSeriesResponse(hints *types.Any) *SeriesResponse {
	return &SeriesResponse{
		Result: &SeriesResponse_Hints{
			Hints: hints,
		},
	}
}

type emptySeriesSet struct{}

func (emptySeriesSet) Next() bool                       { return false }
func (emptySeriesSet) At() (labels.Labels, []AggrChunk) { return nil, nil }
func (emptySeriesSet) Err() error                       { return nil }

// EmptySeriesSet returns a new series set that contains no series.
func EmptySeriesSet() SeriesSet {
	return emptySeriesSet{}
}

// MergeSeriesSets takes all series sets and returns as a union single series set.
// It assumes series are sorted by labels within single SeriesSet, similar to remote read guarantees.
// However, they can be partial: in such case, if the single SeriesSet returns the same series within many iterations,
// MergeSeriesSets will merge those into one.
//
// It also assumes in a "best effort" way that chunks are sorted by min time. It's done as an optimization only, so if input
// series' chunks are NOT sorted, the only consequence is that the duplicates might be not correctly removed. This is double checked
// which on just-before PromQL level as well, so the only consequence is increased network bandwidth.
// If all chunks were sorted, MergeSeriesSet ALSO returns sorted chunks by min time.
//
// Chunks within the same series can also overlap (within all SeriesSet
// as well as single SeriesSet alone). If the chunk ranges overlap, the *exact* chunk duplicates will be removed
// (except one), and any other overlaps will be appended into on chunks slice.
func MergeSeriesSets(all ...SeriesSet) SeriesSet {
	switch len(all) {
	case 0:
		return emptySeriesSet{}
	case 1:
		return newUniqueSeriesSet(all[0])
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
	At() (labels.Labels, []AggrChunk)
	Err() error
}

// mergedSeriesSet takes two series sets as a single series set.
type mergedSeriesSet struct {
	a, b SeriesSet

	lset         labels.Labels
	chunks       []AggrChunk
	adone, bdone bool
}

func newMergedSeriesSet(a, b SeriesSet) *mergedSeriesSet {
	s := &mergedSeriesSet{a: a, b: b}
	// Initialize first elements of both sets as Next() needs
	// one element look-ahead.
	s.adone = !s.a.Next()
	s.bdone = !s.b.Next()

	return s
}

func (s *mergedSeriesSet) At() (labels.Labels, []AggrChunk) {
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
	return labels.Compare(lsetA, lsetB)
}

func (s *mergedSeriesSet) Next() bool {
	if s.adone && s.bdone || s.Err() != nil {
		return false
	}

	d := s.compare()
	if d > 0 {
		s.lset, s.chunks = s.b.At()
		s.bdone = !s.b.Next()
		return true
	}
	if d < 0 {
		s.lset, s.chunks = s.a.At()
		s.adone = !s.a.Next()
		return true
	}

	// Both a and b contains the same series. Go through all chunks, remove duplicates and concatenate chunks from both
	// series sets. We best effortly assume chunks are sorted by min time. If not, we will not detect all deduplicate which will
	// be account on select layer anyway. We do it still for early optimization.
	lset, chksA := s.a.At()
	_, chksB := s.b.At()
	s.lset = lset

	// Slice reuse is not generally safe with nested merge iterators.
	// We err on the safe side an create a new slice.
	s.chunks = make([]AggrChunk, 0, len(chksA)+len(chksB))

	b := 0
Outer:
	for a := range chksA {
		for {
			if b >= len(chksB) {
				// No more b chunks.
				s.chunks = append(s.chunks, chksA[a:]...)
				break Outer
			}

			cmp := chksA[a].Compare(chksB[b])
			if cmp > 0 {
				s.chunks = append(s.chunks, chksA[a])
				break
			}
			if cmp < 0 {
				s.chunks = append(s.chunks, chksB[b])
				b++
				continue
			}

			// Exact duplicated chunks, discard one from b.
			b++
		}
	}

	if b < len(chksB) {
		s.chunks = append(s.chunks, chksB[b:]...)
	}

	s.adone = !s.a.Next()
	s.bdone = !s.b.Next()
	return true
}

// uniqueSeriesSet takes one series set and ensures each iteration contains single, full series.
type uniqueSeriesSet struct {
	SeriesSet
	done bool

	peek *Series

	lset   labels.Labels
	chunks []AggrChunk
}

func newUniqueSeriesSet(wrapped SeriesSet) *uniqueSeriesSet {
	return &uniqueSeriesSet{SeriesSet: wrapped}
}

func (s *uniqueSeriesSet) At() (labels.Labels, []AggrChunk) {
	return s.lset, s.chunks
}

func (s *uniqueSeriesSet) Next() bool {
	if s.Err() != nil {
		return false
	}

	for !s.done {
		if s.done = !s.SeriesSet.Next(); s.done {
			break
		}
		lset, chks := s.SeriesSet.At()
		if s.peek == nil {
			s.peek = &Series{Labels: labelpb.ZLabelsFromPromLabels(lset), Chunks: chks}
			continue
		}

		if labels.Compare(lset, s.peek.PromLabels()) != 0 {
			s.lset, s.chunks = s.peek.PromLabels(), s.peek.Chunks
			s.peek = &Series{Labels: labelpb.ZLabelsFromPromLabels(lset), Chunks: chks}
			return true
		}

		// We assume non-overlapping, sorted chunks. This is best effort only, if it's otherwise it
		// will just be duplicated, but well handled by StoreAPI consumers.
		s.peek.Chunks = append(s.peek.Chunks, chks...)
	}

	if s.peek == nil {
		return false
	}

	s.lset, s.chunks = s.peek.PromLabels(), s.peek.Chunks
	s.peek = nil
	return true
}

// Compare returns positive 1 if chunk is smaller -1 if larger than b by min time, then max time.
// It returns 0 if chunks are exactly the same.
func (m AggrChunk) Compare(b AggrChunk) int {
	if m.MinTime < b.MinTime {
		return 1
	}
	if m.MinTime > b.MinTime {
		return -1
	}

	// Same min time.
	if m.MaxTime < b.MaxTime {
		return 1
	}
	if m.MaxTime > b.MaxTime {
		return -1
	}

	// We could use proto.Equal, but we need ordering as well.
	for _, cmp := range []func() int{
		func() int { return m.Raw.Compare(b.Raw) },
		func() int { return m.Count.Compare(b.Count) },
		func() int { return m.Sum.Compare(b.Sum) },
		func() int { return m.Min.Compare(b.Min) },
		func() int { return m.Max.Compare(b.Max) },
		func() int { return m.Counter.Compare(b.Counter) },
	} {
		if c := cmp(); c == 0 {
			continue
		} else {
			return c
		}
	}
	return 0
}

// Compare returns positive 1 if chunk is smaller -1 if larger.
// It returns 0 if chunks are exactly the same.
func (m *Chunk) Compare(b *Chunk) int {
	if m == nil && b == nil {
		return 0
	}
	if b == nil {
		return 1
	}
	if m == nil {
		return -1
	}

	if m.Type < b.Type {
		return 1
	}
	if m.Type > b.Type {
		return -1
	}
	return bytes.Compare(m.Data, b.Data)
}

func (x *PartialResponseStrategy) UnmarshalJSON(entry []byte) error {
	fieldStr, err := strconv.Unquote(string(entry))
	if err != nil {
		return errors.Wrapf(err, fmt.Sprintf("failed to unqote %v, in order to unmarshal as 'partial_response_strategy'. Possible values are %s", string(entry), strings.Join(PartialResponseStrategyValues, ",")))
	}

	if len(fieldStr) == 0 {
		// NOTE: For Rule default is abort as this is recommended for alerting.
		*x = PartialResponseStrategy_ABORT
		return nil
	}

	strategy, ok := PartialResponseStrategy_value[strings.ToUpper(fieldStr)]
	if !ok {
		return errors.Errorf(fmt.Sprintf("failed to unmarshal %v as 'partial_response_strategy'. Possible values are %s", string(entry), strings.Join(PartialResponseStrategyValues, ",")))
	}
	*x = PartialResponseStrategy(strategy)
	return nil
}

func (x *PartialResponseStrategy) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(x.String())), nil
}

// PromMatchersToMatchers returns proto matchers from Prometheus matchers.
// NOTE: It allocates memory.
func PromMatchersToMatchers(ms ...*labels.Matcher) ([]LabelMatcher, error) {
	res := make([]LabelMatcher, 0, len(ms))
	for _, m := range ms {
		var t LabelMatcher_Type

		switch m.Type {
		case labels.MatchEqual:
			t = LabelMatcher_EQ
		case labels.MatchNotEqual:
			t = LabelMatcher_NEQ
		case labels.MatchRegexp:
			t = LabelMatcher_RE
		case labels.MatchNotRegexp:
			t = LabelMatcher_NRE
		default:
			return nil, errors.Errorf("unrecognized matcher type %d", m.Type)
		}
		res = append(res, LabelMatcher{Type: t, Name: m.Name, Value: m.Value})
	}
	return res, nil
}

// MatchersToPromMatchers returns Prometheus matchers from proto matchers.
// NOTE: It allocates memory.
func MatchersToPromMatchers(ms ...LabelMatcher) ([]*labels.Matcher, error) {
	res := make([]*labels.Matcher, 0, len(ms))
	for _, m := range ms {
		var t labels.MatchType

		switch m.Type {
		case LabelMatcher_EQ:
			t = labels.MatchEqual
		case LabelMatcher_NEQ:
			t = labels.MatchNotEqual
		case LabelMatcher_RE:
			t = labels.MatchRegexp
		case LabelMatcher_NRE:
			t = labels.MatchNotRegexp
		default:
			return nil, errors.Errorf("unrecognized label matcher type %d", m.Type)
		}
		m, err := labels.NewMatcher(t, m.Name, m.Value)
		if err != nil {
			return nil, err
		}
		res = append(res, m)
	}
	return res, nil
}

// MatchersToString converts label matchers to string format.
// String should be parsable as a valid PromQL query metric selector.
func MatchersToString(ms ...LabelMatcher) string {
	var res string
	for i, m := range ms {
		res += m.PromString()
		if i < len(ms)-1 {
			res += ", "
		}
	}
	return "{" + res + "}"
}

// PromMatchersToString converts prometheus label matchers to string format.
// String should be parsable as a valid PromQL query metric selector.
func PromMatchersToString(ms ...*labels.Matcher) string {
	var res string
	for i, m := range ms {
		res += m.String()
		if i < len(ms)-1 {
			res += ", "
		}
	}
	return "{" + res + "}"
}

func (m *LabelMatcher) PromString() string {
	return fmt.Sprintf("%s%s%q", m.Name, m.Type.PromString(), m.Value)
}

func (x LabelMatcher_Type) PromString() string {
	typeToStr := map[LabelMatcher_Type]string{
		LabelMatcher_EQ:  "=",
		LabelMatcher_NEQ: "!=",
		LabelMatcher_RE:  "=~",
		LabelMatcher_NRE: "!~",
	}
	if str, ok := typeToStr[x]; ok {
		return str
	}
	panic("unknown match type")
}

// PromLabels return Prometheus labels.Labels without extra allocation.
func (m *Series) PromLabels() labels.Labels {
	return labelpb.ZLabelsToPromLabels(m.Labels)
}

// Deprecated.
// TODO(bwplotka): Remove this once Cortex dep will stop using it.
type Label = labelpb.ZLabel

// Deprecated.
// TODO(bwplotka): Remove this in next PR. Done to reduce diff only.
type LabelSet = labelpb.ZLabelSet

// Deprecated.
// TODO(bwplotka): Remove this once Cortex dep will stop using it.
func CompareLabels(a, b []Label) int {
	return labels.Compare(labelpb.ZLabelsToPromLabels(a), labelpb.ZLabelsToPromLabels(b))
}

// Deprecated.
// TODO(bwplotka): Remove this once Cortex dep will stop using it.
func LabelsToPromLabelsUnsafe(lset []Label) labels.Labels {
	return labelpb.ZLabelsToPromLabels(lset)
}
