package log

import (
	"fmt"
	"sort"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logqlmodel"
)

const MaxInternedStrings = 1024

var EmptyLabelsResult = NewLabelsResult(labels.Labels{}, labels.Labels{}.Hash())
var EmptyCategorizedLabelsResult = categorizedLabelsResult{
	stream:             EmptyLabelsResult,
	structuredMetadata: EmptyLabelsResult,
	parsed:             EmptyLabelsResult,
}

// LabelsResult is a computed labels result that contains the labels set with associated string and hash.
// The is mainly used for caching and returning labels computations out of pipelines and stages.
type LabelsResult interface {
	String() string
	Labels() labels.Labels
	Hash() uint64
}

// NewLabelsResult creates a new LabelsResult from a labels set and a hash.
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

type CategorizedLabelsResult interface {
	LabelsResult
	Stream() LabelsResult
	StructuredMetadata() LabelsResult
	Parsed() LabelsResult
}

// NewCategorizedLabelsResult creates a new NewCategorizedLabelsResult
func NewCategorizedLabelsResult(stream, structuredMetadata, parsed LabelsResult) CategorizedLabelsResult {
	return &categorizedLabelsResult{
		stream:             stream,
		structuredMetadata: structuredMetadata,
		parsed:             parsed,
	}
}

type categorizedLabelsResult struct {
	stream             LabelsResult
	structuredMetadata LabelsResult
	parsed             LabelsResult
}

func (g categorizedLabelsResult) Stream() LabelsResult {
	return g.stream
}

func (g categorizedLabelsResult) StructuredMetadata() LabelsResult {
	return g.structuredMetadata
}

func (g categorizedLabelsResult) Parsed() LabelsResult {
	return g.parsed
}

func (g categorizedLabelsResult) Labels() labels.Labels {
	allLabels := make(labels.Labels, 0, len(g.stream.Labels())+len(g.structuredMetadata.Labels())+len(g.parsed.Labels()))
	allLabels = append(allLabels, g.stream.Labels()...)
	allLabels = append(allLabels, g.structuredMetadata.Labels()...)
	allLabels = append(allLabels, g.parsed.Labels()...)
	return labels.New(allLabels...)
}

func (g categorizedLabelsResult) String() string {
	return g.Labels().String()
}

func (g categorizedLabelsResult) Hash() uint64 {
	return g.Labels().Hash()
}

type hasher struct {
	buf []byte // buffer for computing hash without bytes slice allocation.
}

// newHasher allow to compute hashes for labels by reusing the same buffer.
func newHasher() *hasher {
	return &hasher{
		buf: make([]byte, 0, 1024),
	}
}

// Hash hashes the labels
func (h *hasher) Hash(lbs labels.Labels) uint64 {
	var hash uint64
	hash, h.buf = lbs.HashWithoutLabels(h.buf, []string(nil)...)
	return hash
}

type LabelCategory string

const (
	StreamLabel             LabelCategory = "stream"
	StructuredMetadataLabel LabelCategory = "structured_metadata"
	ParsedLabel             LabelCategory = "parsed"
	InvalidCategory         LabelCategory = ""
)

type CategorizedLabel struct {
	labels.Label
	Category LabelCategory
}

type CategorizedLabels []CategorizedLabel

func (ls CategorizedLabels) Len() int           { return len(ls) }
func (ls CategorizedLabels) Swap(i, j int)      { ls[i], ls[j] = ls[j], ls[i] }
func (ls CategorizedLabels) Less(i, j int) bool { return ls[i].Name < ls[j].Name }

func (ls CategorizedLabels) ToLabels() labels.Labels {
	ret := make(labels.Labels, 0, len(ls))
	for _, l := range ls {
		ret = append(ret, l.Label)
	}
	return ret
}

func CategorizedLabelsFromLabels(category LabelCategory, lbs labels.Labels) CategorizedLabels {
	ret := make(CategorizedLabels, 0, len(lbs))
	for _, l := range lbs {
		ret = append(ret, CategorizedLabel{
			Label:    l,
			Category: category,
		})
	}
	return ret
}

