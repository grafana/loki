package labelquery

import (
	"fmt"
	"log"
	"time"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

// LabelQuery contains all necessary fields to execute label queries and print out the results
type LabelQuery struct {
	LabelName string
	Quiet     bool
	Start     time.Time
	End       time.Time
}

// DoLabels prints out label results
func (q *LabelQuery) DoLabels(c client.Client) {
	values := q.ListLabels(c)

	for _, value := range values {
		fmt.Println(value)
	}
}

// ListLabels returns an array of label strings
func (q *LabelQuery) ListLabels(c client.Client) []string {
	var labelResponse *loghttp.LabelResponse
	var err error
	if len(q.LabelName) > 0 {
		labelResponse, err = c.ListLabelValues(q.LabelName, q.Quiet, q.Start, q.End)
	} else {
		labelResponse, err = c.ListLabelNames(q.Quiet, q.Start, q.End)
	}
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}
	return labelResponse.Data
}
