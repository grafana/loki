package query

import (
	"fmt"
	"log"
	"strings"

	"github.com/fatih/color"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/loghttp"
)

// TailQuery connects to the Loki websocket endpoint and tails logs
func (q *Query) TailQuery(delayFor int, c *client.Client, out output.LogOutput) {
	conn, err := c.LiveTailQueryConn(q.QueryString, delayFor, q.Limit, q.Start.UnixNano(), q.Quiet)
	if err != nil {
		log.Fatalf("Tailing logs failed: %+v", err)
	}

	tailReponse := new(loghttp.TailResponse)

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

		labels := loghttp.LabelSet{}
		for _, stream := range tailReponse.Streams {
			if !q.NoLabels {

				if len(q.IgnoreLabelsKey) > 0 || len(q.ShowLabelsKey) > 0 {

					ls := stream.Labels

					if len(q.ShowLabelsKey) > 0 {
						ls = matchLabels(true, ls, q.ShowLabelsKey)
					}

					if len(q.IgnoreLabelsKey) > 0 {
						ls = matchLabels(false, ls, q.ShowLabelsKey)
					}

					labels = ls

				} else {
					labels = stream.Labels
				}

			}

			for _, entry := range stream.Entries {
				fmt.Println(out.Format(entry.Timestamp, labels, 0, entry.Line))
			}

		}
		if len(tailReponse.DroppedStreams) != 0 {
			log.Println("Server dropped following entries due to slow client")
			for _, d := range tailReponse.DroppedStreams {
				log.Println(d.Timestamp, d.Labels)
			}
		}
	}
}
