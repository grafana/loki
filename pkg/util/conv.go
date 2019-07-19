package util

import (
	"math"
	"sort"
	"strings"
	"time"

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

// RoundToMilliseconds returns milliseconds precision time from nanoseconds.
// from will be rounded down to the nearest milliseconds while through is rounded up.
func RoundToMilliseconds(from, through time.Time) (model.Time, model.Time) {
	return model.Time(int64(math.Floor(float64(from.UnixNano()) / float64(time.Millisecond)))),
		model.Time(int64(math.Ceil(float64(through.UnixNano()) / float64(time.Millisecond))))
}
