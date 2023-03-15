package log

import (
	"fmt"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logqlmodel"
)

type DropLabels struct {
	dropLabels []DropLabel
}

type DropLabel struct {
	Matcher     *labels.Matcher
	Name        string
	description string
}

func NewDropLabel(matcher *labels.Matcher, name string) DropLabel {
	return DropLabel{
		Matcher:     matcher,
		Name:        name,
		description: fmt.Sprintf("{name%s, value%s}", matcher.Name, matcher.Value),
	}
}

func NewDropLabels(dl []DropLabel) *DropLabels {
	return &DropLabels{dropLabels: dl}
}

func (dl *DropLabels) String() string {
	return "DropLabels"
}

func (dl *DropLabels) Description() string {
	return "drop labels"
}

func (dl *DropLabels) Process(ts int64, line []byte, lbls *LabelsBuilder) ([]byte, bool) {
	for _, dropLabel := range dl.dropLabels {
		if dropLabel.Matcher != nil {
			dropLabelMatches(dropLabel.Matcher, lbls)
			continue
		}
		name := dropLabel.Name
		dropLabelNames(name, lbls)
	}
	return line, true
}

func (dl *DropLabels) RequiredLabelNames() []string { return []string{} }

func isErrorLabel(name string) bool {
	return name == logqlmodel.ErrorLabel
}

func isErrorDetailsLabel(name string) bool {
	return name == logqlmodel.ErrorDetailsLabel
}

func dropLabelNames(name string, lbls *LabelsBuilder) {
	if isErrorLabel(name) {
		lbls.ResetError()
		return
	}
	if isErrorDetailsLabel(name) {
		lbls.ResetErrorDetails()
		return
	}
	if _, ok := lbls.Get(name); ok {
		lbls.Del(name)
	}
}

func dropLabelMatches(matcher *labels.Matcher, lbls *LabelsBuilder) {
	var value string
	name := matcher.Name
	if isErrorLabel(name) {
		value = lbls.GetErr()
		if matcher.Matches(value) {
			lbls.ResetError()
		}
		return
	}
	if isErrorDetailsLabel(name) {
		value = lbls.GetErrorDetails()
		if matcher.Matches(value) {
			lbls.ResetErrorDetails()
		}
		return
	}
	value, _ = lbls.Get(name)
	if matcher.Matches(value) {
		lbls.Del(name)
	}
}
