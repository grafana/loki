package util

import "github.com/grafana/loki/v3/pkg/loghttp"

func MatchLabels(on bool, l loghttp.LabelSet, names []string) loghttp.LabelSet {
	ret := loghttp.LabelSet{}

	nameSet := map[string]struct{}{}
	for _, n := range names {
		nameSet[n] = struct{}{}
	}

	for k, v := range l {
		if _, ok := nameSet[k]; on == ok {
			ret[k] = v
		}
	}

	return ret
}
