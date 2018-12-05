package main

import (
	"fmt"
	"log"

	"github.com/grafana/loki/pkg/logproto"
)

func doLabels() {
	var labelResponse *logproto.LabelResponse
	var err error
	if len(*labelName) > 0 {
		labelResponse, err = listLabelValues(*labelName)
	} else {
		labelResponse, err = listLabelNames()
	}
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}
	for _, value := range labelResponse.Values {
		fmt.Println(value)
	}
}

func listLabels() []string {
	labelResponse, err := listLabelNames()
	if err != nil {
		log.Fatalf("Error fetching labels: %+v", err)
	}
	return labelResponse.Values
}
