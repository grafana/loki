package log

import (
	"github.com/prometheus/prometheus/model/labels"
)

type KeepLabels struct {
	keepLabels []KeepLabel
}

type KeepLabel struct {
	Matcher *labels.Matcher
	Name    string
}

func NewKeepLabel(matcher *labels.Matcher, name string) KeepLabel {
	return KeepLabel{
		Matcher: matcher,
		Name:    name,
	}
}

func NewKeepLabels(dl []KeepLabel) *KeepLabels {
	return &KeepLabels{keepLabels: dl}
}

func (kl *KeepLabels) Process(_ int64, line []byte, lbls *LabelsBuilder) ([]byte, bool) {
	if len(kl.keepLabels) == 0 {
		return line, true
	}

	// TODO: Reuse buf?
	for _, lb := range lbls.UnsortedLabels(nil) {
		var keep bool
		for _, keepLabel := range kl.keepLabels {
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
