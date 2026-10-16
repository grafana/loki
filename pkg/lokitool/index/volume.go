package index

import (
	"github.com/grafana/loki/v3/pkg/lokitool/client"
	"github.com/grafana/loki/v3/pkg/lokitool/output"
	"github.com/grafana/loki/v3/pkg/lokitool/print"
	"github.com/grafana/loki/v3/pkg/lokitool/volume"
)

// GetVolume executes a volume query and prints the results.
func GetVolume(q *volume.Query, c client.QueryClient, out output.LogOutput, statistics bool) error {
	resp, err := c.GetVolume(q)
	if err != nil {
		return err
	}

	resultsPrinter := print.NewQueryResultPrinter(nil, nil, 0, false, false)

	if statistics {
		resultsPrinter.PrintStats(resp.Data.Statistics)
	}

	_, _, err = resultsPrinter.PrintResult(resp.Data.Result, out, nil)
	return err
}
