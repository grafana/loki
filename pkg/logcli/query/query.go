package query

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/logproto"
)

type Query struct {
	QueryString     string
	Start           time.Time
	End             time.Time
	Limit           int
	Forward         bool
	Quiet           bool
	DelayFor        int
	NoLabels        bool
	IgnoreLabelsKey []string
	ShowLabelsKey   []string
	FixedLabelsLen  int
}

func (q *Query) DoQuery(c *client.Client, out output.LogOutput) {
	var (
		i      iter.EntryIterator
		common labels.Labels
	)

	d := logproto.BACKWARD
	if q.Forward {
		d = logproto.FORWARD
	}

	resp, err := c.Query(q.QueryString, q.Limit, q.Start, q.End, d, q.Quiet)
	if err != nil {
		log.Fatalf("Query failed: %+v", err)
	}

	cache, lss := parseLabels(resp)

	labelsCache := func(labels string) labels.Labels {
		return cache[labels]
	}

	common = commonLabels(lss)

	// Remove the labels we want to show from common
	if len(q.ShowLabelsKey) > 0 {
		common = common.MatchLabels(false, q.ShowLabelsKey...)
	}

	if len(common) > 0 && !q.Quiet {
		log.Println("Common labels:", color.RedString(common.String()))
	}

	if len(q.IgnoreLabelsKey) > 0 && !q.Quiet {
		log.Println("Ignoring labels key:", color.RedString(strings.Join(q.IgnoreLabelsKey, ",")))
	}

	// Remove ignored and common labels from the cached labels and
	// calculate the max labels length
	maxLabelsLen := q.FixedLabelsLen
	for key, ls := range cache {
		// Remove common labels
		ls = subtract(ls, common)

		// Remove ignored labels
		if len(q.IgnoreLabelsKey) > 0 {
			ls = ls.MatchLabels(false, q.IgnoreLabelsKey...)
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
