package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/parser"
)

func doQuery() {
	var (
		i            iter.EntryIterator
		labelsCache  = mustParseLabels
		common       labels.Labels
		maxLabelsLen = 100
	)

	if *tail {
		i = tailQuery()
	} else {
		end := time.Now()
		start := end.Add(-*since)
		d := logproto.BACKWARD
		if *forward {
			d = logproto.FORWARD
		}

		resp, err := query(start, end, d)
		if err != nil {
			log.Fatalf("Query failed: %+v", err)
		}

		cache, lss := parseLabels(resp)

		labelsCache = func(labels string) labels.Labels {
			return cache[labels]
		}
		common = commonLabels(lss)
		i = iter.NewQueryResponseIterator(resp, d)

		if len(common) > 0 {
			fmt.Println("Common labels:", color.RedString(common.String()))
		}

		for _, ls := range cache {
			ls = subtract(common, ls)
			len := len(ls.String())
			if maxLabelsLen < len {
				maxLabelsLen = len
			}
		}
	}

	for i.Next() {
		ls := labelsCache(i.Labels())
		ls = subtract(ls, common)
		fmt.Println(
			color.BlueString(i.Entry().Timestamp.Format(time.RFC3339)),
			color.RedString(padLabel(ls, maxLabelsLen)),
			strings.TrimSpace(i.Entry().Line),
		)
	}

	if err := i.Error(); err != nil {
		log.Fatalf("Error from iterator: %v", err)
	}
}

func padLabel(ls labels.Labels, maxLabelsLen int) string {
	labels := ls.String()
	if len(labels) < maxLabelsLen {
		labels += strings.Repeat(" ", maxLabelsLen-len(labels))
	}
	return labels
}

func mustParseLabels(labels string) labels.Labels {
	ls, err := parser.Labels(labels)
	if err != nil {
		log.Fatalf("Failed to parse labels: %+v", err)
	}
	return ls
}

func parseLabels(resp *logproto.QueryResponse) (map[string]labels.Labels, []labels.Labels) {
	cache := make(map[string]labels.Labels, len(resp.Streams))
	lss := make([]labels.Labels, 0, len(resp.Streams))
	for _, stream := range resp.Streams {
		ls := mustParseLabels(stream.Labels)
		cache[stream.Labels] = ls
		lss = append(lss, ls)
	}
	return cache, lss
}

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

func intersect(a, b labels.Labels) labels.Labels {
	var result labels.Labels
	for i, j := 0, 0; i < len(a) && j < len(b); {
		k := strings.Compare(a[i].Name, b[j].Name)
		switch {
		case k == 0:
			if a[i].Value == b[j].Value {
				result = append(result, a[i])
			}
			i++
			j++
		case k < 0:
			i++
		case k > 0:
			j++
		}
	}
	return result
}

// subtract b from a
func subtract(a, b labels.Labels) labels.Labels {
	var result labels.Labels
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		k := strings.Compare(a[i].Name, b[j].Name)
		if k != 0 || a[i].Value != b[j].Value {
			result = append(result, a[i])
		}
		switch {
		case k == 0:
			i++
			j++
		case k < 0:
			i++
		case k > 0:
			j++
		}
	}
	for ; i < len(a); i++ {
		result = append(result, a[i])
	}
	return result
}
