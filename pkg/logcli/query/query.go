package query

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"

	"github.com/fatih/color"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/logproto"
)

// Query contains all necessary fields to execute instant and range queries and print the results.
type Query struct {
	QueryString     string
	Start           time.Time
	End             time.Time
	Limit           int
	Forward         bool
	Quiet           bool
	NoLabels        bool
	IgnoreLabelsKey []string
	ShowLabelsKey   []string
	FixedLabelsLen  int
}

// DoQuery executes the query and prints out the results
func (q *Query) DoQuery(c *client.Client, out output.LogOutput) {
	d := q.resultsDirection()

	var resp *client.QueryResult
	var err error

	if q.isInstant() {
		resp, err = c.Query(q.QueryString, q.Limit, q.Start, d, q.Quiet)
	} else {
		resp, err = c.QueryRange(q.QueryString, q.Limit, q.Start, q.End, d, q.Quiet)
	}

	if err != nil {
		log.Fatalf("Query failed: %+v", err)
	}

	switch resp.ResultType {
	case logql.ValueTypeStreams:
		streams := resp.Result.(logql.Streams)
		q.printStream(streams, out)
	case promql.ValueTypeMatrix:
		matrix := resp.Result.(model.Matrix)
		q.printMatrix(matrix)
	case promql.ValueTypeVector:
		vector := resp.Result.(model.Vector)
		q.printVector(vector)
	default:
		log.Fatalf("Unable to print unsupported type: %v", resp.ResultType)
	}
}

// SetInstant makes the Query an instant type
func (q *Query) SetInstant(time time.Time) {
	q.Start = time
	q.End = time
}

func (q *Query) isInstant() bool {
	return q.Start == q.End
}

func (q *Query) printStream(streams logql.Streams, out output.LogOutput) {
	cache, lss := parseLabels(streams)

	labelsCache := func(labels string) labels.Labels {
		return cache[labels]
	}

	common := commonLabels(lss)

	// Remove the labels we want to show from common
	if len(q.ShowLabelsKey) > 0 {
		common = common.MatchLabels(false, q.ShowLabelsKey...)
	}

	if len(common) > 0 && !q.Quiet {
		log.Println("Common labels:", color.RedString(common.String()))
	}

	if len(q.IgnoreLabelsKey) > 0 && !q.Quiet {
		log.Println("Ignoring labels key:", color.RedString(strings.Join(q.IgnoreLabelsKey, ",")))
	}

	// Remove ignored and common labels from the cached labels and
	// calculate the max labels length
	maxLabelsLen := q.FixedLabelsLen
	for key, ls := range cache {
		// Remove common labels
		ls = subtract(ls, common)

		// Remove ignored labels
		if len(q.IgnoreLabelsKey) > 0 {
			ls = ls.MatchLabels(false, q.IgnoreLabelsKey...)
		}

		// Update cached labels
		cache[key] = ls

		// Update max labels length
		len := len(ls.String())
		if maxLabelsLen < len {
			maxLabelsLen = len
		}
	}

	d := q.resultsDirection()
	i := iter.NewStreamsIterator(streams, d)

	for i.Next() {
		ls := labelsCache(i.Labels())
		fmt.Println(out.Format(i.Entry().Timestamp, &ls, maxLabelsLen, i.Entry().Line))
	}

	if err := i.Error(); err != nil {
		log.Fatalf("Error from iterator: %v", err)
	}
}

func (q *Query) printMatrix(matrix model.Matrix) {
	// yes we are effectively unmarshalling and then immediately marshalling this object back to json.  we are doing this b/c
	// it gives us more flexibility with regard to output types in the future.  initially we are supporting just formatted json but eventually
	// we might add output options such as render to an image file on disk
	bytes, err := json.MarshalIndent(matrix, "", "  ")

	if err != nil {
		log.Fatalf("Error marshalling matrix: %v", err)
	}

	fmt.Print(string(bytes))
}

func (q *Query) printVector(vector model.Vector) {
	bytes, err := json.MarshalIndent(vector, "", "  ")

	if err != nil {
		log.Fatalf("Error marshalling vector: %v", err)
	}

	fmt.Print(string(bytes))
}

func (q *Query) resultsDirection() logproto.Direction {
	if q.Forward {
		return logproto.FORWARD
	}
	return logproto.BACKWARD
}
