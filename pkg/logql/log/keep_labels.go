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
		if logqlmodel.IsPipelineErrorLabel(lb.Name) {
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

// Hints implements Stage.
func (kl *KeepLabels) Hints() StageHints {
	return StageHints{CanModifyLabels: true}
}

func (kl *KeepLabels) RequiredLabelNames() []string {
	return []string{}
}
