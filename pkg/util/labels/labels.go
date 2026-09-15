package labels

import (
	"unsafe"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// ModelLabelSetToMap convert a model.LabelSet to a map[string]string
func ModelLabelSetToMap(m model.LabelSet) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}
	return *(*map[string]string)(unsafe.Pointer(&m)) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}

// MapToModelLabelSet converts a map into a model.LabelSet
func MapToModelLabelSet(m map[string]string) model.LabelSet {
	if len(m) == 0 {
		return model.LabelSet{}
	}
	return *(*map[model.LabelName]model.LabelValue)(unsafe.Pointer(&m)) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}

func AddLabelSetToBuilder(b *labels.Builder, ls model.LabelSet) {
	for name, value := range ls {
		b.Set(string(name), string(value))
	}
}

func BuilderToLabelSet(ls *labels.Builder) model.LabelSet {
	m := make(model.LabelSet)
	ls.Range(func(l labels.Label) {
		m[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	})
	return m
}
