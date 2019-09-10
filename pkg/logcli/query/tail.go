package query

import (
	"fmt"
	"log"
	"strings"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/querier"

	"github.com/fatih/color"
	promlabels "github.com/prometheus/prometheus/pkg/labels"
)

func (q *Query) TailQuery(delayFor int, c *client.Client, out output.LogOutput) {
	conn, err := c.LiveTailQueryConn(q.QueryString, delayFor, q.Limit, q.Start.UnixNano(), q.Quiet)
	if err != nil {
		log.Fatalf("Tailing logs failed: %+v", err)
	}

	tailReponse := new(querier.TailResponse)

	if len(q.IgnoreLabelsKey) > 0 {
		log.Println("Ignoring labels key:", color.RedString(strings.Join(q.IgnoreLabelsKey, ",")))
	}

	if len(q.ShowLabelsKey) > 0 {
		log.Println("Print only labels key:", color.RedString(strings.Join(q.ShowLabelsKey, ",")))
	}

	for {
		err := conn.ReadJSON(tailReponse)
		if err != nil {
			log.Println("Error reading stream:", err)
			return
		}

		labels := ""
		parsedLabels := promlabels.Labels{}
		for _, stream := range tailReponse.Streams {
			if !q.NoLabels {

				if len(q.IgnoreLabelsKey) > 0 || len(q.ShowLabelsKey) > 0 {

					ls := mustParseLabels(stream.GetLabels())

					if len(q.ShowLabelsKey) > 0 {
						ls = ls.MatchLabels(true, q.ShowLabelsKey...)
					}

					if len(q.IgnoreLabelsKey) > 0 {
						ls = ls.MatchLabels(false, q.IgnoreLabelsKey...)
					}

					labels = ls.String()

				} else {
					labels = stream.Labels
				}
				parsedLabels = mustParseLabels(labels)
			}

			for _, entry := range stream.Entries {
				fmt.Println(out.Format(entry.Timestamp, &parsedLabels, 0, entry.Line))
			}

		}
		if len(tailReponse.DroppedEntries) != 0 {
			log.Println("Server dropped following entries due to slow client")
			for _, d := range tailReponse.DroppedEntries {
				log.Println(d.Timestamp, d.Labels)
			}
		}
	}
}
