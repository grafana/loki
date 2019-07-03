package util

import (
	"sort"
	"strings"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/common/model"
)

type byLabel []client.LabelAdapter

func (s byLabel) Len() int           { return len(s) }
func (s byLabel) Less(i, j int) bool { return strings.Compare(s[i].Name, s[j].Name) < 0 }
func (s byLabel) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// ToClientLabels parses the labels and converts them to the Cortex type.
func ToClientLabels(labels string) ([]client.LabelAdapter, error) {
	ls, err := logql.ParseExpr(labels)
	if err != nil {
		return nil, err
	}
	matchers := ls.Matchers()
	result := make([]client.LabelAdapter, 0, len(matchers))
	for _, m := range matchers {
		result = append(result, client.LabelAdapter{
			Name:  m.Name,
			Value: m.Value,
		})
	}
	sort.Sort(byLabel(result))
	return result, nil
}

// ModelLabelSetToMap convert a model.LabelSet to a map[string]string
func ModelLabelSetToMap(m model.LabelSet) map[string]string {
	result := map[string]string{}
	for k, v := range m {
		result[string(k)] = string(v)
	}
	return result
}
