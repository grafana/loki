package log

import (
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

type KeepLabels struct {
	labels []NamedLabelMatcher
}

func NewKeepLabels(labels []NamedLabelMatcher) *KeepLabels {
	return &KeepLabels{labels: labels}
}

func (kl *KeepLabels) Process(_ int64, line []byte, lbls *LabelsBuilder) ([]byte, bool) {
	if len(kl.labels) == 0 {
		return line, true
	}

	// TODO: Reuse buf?
	for _, lb := range lbls.UnsortedLabels(nil) {
		if isSpecialLabel(lb.Name) {
			continue
		}

		var keep bool
		for _, keepLabel := range kl.labels {
			if keepLabel.Matcher != nil && keepLabel.Matcher.Name == lb.Name && keepLabel.Matcher.Matches(lb.Value) {
				keep = true
				break
			}

			if keepLabel.Name == lb.Name {
				keep = true
				break
			}
		}

		if !keep {
			lbls.Del(lb.Name)
		}
	}

	return line, true
}

func (kl *KeepLabels) RequiredLabelNames() []string {
	return []string{}
}

func isSpecialLabel(lblName string) bool {
	switch lblName {
	case logqlmodel.ErrorLabel, logqlmodel.ErrorDetailsLabel, logqlmodel.PreserveErrorLabel:
		return true
	}

	return false
}
