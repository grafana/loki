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

	del := make([]string, 0, 10)
	lbls.Range(func(name, value []byte) {
		if isSpecialLabel(unsafeGetString(name)) {
			return
		}

		var keep bool
		for _, keepLabel := range kl.labels {
			if keepLabel.Matcher != nil && keepLabel.Matcher.Name == unsafeGetString(name) && keepLabel.Matcher.Matches(unsafeGetString(value)) {
				keep = true
				break
			}

			if keepLabel.Name == unsafeGetString(name) {
				keep = true
				break
			}
		}

		if !keep {
			del = append(del, unsafeGetString(name))
		}
	})
	for _, name := range del {
		lbls.Del(name)
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
