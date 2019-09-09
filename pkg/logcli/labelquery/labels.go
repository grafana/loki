package labelquery

import (
	"fmt"
	"log"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logproto"
)

type LabelQuery struct {
	LabelName string
	Quiet     bool
}

func (q *LabelQuery) DoLabels(c *client.Client) {
	var labelResponse *logproto.LabelResponse
	var err error
	if len(q.LabelName) > 0 {
		labelResponse, err = c.ListLabelValues(q.LabelName, q.Quiet)
	} else {
		labelResponse, err = c.ListLabelNames(q.Quiet)
	}
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}
	for _, value := range labelResponse.Values {
		fmt.Println(value)
	}
}

func (q *LabelQuery) ListLabels(c *client.Client) []string {
	labelResponse, err := c.ListLabelNames(q.Quiet)
	if err != nil {
		log.Fatalf("Error fetching labels: %+v", err)
	}
	return labelResponse.Values
}
