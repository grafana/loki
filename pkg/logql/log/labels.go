package log

import (
	"sort"

	"github.com/prometheus/prometheus/pkg/labels"
)

var (
	emptyLabelsResult = NewLabelsResult(labels.Labels{}, labels.Labels{}.Hash())
)

type LabelsResult interface {
	String() string
	Labels() labels.Labels
	Hash() uint64
}

func NewLabelsResult(lbs labels.Labels, hash uint64) LabelsResult {
	return &labelsResult{lbs: lbs, s: lbs.String(), h: hash}
}

type labelsResult struct {
	lbs labels.Labels
	s   string
	h   uint64
}

func (l labelsResult) String() string {
	return l.s
}

func (l labelsResult) Labels() labels.Labels {
	return l.lbs
}

func (l labelsResult) Hash() uint64 {
	return l.h
}

type hasher struct {
	buf []byte // buffer for computing hash without bytes slice allocation.
}

func newHasher() *hasher {
	return &hasher{
		buf: make([]byte, 0, 1024),
	}
}

func (h *hasher) Hash(lbs labels.Labels) uint64 {
	var hash uint64
	hash, h.buf = lbs.HashWithoutLabels(h.buf, []string(nil)...)
	return hash
}

func (h *hasher) hashWithoutLabels(lbs labels.Labels, groups ...string) uint64 {
	var hash uint64
	hash, h.buf = lbs.HashWithoutLabels(h.buf, groups...)
	return hash
}

func (h *hasher) hashForLabels(lbs labels.Labels, groups ...string) uint64 {
	var hash uint64
	hash, h.buf = lbs.HashForLabels(h.buf, groups...)
	return hash
}

type BaseLabelsBuilder struct {
	// the current base
	base labels.Labels
	del  []string
	add  []labels.Label

	err string

	groups            []string
	without, noLabels bool

	resultCache map[uint64]LabelsResult
	*hasher
}

// LabelsBuilder is the same as labels.Builder but tailored for this package.
type LabelsBuilder struct {
	currentLabels labels.Labels
	currentResult LabelsResult

	*BaseLabelsBuilder
}

func NewBaseLabelsBuilderWithGrouping(groups []string, without, noLabels bool) *BaseLabelsBuilder {
	return &BaseLabelsBuilder{
		del:         make([]string, 0, 5),
		add:         make([]labels.Label, 0, 16),
		resultCache: make(map[uint64]LabelsResult),
		hasher:      newHasher(),
		groups:      groups,
		noLabels:    noLabels,
		without:     without,
	}
}

// NewLabelsBuilder creates a new labels builder.
func NewBaseLabelsBuilder() *BaseLabelsBuilder {
	return NewBaseLabelsBuilderWithGrouping(nil, false, false)
}

func (b *BaseLabelsBuilder) ForLabels(lbs labels.Labels, hash uint64) *LabelsBuilder {
	if labelResult, ok := b.resultCache[hash]; ok {
		res := &LabelsBuilder{
			currentLabels:     lbs,
			currentResult:     labelResult,
			BaseLabelsBuilder: b,
		}
		return res
	}
	labelResult := NewLabelsResult(lbs, hash)
	b.resultCache[hash] = labelResult
	res := &LabelsBuilder{
		currentLabels:     lbs,
		currentResult:     labelResult,
		BaseLabelsBuilder: b,
	}
	return res
}

// Reset clears all current state for the builder.
func (b *LabelsBuilder) Reset() {
	b.base = b.currentLabels
	b.del = b.del[:0]
	b.add = b.add[:0]
	b.err = ""
}

// SetErr sets the error label.
func (b *LabelsBuilder) SetErr(err string) *LabelsBuilder {
	b.err = err
	return b
}

// GetErr return the current error label value.
func (b *LabelsBuilder) GetErr() string {
	return b.err
}

// HasErr tells if the error label has been set.
func (b *LabelsBuilder) HasErr() bool {
	return b.err != ""
}

// BaseHas returns the base labels have the given key
func (b *LabelsBuilder) BaseHas(key string) bool {
	return b.base.Has(key)
}

func (b *LabelsBuilder) Get(key string) (string, bool) {
	for _, a := range b.add {
		if a.Name == key {
			return a.Value, true
		}
	}
	for _, d := range b.del {
		if d == key {
			return "", false
		}
	}

	for _, l := range b.base {
		if l.Name == key {
			return l.Value, true
		}
	}
	return "", false
}

// Del deletes the label of the given name.
func (b *LabelsBuilder) Del(ns ...string) *LabelsBuilder {
	for _, n := range ns {
		for i, a := range b.add {
			if a.Name == n {
				b.add = append(b.add[:i], b.add[i+1:]...)
			}
		}
		b.del = append(b.del, n)
	}
	return b
}

// Set the name/value pair as a label.
func (b *LabelsBuilder) Set(n, v string) *LabelsBuilder {
	for i, a := range b.add {
		if a.Name == n {
			b.add[i].Value = v
			return b
		}
	}
	b.add = append(b.add, labels.Label{Name: n, Value: v})

	return b
}

