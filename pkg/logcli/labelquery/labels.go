package labelquery

import (
	"fmt"
	"log"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/loghttp"
)

// LabelQuery contains all necessary fields to execute label queries and print out the resutls
type LabelQuery struct {
	LabelName string
	Quiet     bool
}

// DoLabels prints out label results
func (q *LabelQuery) DoLabels(c *client.Client) {
	values := q.ListLabels(c)

	for _, value := range values {
		fmt.Println(value)
	}
}

// ListLabels returns an array of label strings
func (q *LabelQuery) ListLabels(c *client.Client) []string {
	var labelResponse *loghttp.LabelResponse
	var err error
	if len(q.LabelName) > 0 {
		labelResponse, err = c.ListLabelValues(q.LabelName, q.Quiet)
	} else {
		labelResponse, err = c.ListLabelNames(q.Quiet)
	}
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}
	return labelResponse.Values
}
