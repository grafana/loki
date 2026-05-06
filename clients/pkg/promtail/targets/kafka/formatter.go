package kafka

import (
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/pkg/util"
)

func format(lbs labels.Labels, cfg []*relabel.Config) model.LabelSet {
	if lbs.IsEmpty() {
		return nil
	}
	lb := labels.NewBuilder(lbs)
	if len(cfg) > 0 {
		if keep := relabel.ProcessBuilder(lb, cfg...); !keep {
			return nil
		}
	}
	processed := lb.Labels()
	labelOut := model.LabelSet(util.LabelsToMetric(processed))
	for k := range labelOut {
		if strings.HasPrefix(string(k), "__") {
			delete(labelOut, k)
		}
	}
	return labelOut
}
