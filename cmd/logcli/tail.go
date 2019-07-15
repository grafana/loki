package main

import (
	"log"
	"strings"

	"github.com/grafana/loki/pkg/querier"

	"github.com/fatih/color"
)

func tailQuery() {
	conn, err := liveTailQueryConn()
	if err != nil {
		log.Fatalf("Tailing logs failed: %+v", err)
	}

	tailReponse := new(querier.TailResponse)

	if len(*ignoreLabelsKey) > 0 {
		log.Println("Ingoring labels key:", color.RedString(strings.Join(*ignoreLabelsKey, ",")))
	}

	if len(*showLabelsKey) > 0 {
		log.Println("Print only labels key:", color.RedString(strings.Join(*showLabelsKey, ",")))
	}

	for {
		err := conn.ReadJSON(tailReponse)
		if err != nil {
			log.Println("Error reading stream:", err)
			return
		}

		labels := ""
		for _, stream := range tailReponse.Streams {
			if !*noLabels {

				if len(*ignoreLabelsKey) > 0 || len(*showLabelsKey) > 0 {

					ls := mustParseLabels(stream.GetLabels())

					if len(*showLabelsKey) > 0 {
						ls = ls.MatchLabels(true, *showLabelsKey...)
					}

					if len(*ignoreLabelsKey) > 0 {
						ls = ls.MatchLabels(false, *ignoreLabelsKey...)
					}

					labels = ls.String()

				} else {

					labels = stream.Labels
				}
			}

			for _, entry := range stream.Entries {
				lbls := mustParseLabels(labels)
				Outputs[*outputMode].Print(entry.Timestamp, &lbls, entry.Line)
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
