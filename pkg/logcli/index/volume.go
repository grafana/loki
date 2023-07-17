package index

import (
	"log"
	"time"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/logcli/print"
	"github.com/grafana/loki/pkg/loghttp"
)

type VolumeQuery struct {
	QueryString string
	Start       time.Time
	End         time.Time
	Step        time.Duration
	Quiet       bool
	Limit       int
}

// DoVolume executes a volume query and prints the results
func (q *VolumeQuery) DoVolume(c client.Client, out output.LogOutput, statistics bool) {
	resp := q.Volume(c)

	resultsPrinter := print.NewQueryResultPrinter(nil, nil, q.Quiet, 0, false)

	if statistics {
		resultsPrinter.PrintStats(resp.Data.Statistics)
	}

	_, _ = resultsPrinter.PrintResult(resp.Data.Result, out, nil)
}

// Volume returns a volume result
func (q *VolumeQuery) Volume(c client.Client) *loghttp.QueryResponse {
	var resp *loghttp.QueryResponse
	var err error

	resp, err = c.GetVolume(q.QueryString, q.Start, q.End, q.Step, q.Limit, q.Quiet)
	if err != nil {
		log.Fatalf("Error doing request: %+v", err)
	}

	return resp
}