// BaseLabelsBuilder is a label builder used by pipeline and stages.
// Only one base builder is used and it contains cache for each LabelsBuilders.
type BaseLabelsBuilder struct {
	del []string
	add []CategorizedLabel
	// nolint:structcheck
	// https://github.com/golangci/golangci-lint/issues/826
	err string
	// nolint:structcheck
	errDetails string

	groups            []string
	parserKeyHints    ParserHint // label key hints for metric queries that allows to limit parser extractions to only this list of labels.
	without, noLabels bool

	resultCache map[uint64]LabelsResult
	*hasher
}

// LabelsBuilder is the same as labels.Builder but tailored for this package.
type LabelsBuilder struct {
	base          labels.Labels
	baseMap       map[string]string
	buf           CategorizedLabels
	currentResult LabelsResult
	groupedResult LabelsResult

	*BaseLabelsBuilder
}

// NewBaseLabelsBuilderWithGrouping creates a new base labels builder with grouping to compute results.
func NewBaseLabelsBuilderWithGrouping(groups []string, parserKeyHints ParserHint, without, noLabels bool) *BaseLabelsBuilder {
	if parserKeyHints == nil {
		parserKeyHints = noParserHints
	}

	return &BaseLabelsBuilder{
		del:            make([]string, 0, 5),
		add:            make(CategorizedLabels, 0, 16),
		resultCache:    make(map[uint64]LabelsResult),
		hasher:         newHasher(),
		groups:         groups,
		parserKeyHints: parserKeyHints,
		noLabels:       noLabels,
		without:        without,
	}
}

// NewLabelsBuilder creates a new base labels builder.
func NewBaseLabelsBuilder() *BaseLabelsBuilder {
	return NewBaseLabelsBuilderWithGrouping(nil, noParserHints, false, false)
}

// ForLabels creates a labels builder for a given labels set as base.
// The labels cache is shared across all created LabelsBuilders.
func (b *BaseLabelsBuilder) ForLabels(lbs labels.Labels, hash uint64) *LabelsBuilder {
	if labelResult, ok := b.resultCache[hash]; ok {
		res := &LabelsBuilder{
			base:              lbs,
			currentResult:     labelResult,
			BaseLabelsBuilder: b,
		}
		return res
	}
	labelResult := NewLabelsResult(lbs, hash)
	b.resultCache[hash] = labelResult
	res := &LabelsBuilder{
		base:              lbs,
		currentResult:     labelResult,
		BaseLabelsBuilder: b,
	}
	return res
}

// Reset clears all current state for the builder.
func (b *BaseLabelsBuilder) Reset() {
	b.del = b.del[:0]
	b.add = b.add[:0]
	b.err = ""
	b.errDetails = ""
	b.parserKeyHints.Reset()
}

