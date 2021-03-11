package query

import (
	"github.com/grafana/loki/pkg/loghttp"
)

// return commonLabels labels between given labels set
func commonLabels(streams loghttp.Streams) loghttp.LabelSet {
	if len(streams) == 0 {
		return nil
	}

	result := streams[0].Labels
	for i := 1; i < len(streams); i++ {
		result = intersect(result, streams[i].Labels)
	}
	return result
}

// intersect two labels set
func intersect(a, b loghttp.LabelSet) loghttp.LabelSet {
	set := loghttp.LabelSet{}

	for ka, va := range a {
		if vb, ok := b[ka]; ok {
			if vb == va {
				set[ka] = va
			}
		}
	}
	return set
}

// subtract labels set b from labels set a
func subtract(a, b loghttp.LabelSet) loghttp.LabelSet {
	set := loghttp.LabelSet{}

	for ka, va := range a {
		if vb, ok := b[ka]; ok {
			if vb == va {
				continue
			}
		}
		set[ka] = va
	}
	return set
}

func matchLabels(on bool, l loghttp.LabelSet, names []string) loghttp.LabelSet {
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
