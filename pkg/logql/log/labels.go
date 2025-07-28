package log

import (
	"fmt"
	"sync"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

const MaxInternedStrings = 1024

var EmptyLabelsResult = NewLabelsResult(labels.EmptyLabels().String(), labels.StableHash(labels.EmptyLabels()), labels.EmptyLabels(), labels.EmptyLabels(), labels.EmptyLabels())

// LabelsResult is a computed labels result that contains the labels set with associated string and hash.
// The is mainly used for caching and returning labels computations out of pipelines and stages.
type LabelsResult interface {
	String() string
	Labels() labels.Labels
	Stream() labels.Labels
	StructuredMetadata() labels.Labels
	Parsed() labels.Labels
	Hash() uint64
}

// NewLabelsResult creates a new LabelsResult.
// It takes the string representation of the labels, the hash of the labels and the labels categorized.
func NewLabelsResult(allLabelsStr string, hash uint64, stream, structuredMetadata, parsed labels.Labels) LabelsResult {
	return &labelsResult{
		s:                  allLabelsStr,
		h:                  hash,
		stream:             stream,
		structuredMetadata: structuredMetadata,
		parsed:             parsed,
	}
}

type labelsResult struct {
	s string
	h uint64

	stream             labels.Labels
	structuredMetadata labels.Labels
	parsed             labels.Labels
}

func (l labelsResult) String() string {
	return l.s
}

func (l labelsResult) Labels() labels.Labels {
	size := l.stream.Len() + l.structuredMetadata.Len() + l.parsed.Len()
	b := labels.NewScratchBuilder(size)

	l.stream.Range(func(l labels.Label) {
		b.Add(l.Name, l.Value)
	})
	l.structuredMetadata.Range(func(l labels.Label) {
		b.Add(l.Name, l.Value)
	})
	l.parsed.Range(func(l labels.Label) {
		b.Add(l.Name, l.Value)
	})

	b.Sort()
	return b.Labels()
}

func (l labelsResult) Hash() uint64 {
	return l.h
}

func (l labelsResult) Stream() labels.Labels {
	return l.stream
}

func (l labelsResult) StructuredMetadata() labels.Labels {
	return l.structuredMetadata
}

func (l labelsResult) Parsed() labels.Labels {
	return l.parsed
}

type LabelCategory int

const (
	StreamLabel LabelCategory = iota
	StructuredMetadataLabel
	ParsedLabel
	InvalidCategory

	numValidCategories = 3
)

var allCategories = []LabelCategory{
	StreamLabel,
	StructuredMetadataLabel,
	ParsedLabel,
}

func categoriesContain(categories []LabelCategory, category LabelCategory) bool {
	for _, c := range categories {
		if c == category {
			return true
		}
	}
	return false
}

// BaseLabelsBuilder is a label builder used by pipeline and stages.
// Only one base builder is used and it contains cache for each LabelsBuilders.
type BaseLabelsBuilder struct {
	del []string
	add [numValidCategories][]labels.Label
	// nolint:structcheck
	// https://github.com/golangci/golangci-lint/issues/826
	err string
	// nolint:structcheck
	errDetails string

	groups                       []string
	baseMap                      map[string]string
	parserKeyHints               ParserHint // label key hints for metric queries that allows to limit parser extractions to only this list of labels.
	without, noLabels            bool
	referencedStructuredMetadata bool
	jsonPaths                    map[string][]string // Maps label names to their original JSON paths

	resultCache map[uint64]LabelsResult
	*hasher
}

// LabelsBuilder is the same as labels.Builder but tailored for this package.
type LabelsBuilder struct {
	base          labels.Labels
	buf           []labels.Label
	currentResult LabelsResult
	groupedResult LabelsResult

	*BaseLabelsBuilder
}

// NewBaseLabelsBuilderWithGrouping creates a new base labels builder with grouping to compute results.
func NewBaseLabelsBuilderWithGrouping(groups []string, parserKeyHints ParserHint, without, noLabels bool) *BaseLabelsBuilder {
	if parserKeyHints == nil {
		parserKeyHints = NoParserHints()
	}

	const labelsCapacity = 16
	return &BaseLabelsBuilder{
		del: make([]string, 0, 5),
		add: [numValidCategories][]labels.Label{
			StreamLabel:             make([]labels.Label, 0, labelsCapacity),
			StructuredMetadataLabel: make([]labels.Label, 0, labelsCapacity),
			ParsedLabel:             make([]labels.Label, 0, labelsCapacity),
		},
		resultCache:    make(map[uint64]LabelsResult),
		hasher:         newHasher(),
		groups:         groups,
		parserKeyHints: parserKeyHints,
		noLabels:       noLabels,
		without:        without,
		jsonPaths:      make(map[string][]string),
	}
}

// NewBaseLabelsBuilder creates a new base labels builder.
func NewBaseLabelsBuilder() *BaseLabelsBuilder {
	return NewBaseLabelsBuilderWithGrouping(nil, NoParserHints(), false, false)
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
	labelResult := NewLabelsResult(lbs.String(), hash, lbs, labels.EmptyLabels(), labels.EmptyLabels())
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
	for k := range b.add {
		b.add[k] = b.add[k][:0]
	}
	b.err = ""
	b.errDetails = ""
	b.baseMap = nil
	b.parserKeyHints.Reset()
}

// ParserLabelHints returns a limited list of expected labels to extract for metric queries.
// Returns nil when it's impossible to hint labels extractions.
func (b *BaseLabelsBuilder) ParserLabelHints() ParserHint {
	return b.parserKeyHints
}

func (b *BaseLabelsBuilder) hasDel() bool {
	return len(b.del) > 0
}

func (b *BaseLabelsBuilder) hasAdd() bool {
	for _, lbls := range b.add {
		if len(lbls) > 0 {
			return true
		}
	}
	return false
}

func (b *BaseLabelsBuilder) sizeAdd() int {
	var length int
	for _, lbls := range b.add {
		length += len(lbls)
	}
	return length
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

// GetWithCategory returns the value and the category of a labels key if it exists.
func (b *LabelsBuilder) GetWithCategory(key string) (string, LabelCategory, bool) {
	v, category, ok := b.getWithCategory(key)
	if category == StructuredMetadataLabel {
		b.referencedStructuredMetadata = true
	}

	return v, category, ok
}

// GetWithCategory returns the value and the category of a labels key if it exists.
func (b *LabelsBuilder) getWithCategory(key string) (string, LabelCategory, bool) {
	for category, lbls := range b.add {
		for _, l := range lbls {
			if l.Name == key {
				return l.Value, LabelCategory(category), true
			}
		}
	}
	for _, d := range b.del {
		if d == key {
			return "", InvalidCategory, false
		}
	}

	value := b.base.Get(key)

	if value != "" {
		return value, StreamLabel, true
	}

	return "", InvalidCategory, false
}

func (b *LabelsBuilder) Get(key string) (string, bool) {
	v, _, ok := b.GetWithCategory(key)
	return v, ok
}

// Del deletes the label of the given name.
func (b *LabelsBuilder) Del(ns ...string) *LabelsBuilder {
	for _, n := range ns {
		for category := range b.add {
			b.deleteWithCategory(LabelCategory(category), n)
		}
		b.del = append(b.del, n)
	}
	return b
}

// deleteWithCategory removes the label from the specified category
func (b *LabelsBuilder) deleteWithCategory(category LabelCategory, n string) {
	for i, l := range b.add[category] {
		if l.Name == n {
			b.add[category] = append(b.add[category][:i], b.add[category][i+1:]...)
		}
	}
}

// Set the name/value pair as a label.
// The value `v` may not be set if a category with higher preference already contains `n`.
// Category preference goes as Parsed > Structured Metadata > Stream.
func (b *LabelsBuilder) Set(category LabelCategory, n, v string) *LabelsBuilder {
	// Parsed takes precedence over Structured Metadata and Stream labels.
	// If category is Parsed, we delete `n` from the structured metadata and stream labels.
	if category == ParsedLabel {
		b.deleteWithCategory(StructuredMetadataLabel, n)
		b.deleteWithCategory(StreamLabel, n)
	}

	// Structured Metadata takes precedence over Stream labels.
	// If category is `StructuredMetadataLabel`,we delete `n` from the stream labels.
	// If `n` exists in the parsed labels, we won't overwrite it's value and we just return what we have.
	if category == StructuredMetadataLabel {
		b.deleteWithCategory(StreamLabel, n)
		if labelsContain(b.add[ParsedLabel], n) {
			return b
		}
	}

	// Finally, if category is `StreamLabel` and `n` already exists in either the structured metadata or
	// parsed labels, the `Set` operation is a noop and we return the unmodified labels builder.
	if category == StreamLabel {
		if labelsContain(b.add[StructuredMetadataLabel], n) || labelsContain(b.add[ParsedLabel], n) {
			return b
		}
	}

	for i, a := range b.add[category] {
		if a.Name == n {
			b.add[category][i].Value = v
			return b
		}
	}
	b.add[category] = append(b.add[category], labels.Label{Name: n, Value: v})

	if category == ParsedLabel {
		// We record parsed labels as extracted so that future parse stages can
		// quickly bypass any existing extracted fields.
		//
		// Note that because this is used for bypassing extracted fields, and
		// because parsed labels always take precedence over structured metadata
		// and stream labels, we must only call RecordExtracted for parsed labels.
		b.parserKeyHints.RecordExtracted(n)
	}
	return b
}

// Add the labels to the builder. If a label with the same name
// already exists in the base labels, a suffix is added to the name.
func (b *LabelsBuilder) Add(category LabelCategory, lbs labels.Labels) *LabelsBuilder {
	lbs.Range(func(l labels.Label) {
		name := l.Name
		if b.BaseHas(name) {
			name = fmt.Sprintf("%s%s", name, duplicateSuffix)
		}

		if name == logqlmodel.ErrorLabel {
			b.err = l.Value
			return
		}

		if name == logqlmodel.ErrorDetailsLabel {
			b.errDetails = l.Value
			return
		}

		b.Set(category, name, l.Value)
	})
	return b
}

// SetJSONPath sets the original JSON path parts that a label came from
func (b *LabelsBuilder) SetJSONPath(labelName string, jsonPath []string) *LabelsBuilder {
	b.jsonPaths[labelName] = jsonPath
	return b
}

// GetJSONPath gets the original JSON path parts for a given label if available
func (b *LabelsBuilder) GetJSONPath(labelName string) []string {
	path, ok := b.jsonPaths[labelName]
	if !ok {
		return nil
	}

	return path
}

func (b *LabelsBuilder) appendErrors(buf []labels.Label) []labels.Label {
	if b.err != "" {
		buf = append(buf, labels.Label{
			Name:  logqlmodel.ErrorLabel,
			Value: b.err,
		})
	}
	if b.errDetails != "" {
		buf = append(buf, labels.Label{
			Name:  logqlmodel.ErrorDetailsLabel,
			Value: b.errDetails,
		})
	}
	return buf
}

func (b *LabelsBuilder) UnsortedLabels(buf []labels.Label, categories ...LabelCategory) []labels.Label {
	if categories == nil {
		categories = allCategories
	}

	if !b.hasDel() && !b.hasAdd() && categoriesContain(categories, StreamLabel) {
		if buf == nil {
			buf = make([]labels.Label, 0, b.base.Len()+1) // +1 for error label.
		} else {
			buf = buf[:0]
		}

		b.base.Range(func(l labels.Label) {
			buf = append(buf, l)
		})

		if categoriesContain(categories, ParsedLabel) {
			buf = b.appendErrors(buf)
		}

		return buf
	}

	// In the general case, labels are removed, modified or moved
	// rather than added.
	if buf == nil {
		size := b.base.Len() + b.sizeAdd() + 1
		buf = make([]labels.Label, 0, size)
	} else {
		buf = buf[:0]
	}

	if categoriesContain(categories, StreamLabel) {
		b.base.Range(func(l labels.Label) {
			// Skip stream labels to be deleted
			for _, n := range b.del {
				if l.Name == n {
					return
				}
			}

			// Skip stream labels which value will be replaced by structured metadata
			if labelsContain(b.add[StructuredMetadataLabel], l.Name) {
				return
			}

			// Skip stream labels which value will be replaced by parsed labels
			if labelsContain(b.add[ParsedLabel], l.Name) {
				return
			}

			// Take value from stream label if present
			if value, found := findLabelValue(b.add[StreamLabel], l.Name); found {
				buf = append(buf, labels.Label{Name: l.Name, Value: value})
			} else {
				buf = append(buf, l)
			}
		})
	}

	if categoriesContain(categories, StructuredMetadataLabel) {
		for _, l := range b.add[StructuredMetadataLabel] {
			if labelsContain(b.add[ParsedLabel], l.Name) {
				continue
			}

			buf = append(buf, l)
		}
	}

	if categoriesContain(categories, ParsedLabel) {
		buf = append(buf, b.add[ParsedLabel]...)
	}
	if (b.HasErr() || b.HasErrorDetails()) && categoriesContain(categories, ParsedLabel) {
		buf = b.appendErrors(buf)
	}

	return buf
}

type stringMapPool struct {
	pool sync.Pool
}

func newStringMapPool() *stringMapPool {
	return &stringMapPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make(map[string]string)
			},
		},
	}
}

