package main

import (
	"log"

	"github.com/grafana/loki/pkg/logproto"
)

func tailQuery() {
	conn, err := liveTailQueryConn()
	if err != nil {
		log.Fatalf("Tailing logs failed: %+v", err)
	}

	stream := new(logproto.Stream)

	for {
		err := conn.ReadJSON(stream)
		if err != nil {
			log.Println("Error reading stream:", err)
			return
		}

		labels := ""
		if !*noLabels {
			labels = stream.Labels
		}
		for _, entry := range stream.Entries {
			printLogEntry(entry.Timestamp, labels, entry.Line)
		}
	}
}
