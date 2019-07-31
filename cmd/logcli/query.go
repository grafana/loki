package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logcli/output"
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

func doQuery(out output.LogOutput) {
	if *tail {
		tailQuery(out)
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

	// Remove ignored and common labels from the cached labels and
	// calculate the max labels length
	maxLabelsLen := *fixedLabelsLen
	for key, ls := range cache {
		// Remove common labels
		ls = subtract(ls, common)

		// Remove ignored labels
		if len(*ignoreLabelsKey) > 0 {
			ls = ls.MatchLabels(false, *ignoreLabelsKey...)
		}

		// Update cached labels
		cache[key] = ls

		// Update max labels length
		len := len(ls.String())
		if maxLabelsLen < len {
			maxLabelsLen = len
		}
	}

	i = iter.NewQueryResponseIterator(resp, d)

	for i.Next() {
		ls := labelsCache(i.Labels())
		fmt.Println(out.Format(i.Entry().Timestamp, &ls, maxLabelsLen, i.Entry().Line))
	}

	if err := i.Error(); err != nil {
		log.Fatalf("Error from iterator: %v", err)
	}
}