func (s *stringMapPool) Get() map[string]string {
	m := s.pool.Get().(map[string]string)
	clear(m)
	return m
}

func (s *stringMapPool) Put(m map[string]string) {
	s.pool.Put(m)
}

var smp = newStringMapPool()

// puts labels entries into an existing map, it is up to the caller to
// properly clear the map if it is going to be reused
func (b *LabelsBuilder) IntoMap(m map[string]string) {
	if !b.hasDel() && !b.hasAdd() && !b.HasErr() {
		if b.baseMap == nil {
			b.baseMap = b.base.Map()
		}
		for k, v := range b.baseMap {
			m[k] = v
		}
		return
	}
	b.buf = b.UnsortedLabels(b.buf)
	// todo should we also cache maps since limited by the result ?
	// Maps also don't create a copy of the labels.
	for _, l := range b.buf {
		m[l.Name] = l.Value
	}
}

func (b *LabelsBuilder) Map() (map[string]string, bool) {
	if !b.hasDel() && !b.hasAdd() && !b.HasErr() {
		if b.baseMap == nil {
			b.baseMap = b.base.Map()
		}
		return b.baseMap, false
	}
	b.buf = b.UnsortedLabels(b.buf)
	// todo should we also cache maps since limited by the result ?
	// Maps also don't create a copy of the labels.
	res := smp.Get()
	for _, l := range b.buf {
		res[l.Name] = l.Value
	}
	return res, true
}

