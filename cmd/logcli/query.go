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

func getStart(end time.Time) time.Time {
	start := end.Add(-*since)
	if *from != "" {
		var err error
		start, err = time.Parse(time.RFC3339Nano, *from)
		if err != nil {
			log.Fatalf("error parsing date '%s': %s", *from, err)
		}
	}
	return start
}

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
	start := getStart(end)

	if *to != "" {
		var err error
		end, err = time.Parse(time.RFC3339Nano, *to)
		if err != nil {
			log.Fatalf("error parsing --to date '%s': %s", *to, err)
		}
	}

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

	if len(common) > 0 && !*quiet {
		log.Println("Common labels:", color.RedString(common.String()))
	}

	if len(*ignoreLabelsKey) > 0 && !*quiet {
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

	Outputs["default"] = DefaultOutput{
		MaxLabelsLen: maxLabelsLen,
		CommonLabels: common,
	}

	for i.Next() {
		ls := labelsCache(i.Labels())
		Outputs[*outputMode].Print(i.Entry().Timestamp, &ls, i.Entry().Line)
	}

	if err := i.Error(); err != nil {
		log.Fatalf("Error from iterator: %v", err)
	}
}
