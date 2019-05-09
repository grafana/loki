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

	resp := new(querier.TailResponse)

	for {
		err := conn.ReadJSON(resp)
		if err != nil {
			log.Println("Error reading stream:", err)
			return
		}
		for _, stream := range resp.Stream {

			labels := ""
			if !*noLabels {
				labels = stream.Labels
			}
			for _, entry := range stream.Entries {
				printLogEntry(entry.Timestamp, labels, entry.Line)
			}
		}
		if len(resp.DroppedEntries) != 0 {
			for _, d := range resp.DroppedEntries {
				fmt.Println(d.Timestamp, d.Labels)
			}
		}
	}
}
