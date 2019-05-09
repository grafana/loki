package main

import (
	"log"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/prometheus/prometheus/pkg/labels"

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
		log.Println("Common labels:", color.RedString(common.String()))
	}

	if len(*ignoreLabelsKey) > 0 {
		log.Println("Ignoring labels key:", color.RedString(strings.Join(*ignoreLabelsKey, ",")))
	}

	// Get the max size of labels
	maxLabelsLen := *fixedLabelsLen
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
