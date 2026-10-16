package labelquery

import (
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/lokitool/client"
)

// LabelQuery contains all necessary fields to execute label queries and print out the results
type LabelQuery struct {
	LabelName string
	Start     time.Time
	End       time.Time
}

// DoLabels prints out label results
func (q *LabelQuery) DoLabels(c client.QueryClient) error {
	values, err := q.ListLabels(c)
	if err != nil {
		return err
	}
	for _, value := range values {
		fmt.Println(value)
	}
	return nil
}

// ListLabels returns an array of label strings
func (q *LabelQuery) ListLabels(c client.QueryClient) ([]string, error) {
	var labelResponse *loghttp.LabelResponse
	var err error
	if len(q.LabelName) > 0 {
		labelResponse, err = c.ListLabelValues(q.LabelName, q.Start, q.End)
	} else {
		labelResponse, err = c.ListLabelNames(q.Start, q.End)
	}
	if err != nil {
		return nil, err
	}
	return labelResponse.Data, nil
}
