package index

import (
	"fmt"
	"time"

	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type StatsQuery struct {
	QueryString string
	Start       time.Time
	End         time.Time
	Quiet       bool
}

// DoStats executes the stats query and prints the results
func (q *StatsQuery) DoStats(c client.Client) error {
	stats, err := q.Stats(c)
	if err != nil {
		return err
	}
	kvs := stats.LoggingKeyValues()

	fmt.Print("{\n")
	for i := 0; i < len(kvs)-1; i = i + 2 {
		k := kvs[i].(string)
		v := kvs[i+1]
		if k == "bytes" {
			fmt.Printf("  %s: %s\n", color.BlueString(k), v)
			continue
		}

		fmt.Printf("  %s: %d\n", color.BlueString(k), v)
	}
	fmt.Print("}\n")
	return nil
}

// Stats returns an index stats response
func (q *StatsQuery) Stats(c client.Client) (*logproto.IndexStatsResponse, error) {
	var statsResponse *logproto.IndexStatsResponse
	var err error

	statsResponse, err = c.GetStats(q.QueryString, q.Start, q.End, q.Quiet)

	if err != nil {
		return nil, err
	}
	return statsResponse, nil
}