// LabelsResult returns the LabelsResult from the builder.
// No grouping is applied and the cache is used when possible.
func (b *LabelsBuilder) LabelsResult() LabelsResult {
	// unchanged path.
	if !b.hasDel() && !b.hasAdd() && !b.HasErr() {
		return b.currentResult
	}

	// Get all labels at once and sort them
	b.buf = b.UnsortedLabels(b.buf)
	lbls := labels.New(b.buf...)
	hash := b.hasher.Hash(lbls)

	if cached, ok := b.resultCache[hash]; ok {
		return cached
	}

	// Now segregate the sorted labels into their categories
	var stream, meta, parsed []labels.Label

	for _, l := range b.buf {
		// Skip error labels for stream and meta categories
		if l.Name == logqlmodel.ErrorLabel || l.Name == logqlmodel.ErrorDetailsLabel {
			parsed = append(parsed, l)
			continue
		}

		// Check which category this label belongs to
		if labelsContain(b.add[ParsedLabel], l.Name) {
			parsed = append(parsed, l)
		} else if labelsContain(b.add[StructuredMetadataLabel], l.Name) {
			meta = append(meta, l)
		} else {
			stream = append(stream, l)
		}
	}

	result := NewLabelsResult(lbls.String(), hash, labels.New(stream...), labels.New(meta...), labels.New(parsed...))
	b.resultCache[hash] = result

	return result
}

