package index

import (
	"log"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/logcli/print"
	"github.com/grafana/loki/v3/pkg/logcli/volume"
	"github.com/grafana/loki/v3/pkg/loghttp"
)

// GetVolume executes a volume query and prints the results
func GetVolume(q *volume.Query, c client.Client, out output.LogOutput, statistics bool) {
	do(q, false, c, out, statistics)
}

// GetVolumeRange executes a volume query over a period of time and prints the results, which will
// be a collection of data points over time.
func GetVolumeRange(q *volume.Query, c client.Client, out output.LogOutput, statistics bool) {
	do(q, true, c, out, statistics)
}

func do(q *volume.Query, rangeQuery bool, c client.Client, out output.LogOutput, statistics bool) {
	resp := getVolume(q, rangeQuery, c)

	resultsPrinter := print.NewQueryResultPrinter(nil, nil, q.Quiet, 0, false, false)

	if statistics {
		resultsPrinter.PrintStats(resp.Data.Statistics)
	}

	_, _ = resultsPrinter.PrintResult(resp.Data.Result, out, nil)
}

// getVolume returns a volume result
func getVolume(q *volume.Query, rangeQuery bool, c client.Client) *loghttp.QueryResponse {
	var resp *loghttp.QueryResponse
	var err error

	if rangeQuery {
		resp, err = c.GetVolumeRange(q)
	} else {
		resp, err = c.GetVolume(q)
	}
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}

	return resp
}
