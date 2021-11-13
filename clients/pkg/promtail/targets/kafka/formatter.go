package kafka

import (
	"strings"

	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/relabel"
)

func format(lbs labels.Labels, cfg []*relabel.Config) model.LabelSet {
	if len(lbs) == 0 {
		return nil
	}
	processed := relabel.Process(lbs, cfg...)
	labelOut := model.LabelSet(util.LabelsToMetric(processed))
	for k := range labelOut {
		if strings.HasPrefix(string(k), "__") {
			delete(labelOut, k)
		}
	}
	return labelOut
}
