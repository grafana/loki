package main

import (
	"log"
	"strings"

	"github.com/fatih/color"

	"github.com/grafana/loki/pkg/logproto"
)

func tailQuery() {
	conn, err := liveTailQueryConn()
	if err != nil {
		log.Fatalf("Tailing logs failed: %+v", err)
	}

	stream := new(logproto.Stream)

	if len(*ignoreLabelsKey) > 0 {
		log.Println("Ingoring labels key:", color.RedString(strings.Join(*ignoreLabelsKey, ",")))
	}

	if len(*showLabelsKey) > 0 {
		log.Println("Print only labels key:", color.RedString(strings.Join(*showLabelsKey, ",")))
	}

	for {
		err := conn.ReadJSON(stream)
		if err != nil {
			log.Println("Error reading stream:", err)
			return
		}

		labels := ""
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
			printLogEntry(entry.Timestamp, labels, entry.Line)
		}
	}
}
