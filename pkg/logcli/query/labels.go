package query

import (
	"fmt"
	"log"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logproto"
)

func DoLabels(labelName string, quiet bool, c *client.Client) {
	var labelResponse *logproto.LabelResponse
	var err error
	if len(labelName) > 0 {
		labelResponse, err = c.ListLabelValues(labelName, quiet)
	} else {
		labelResponse, err = c.ListLabelNames(quiet)
	}
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}
	for _, value := range labelResponse.Values {
		fmt.Println(value)
	}
}

func ListLabels(quiet bool, c *client.Client) []string {
	labelResponse, err := c.ListLabelNames(quiet)
	if err != nil {
		log.Fatalf("Error fetching labels: %+v", err)
	}
	return labelResponse.Values
}
