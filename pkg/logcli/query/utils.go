package query

import (
	"log"
	"sort"

	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
)

// parse labels from string
func mustParseLabels(labels string) labels.Labels {
	ls, err := promql.ParseMetric(labels)
	if err != nil {
		log.Fatalf("Failed to parse labels: %+v", err)
	}
	return ls
}

// parse labels from response stream
func parseLabels(streams logql.Streams) (map[string]labels.Labels, []labels.Labels) {
	cache := make(map[string]labels.Labels, len(streams))
	lss := make([]labels.Labels, 0, len(streams))
	for _, stream := range streams {
		ls := mustParseLabels(stream.Labels)
		cache[stream.Labels] = ls
		lss = append(lss, ls)
	}
	return cache, lss
}

// return commonLabels labels between given labels set
func commonLabels(lss []labels.Labels) labels.Labels {
	if len(lss) == 0 {
		return nil
	}

	result := lss[0]
	for i := 1; i < len(lss); i++ {
		result = intersect(result, lss[i])
	}
	return result
}

// intersect two labels set
func intersect(a, b labels.Labels) labels.Labels {

	set := labels.Labels{}
	ma := a.Map()
	mb := b.Map()

	for ka, va := range ma {
		if vb, ok := mb[ka]; ok {
			if vb == va {
				set = append(set, labels.Label{
					Name:  ka,
					Value: va,
				})
			}
		}
	}
	sort.Sort(set)
	return set
}

// subtract labels set b from labels set a
func subtract(a, b labels.Labels) labels.Labels {

	set := labels.Labels{}
	ma := a.Map()
	mb := b.Map()

	for ka, va := range ma {
		if vb, ok := mb[ka]; ok {
			if vb == va {
				continue
			}
		}
		set = append(set, labels.Label{
			Name:  ka,
			Value: va,
		})
	}
	sort.Sort(set)
	return set
}
