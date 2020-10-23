package log

import (
	"sort"

	"github.com/prometheus/prometheus/pkg/labels"
)

// LabelsBuilder is the same as labels.Builder but tailored for this package.
type LabelsBuilder struct {
	base labels.Labels
	del  []string
	add  []labels.Label

	err string
}

// NewLabelsBuilder creates a new labels builder.
func NewLabelsBuilder() *LabelsBuilder {
	return &LabelsBuilder{
		del: make([]string, 0, 5),
		add: make([]labels.Label, 0, 5),
	}
}

// Reset clears all current state for the builder.
func (b *LabelsBuilder) Reset(base labels.Labels) {
	b.base = base
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

// Base returns the base labels unmodified
func (b *LabelsBuilder) Base() labels.Labels {
	return b.base
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

func (b *LabelsBuilder) WithoutLabels(names ...string) labels.Labels {
	// naive implementation for now.
	return b.Labels().WithoutLabels(names...)
}

func (b *LabelsBuilder) WithLabels(names ...string) labels.Labels {
	// naive implementation for now.
	return b.Labels().WithLabels(names...)
}
