// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storepb

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unsafe"

	"github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/thanos-io/thanos/pkg/store/storepb/prompb"
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

// CompareLabels compares two sets of labels.
// After lexicographical order, the set with fewer labels comes first.
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

	lset   []Label
	chunks []AggrChunk
}

func newUniqueSeriesSet(wrapped SeriesSet) *uniqueSeriesSet {
	return &uniqueSeriesSet{SeriesSet: wrapped}
}

func (s *uniqueSeriesSet) At() ([]Label, []AggrChunk) {
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
			s.peek = &Series{Labels: lset, Chunks: chks}
			continue
		}

		if CompareLabels(lset, s.peek.Labels) != 0 {
			s.lset, s.chunks = s.peek.Labels, s.peek.Chunks
			s.peek = &Series{Labels: lset, Chunks: chks}
			return true
		}

		// We assume non-overlapping, sorted chunks. This is best effort only, if it's otherwise it
		// will just be duplicated, but well handled by StoreAPI consumers.
		s.peek.Chunks = append(s.peek.Chunks, chks...)
	}

	if s.peek == nil {
		return false
	}

	s.lset, s.chunks = s.peek.Labels, s.peek.Chunks
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

// LabelsToPromLabels converts Thanos proto labels to Prometheus labels in type safe manner.
// NOTE: It allocates memory.
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
// NOTE: It allocates memory.
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
// NOTE: This depends on order of struct fields etc, so use with extreme care.
func PromLabelsToLabelsUnsafe(lset labels.Labels) []Label {
	return *(*[]Label)(unsafe.Pointer(&lset))
}

// PrompbLabelsToLabelsUnsafe converts Prometheus proto labels to Thanos proto labels in type unsafe manner.
// It reuses the same memory. Caller should abort using passed labels.Labels.
//
// NOTE: This depends on order of struct fields etc, so use with extreme care.
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

// ExtendLabels extend given labels by extend in labels format.
// The type conversion is done safely, which means we don't modify extend labels underlying array.
//
// In case of existing labels already present in given label set, it will be overwritten by external one.
func ExtendLabels(lset []Label, extend labels.Labels) []Label {
	overwritten := map[string]struct{}{}
	for i, l := range lset {
		if v := extend.Get(l.Name); v != "" {
			lset[i].Value = v
			overwritten[l.Name] = struct{}{}
		}
	}

	for _, l := range extend {
		if _, ok := overwritten[l.Name]; ok {
			continue
		}
		lset = append(lset, Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}
	sort.Slice(lset, func(i, j int) bool {
		return lset[i].Name < lset[j].Name
	})
	return lset
}

// TranslatePromMatchers returns proto matchers from Prometheus matchers.
// NOTE: It allocates memory.
func TranslatePromMatchers(ms ...*labels.Matcher) ([]LabelMatcher, error) {
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

// TranslateFromPromMatchers returns Prometheus matchers from proto matchers.
// NOTE: It allocates memory.
func TranslateFromPromMatchers(ms ...LabelMatcher) ([]*labels.Matcher, error) {
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
			return nil, errors.Errorf("unrecognized matcher type %d", m.Type)
		}
		res = append(res, &labels.Matcher{Type: t, Name: m.Name, Value: m.Value})
	}
	return res, nil
}