func labelsContain(labels []labels.Label, name string) bool {
	for _, l := range labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

func findLabelValue(labels []labels.Label, name string) (string, bool) {
	for _, l := range labels {
		if l.Name == name {
			return l.Value, true
		}
	}
	return "", false
}

func (b *BaseLabelsBuilder) toUncategorizedResult(buf []labels.Label) LabelsResult {
	lbls := labels.New(buf...)
	hash := b.hasher.Hash(lbls)
	if cached, ok := b.resultCache[hash]; ok {
		return cached
	}

	res := NewLabelsResult(lbls.String(), hash, lbls, labels.EmptyLabels(), labels.EmptyLabels())
	b.resultCache[hash] = res
	return res
}

// GroupedLabels returns the LabelsResult from the builder.
// Groups are applied and the cache is used when possible.
func (b *LabelsBuilder) GroupedLabels() LabelsResult {
	if b.HasErr() {
		// We need to return now before applying grouping otherwise the error might get lost.
		return b.LabelsResult()
	}
	if b.noLabels {
		return EmptyLabelsResult
	}
	// unchanged path.
	if !b.hasDel() && !b.hasAdd() {
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
		b.buf = make([]labels.Label, 0, len(b.groups))
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
		for category, la := range b.add {
			for _, l := range la {
				if g == l.Name {
					if LabelCategory(category) == StructuredMetadataLabel {
						b.referencedStructuredMetadata = true
					}
					b.buf = append(b.buf, l)
					continue Outer
				}
			}
		}

		value := b.base.Get(g)
		if value != "" {
			b.buf = append(b.buf, labels.Label{Name: g, Value: value})
		}
	}
	return b.toUncategorizedResult(b.buf)
}

func (b *LabelsBuilder) withoutResult() LabelsResult {
	if b.buf == nil {
		size := b.base.Len() + b.sizeAdd() - len(b.del) - len(b.groups)
		if size < 0 {
			size = 0
		}
		b.buf = make([]labels.Label, 0, size)
	} else {
		b.buf = b.buf[:0]
	}

	b.base.Range(func(l labels.Label) {
		for _, n := range b.del {
			if l.Name == n {
				return
			}
		}
		for _, lbls := range b.add {
			for _, la := range lbls {
				if l.Name == la.Name {
					return
				}
			}
		}
		for _, lg := range b.groups {
			if l.Name == lg {
				return
			}
		}
		b.buf = append(b.buf, l)
	})

	for category, lbls := range b.add {
	OuterAdd:
		for _, la := range lbls {
			for _, lg := range b.groups {
				if la.Name == lg {
					if LabelCategory(category) == StructuredMetadataLabel {
						b.referencedStructuredMetadata = true
					}
					continue OuterAdd
				}
			}
			b.buf = append(b.buf, la)
		}
	}

	return b.toUncategorizedResult(b.buf)
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
	res := NewLabelsResult(lbs.String(), labels.StableHash(lbs), lbs, labels.EmptyLabels(), labels.EmptyLabels())
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
