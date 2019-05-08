package main

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

func doQuery() {
	if *tail {
		tailQuery()
		return
	}

	var (
		i      iter.EntryIterator
		common labels.Labels
	)

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

	labelsCache := func(labels string) labels.Labels {
		return cache[labels]
	}

	common = commonLabels(lss)

	// Remove the labels we want to show from common
	if len(*showLabelsKey) > 0 {
		common = common.MatchLabels(false, *showLabelsKey...)
	}

	if len(common) > 0 {
		fmt.Println("Common labels:", color.RedString(common.String()))
	}

	if len(*ignoreLabelsKey) > 0 {
		fmt.Println("Ignoring labels key:", color.RedString(strings.Join(*ignoreLabelsKey, ",")))
	}

	// Get the max size of labels
	maxLabelsLen := 0
	for _, ls := range cache {
		ls = subtract(common, ls)
		if len(*ignoreLabelsKey) > 0 {
			ls = ls.MatchLabels(false, *ignoreLabelsKey...)
		}
		len := len(ls.String())
		if maxLabelsLen < len {
			maxLabelsLen = len
		}
	}

	i = iter.NewQueryResponseIterator(resp, d)

	for i.Next() {
		ls := labelsCache(i.Labels())
		ls = subtract(ls, common)
		if len(*ignoreLabelsKey) > 0 {
			ls = ls.MatchLabels(false, *ignoreLabelsKey...)
		}

		labels := ""
		if !*noLabels {
			labels = padLabel(ls, maxLabelsLen)
		}

		printLogEntry(i.Entry().Timestamp, labels, i.Entry().Line)
	}

	if err := i.Error(); err != nil {
		log.Fatalf("Error from iterator: %v", err)
	}
}

func printLogEntry(ts time.Time, lbls string, line string) {
	fmt.Println(
		color.BlueString(ts.Format(time.RFC3339)),
		color.RedString(lbls),
		strings.TrimSpace(line),
	)
}

func padLabel(ls labels.Labels, maxLabelsLen int) string {
	labels := ls.String()
	if len(labels) < maxLabelsLen {
		labels += strings.Repeat(" ", maxLabelsLen-len(labels))
	}
	return labels
}

func mustParseLabels(labels string) labels.Labels {
	ls, err := promql.ParseMetric(labels)
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