// ParserLabelHints returns a limited list of expected labels to extract for metric queries.
// Returns nil when it's impossible to hint labels extractions.
func (b *BaseLabelsBuilder) ParserLabelHints() ParserHint {
	return b.parserKeyHints
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

func (b *LabelsBuilder) SetErrorDetails(desc string) *LabelsBuilder {
	b.errDetails = desc
	return b
}

func (b *LabelsBuilder) ResetError() *LabelsBuilder {
	b.err = ""
	return b
}

func (b *LabelsBuilder) ResetErrorDetails() *LabelsBuilder {
	b.errDetails = ""
	return b
}

func (b *LabelsBuilder) GetErrorDetails() string {
	return b.errDetails
}

func (b *LabelsBuilder) HasErrorDetails() bool {
	return b.errDetails != ""
}

// BaseHas returns the base labels have the given key
func (b *LabelsBuilder) BaseHas(key string) bool {
	return b.base.Has(key)
}

// Get returns the value of a labels key if it exists.
func (b *LabelsBuilder) get(key string) (string, LabelCategory, bool) {
	for _, a := range b.add {
		if a.Name == key {
			return a.Value, a.Category, true
		}
	}
	for _, d := range b.del {
		if d == key {
			return "", InvalidCategory, false
		}
	}

	for _, l := range b.base {
		if l.Name == key {
			return l.Value, StreamLabel, true
		}
	}
	return "", InvalidCategory, false
}

func (b *LabelsBuilder) Get(key string) (string, bool) {
	v, _, ok := b.get(key)
	return v, ok
}

func (b *LabelsBuilder) GetWithCategory(key string) (string, LabelCategory, bool) {
	return b.get(key)
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
func (b *LabelsBuilder) Set(category LabelCategory, n, v string) *LabelsBuilder {
	for i, a := range b.add {
		if a.Name == n {
			b.add[i].Value = v
			return b
		}
	}
	b.add = append(b.add, CategorizedLabel{
		Label: labels.Label{
			Name:  n,
			Value: v,
		},
		Category: category,
	})

	// Sometimes labels are set and later modified. Only record
	// each label once
	b.parserKeyHints.RecordExtracted(n)
	return b
}

// Add the labels to the builder. If a label with the same name
// already exists in the base labels, a suffix is added to the name.
func (b *LabelsBuilder) Add(category LabelCategory, labels ...labels.Label) *LabelsBuilder {
	for _, l := range labels {
		name := l.Name
		if b.BaseHas(name) {
			name = fmt.Sprintf("%s%s", name, duplicateSuffix)
		}
		b.Set(category, name, l.Value)
	}
	return b
}

// Labels returns the labels from the builder. If no modifications
// were made, the original labels are returned.
func (b *LabelsBuilder) labels() CategorizedLabels {
	b.buf = b.UnsortedLabels(b.buf)
	sort.Sort(b.buf)
	return b.buf
}

func (b *LabelsBuilder) labelsForCategory(category LabelCategory) CategorizedLabels {
	b.buf = b.UnsortedLabels(b.buf)
	filteredLabels := make(CategorizedLabels, 0, len(b.buf))
	for _, l := range b.buf {
		if l.Category == category {
			filteredLabels = append(filteredLabels, l)
		}
	}
	sort.Sort(filteredLabels)
	return filteredLabels
}

func (b *LabelsBuilder) appendErrors(buf CategorizedLabels) CategorizedLabels {
	if b.err != "" {
		buf = append(buf, CategorizedLabel{
			Label: labels.Label{
				Name:  logqlmodel.ErrorLabel,
				Value: b.err,
			},
			Category: ParsedLabel,
		})
	}
	if b.errDetails != "" {
		buf = append(buf, CategorizedLabel{
			Label: labels.Label{
				Name:  logqlmodel.ErrorDetailsLabel,
				Value: b.errDetails,
			},
			Category: ParsedLabel,
		})
	}
	return buf
}

func (b *LabelsBuilder) UnsortedLabels(buf CategorizedLabels) CategorizedLabels {
	if len(b.del) == 0 && len(b.add) == 0 {
		if buf == nil {
			buf = make(CategorizedLabels, 0, len(b.base)+1)
		} else {
			buf = buf[:0]
		}
		for _, l := range b.base {
			buf = append(buf, CategorizedLabel{
				Label:    l,
				Category: StreamLabel,
			})
		}
		return b.appendErrors(buf)
	}

	// In the general case, labels are removed, modified or moved
	// rather than added.
	if buf == nil {
		buf = make(CategorizedLabels, 0, len(b.base)+len(b.add)+1)
	} else {
		buf = buf[:0]
	}
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
		buf = append(buf, CategorizedLabel{
			Label:    l,
			Category: StreamLabel,
		})
	}
	buf = append(buf, b.add...)
	return b.appendErrors(buf)
}

func (b *LabelsBuilder) Map() map[string]string {
	if len(b.del) == 0 && len(b.add) == 0 && b.err == "" {
		if b.baseMap == nil {
			b.baseMap = b.base.Map()
		}
		return b.baseMap
	}
	b.buf = b.UnsortedLabels(b.buf)
	// todo should we also cache maps since limited by the result ?
	// Maps also don't create a copy of the labels.
	res := make(map[string]string, len(b.buf))
	for _, l := range b.buf {
		res[l.Name] = l.Value
	}
	return res
}

// LabelsResult returns the LabelsResult from the builder.
// No grouping is applied and the cache is used when possible.
func (b *LabelsBuilder) LabelsResult() LabelsResult {
	// unchanged path.
	if len(b.del) == 0 && len(b.add) == 0 && b.err == "" {
		return b.currentResult
	}
	return b.toResult(b.labels())
}

// CategorizedLabelsResult returns the LabelsResult from the builder.
// No grouping is applied and the cache is used when possible.
func (b *LabelsBuilder) CategorizedLabelsResult() CategorizedLabelsResult {
	// unchanged path.
	if len(b.del) == 0 && len(b.add) == 0 && b.err == "" {
		return NewCategorizedLabelsResult(b.currentResult, EmptyLabelsResult, EmptyLabelsResult)
	}

	return NewCategorizedLabelsResult(
		b.toResult(b.labelsForCategory(StreamLabel)),
		b.toResult(b.labelsForCategory(StructuredMetadataLabel)),
		b.toResult(b.labelsForCategory(ParsedLabel)),
	)
}

func (b *BaseLabelsBuilder) toResult(buf CategorizedLabels) LabelsResult {
	lbls := buf.ToLabels()
	hash := b.hasher.Hash(lbls)
	if cached, ok := b.resultCache[hash]; ok {
		return cached
	}
	res := NewLabelsResult(lbls, hash)
	b.resultCache[hash] = res
	return res
}

// GroupedLabels returns the LabelsResult from the builder.
// Groups are applied and the cache is used when possible.
func (b *LabelsBuilder) GroupedLabels() LabelsResult {
	if b.err != "" {
		// We need to return now before applying grouping otherwise the error might get lost.
		return b.LabelsResult()
	}
	if b.noLabels {
		return EmptyLabelsResult
	}
	// unchanged path.
	if len(b.del) == 0 && len(b.add) == 0 {
		if len(b.groups) == 0 {
			return b.currentResult
		}
		return b.toBaseGroup()
	}
	// no grouping
	if len(b.groups) == 0 {
		return b.LabelsResult()
	}

	if b.without {
		return b.withoutResult()
	}
	return b.withResult()
}

func (b *LabelsBuilder) withResult() LabelsResult {
	if b.buf == nil {
		b.buf = make(CategorizedLabels, 0, len(b.groups))
	} else {
		b.buf = b.buf[:0]
	}
Outer:
	for _, g := range b.groups {
		for _, n := range b.del {
			if g == n {
				continue Outer
			}
		}
		for _, la := range b.add {
			if g == la.Name {
				b.buf = append(b.buf, la)
				continue Outer
			}
		}
		for _, l := range b.base {
			if g == l.Name {
				b.buf = append(b.buf, CategorizedLabel{
					Label:    l,
					Category: StreamLabel,
				})
				continue Outer
			}
		}
	}
	return b.toResult(b.buf)
}

func (b *LabelsBuilder) withoutResult() LabelsResult {
	if b.buf == nil {
		size := len(b.base) + len(b.add) - len(b.del) - len(b.groups)
		if size < 0 {
			size = 0
		}
		b.buf = make(CategorizedLabels, 0, size)
	} else {
		b.buf = b.buf[:0]
	}
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
		b.buf = append(b.buf, CategorizedLabel{
			Label:    l,
			Category: StreamLabel,
		})
	}
OuterAdd:
	for _, la := range b.add {
		for _, lg := range b.groups {
			if la.Name == lg {
				continue OuterAdd
			}
		}
		b.buf = append(b.buf, la)
	}
	sort.Sort(b.buf)
	return b.toResult(b.buf)
}

func (b *LabelsBuilder) toBaseGroup() LabelsResult {
	if b.groupedResult != nil {
		return b.groupedResult
	}
	var lbs labels.Labels
	if b.without {
		lbs = labels.NewBuilder(b.base).Del(b.groups...).Labels()
	} else {
		lbs = labels.NewBuilder(b.base).Keep(b.groups...).Labels()
	}
	res := NewLabelsResult(lbs, lbs.Hash())
	b.groupedResult = res
	return res
}

type internedStringSet map[string]struct {
	s  string
	ok bool
}

func (i internedStringSet) Get(data []byte, createNew func() (string, bool)) (string, bool) {
	s, ok := i[string(data)]
	if ok {
		return s.s, s.ok
	}
	newStr, ok := createNew()
	if len(i) >= MaxInternedStrings {
		return newStr, ok
	}
	i[string(data)] = struct {
		s  string
		ok bool
	}{s: newStr, ok: ok}
	return newStr, ok
}
