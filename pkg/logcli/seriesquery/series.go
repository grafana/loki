package seriesquery

import (
	"fmt"
	"log"
	"time"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/loghttp"
)

// SeriesQuery contains all necessary fields to execute label queries and print out the results
type SeriesQuery struct {
	Matchers []string
	Start    time.Time
	End      time.Time
	Quiet    bool
}

// DoSeries prints out series results
func (q *SeriesQuery) DoSeries(c *client.Client) {
	values := q.GetSeries(c)

	for _, value := range values {
		fmt.Println(value)
	}
}

// GetSeries returns an array of label sets
func (q *SeriesQuery) GetSeries(c *client.Client) []loghttp.LabelSet {
	seriesResponse, err := c.Series(q.Matchers, q.Start, q.End, q.Quiet)
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}
	return seriesResponse.Data
}
