package main

import (
	"fmt"
	"log"
	"time"

	"github.com/grafana/loki/pkg/logproto"
)

type droppedEntry struct {
	Timestamp time.Time
	Labels string
}

type tailResponse struct {
	Stream logproto.Stream
	DroppedEntries []droppedEntry
}

func tailQuery() {
	conn, err := liveTailQueryConn()
	if err != nil {
		log.Fatalf("Tailing logs failed: %+v", err)
	}

	resp := new(tailResponse)

	for {
		err := conn.ReadJSON(resp)
		if err != nil {
			log.Println("Error reading stream:", err)
			return
		}

		labels := ""
		if !*noLabels {
			labels = resp.Stream.Labels
		}
		for _, entry := range resp.Stream.Entries {
			printLogEntry(entry.Timestamp, labels, entry.Line)
		}
		if len(resp.DroppedEntries) != 0 {
			for _, d := range resp.DroppedEntries {
				fmt.Println(d.Timestamp, d.Labels)
			}
		}
	}
}