// Labels returns the labels from the builder. If no modifications
// were made, the original labels are returned.
func (b *LabelsBuilder) Labels() labels.Labels {
	if len(b.del) == 0 && len(b.add) == 0 {
		if b.err == "" {
			return b.base
		}
		res := append(b.base.Copy(), labels.Label{Name: ErrorLabel, Value: b.err})
		sort.Sort(res)
		return res
	}

	// In the general case, labels are removed, modified or moved
	// rather than added.
	res := make(labels.Labels, 0, len(b.base))
Outer:
	for _, l := range b.base {
		for _, n := range b.del {
			if l.Name == n {
				continue Outer
			}
		}
		for _, la := range b.add {
			if l.Name == la.Name {
				continue Outer
			}
		}
		res = append(res, l)
	}
	res = append(res, b.add...)
	if b.err != "" {
		res = append(res, labels.Label{Name: ErrorLabel, Value: b.err})
	}
	sort.Sort(res)

	return res
}

// Labels returns the labels from the builder. If no modifications
// were made, the original labels are returned.
func (b *LabelsBuilder) LabelsResult() LabelsResult {
	// unchanged path.
	if len(b.del) == 0 && len(b.add) == 0 {
		if b.err == "" {
			return b.currentResult
		}
		// unchanged but with error.
		res := append(b.base.Copy(), labels.Label{Name: ErrorLabel, Value: b.err})
		sort.Sort(res)
		return b.toResult(res)
	}

	// In the general case, labels are removed, modified or moved
	// rather than added.
	res := make(labels.Labels, 0, len(b.base)+len(b.add))
Outer:
	for _, l := range b.base {
		for _, n := range b.del {
			if l.Name == n {
				continue Outer
			}
		}
		for _, la := range b.add {
			if l.Name == la.Name {
				continue Outer
			}
		}
		res = append(res, l)
	}
	res = append(res, b.add...)
	if b.err != "" {
		res = append(res, labels.Label{Name: ErrorLabel, Value: b.err})
	}
	sort.Sort(res)

	return b.toResult(res)
}

func (b *BaseLabelsBuilder) toResult(lbs labels.Labels) LabelsResult {
	hash := b.hasher.Hash(lbs)
	if cached, ok := b.resultCache[hash]; ok {
		return cached
	}
	res := NewLabelsResult(lbs, hash)
	b.resultCache[hash] = res
	return res
}

// Labels returns the labels from the builder. If no modifications
// were made, the original labels are returned.
func (b *LabelsBuilder) GroupedLabels() LabelsResult {
	if b.err != "" {
		// We need to return now before applying grouping otherwise the error might get lost.
		return b.LabelsResult()
	}
	if b.noLabels {
		return emptyLabelsResult
	}
	// unchanged path.
	if len(b.del) == 0 && len(b.add) == 0 {
		if len(b.groups) == 0 {
			return b.currentResult
		}
		return b.toGroup(b.currentLabels)
	}

	if b.without {
		return b.withoutResult()
	}
	return b.withResult()
}

func (b *LabelsBuilder) withResult() LabelsResult {
	res := make(labels.Labels, 0, len(b.groups))
Outer:
	for _, g := range b.groups {
		for _, n := range b.del {
			if g == n {
				continue Outer
			}
		}
		for _, la := range b.add {
			if g == la.Name {
				res = append(res, la)
				continue Outer
			}
		}
		for _, l := range b.base {
			if g == l.Name {
				res = append(res, l)
				continue Outer
			}
		}
	}
	return b.toResult(res)
}

func (b *LabelsBuilder) withoutResult() LabelsResult {
	size := len(b.base) + len(b.add) - len(b.del) - len(b.groups)
	if size < 0 {
		size = 0
	}
	res := make(labels.Labels, 0, size)
Outer:
	for _, l := range b.base {
		for _, n := range b.del {
			if l.Name == n {
				continue Outer
			}
		}
		for _, la := range b.add {
			if l.Name == la.Name {
				continue Outer
			}
		}
		for _, lg := range b.groups {
			if l.Name == lg {
				continue Outer
			}
		}
		res = append(res, l)
	}
OuterAdd:
	for _, la := range b.add {
		for _, lg := range b.groups {
			if la.Name == lg {
				continue OuterAdd
			}
		}
		res = append(res, la)
	}
	sort.Sort(res)
	return b.toResult(res)
}

func (b *LabelsBuilder) toGroup(from labels.Labels) LabelsResult {
	var hash uint64
	if b.without {
		hash = b.hasher.hashWithoutLabels(from, b.groups...)
	} else {
		hash = b.hasher.hashForLabels(from, b.groups...)
	}
	if cached, ok := b.resultCache[hash]; ok {
		return cached
	}
	var lbs labels.Labels
	if b.without {
		lbs = from.WithoutLabels(b.groups...)
	} else {
		lbs = from.WithLabels(b.groups...)
	}
	res := NewLabelsResult(lbs, hash)
	b.resultCache[hash] = res
	return res
}
