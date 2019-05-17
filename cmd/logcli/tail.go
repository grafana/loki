package main

import (
	"fmt"
	"log"

	"github.com/grafana/loki/pkg/querier"
)

func tailQuery() {
	conn, err := liveTailQueryConn()
	if err != nil {
		log.Fatalf("Tailing logs failed: %+v", err)
	}

	tailReponse := new(querier.TailResponse)

	for {
		err := conn.ReadJSON(tailReponse)
		if err != nil {
			log.Println("Error reading stream:", err)
			return
		}
		for _, stream := range tailReponse.Streams {

			labels := ""
			if !*noLabels {
				labels = stream.Labels
			}
			for _, entry := range stream.Entries {
				printLogEntry(entry.Timestamp, labels, entry.Line)
			}
		}
		if len(tailReponse.DroppedEntries) != 0 {
			for _, d := range tailReponse.DroppedEntries {
				fmt.Println(d.Timestamp, d.Labels)
			}
		}
	}
}
