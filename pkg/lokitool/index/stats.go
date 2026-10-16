package index

import (
	"fmt"
	"time"

	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/lokitool/client"
)

// StatsQuery contains the parameters of an index stats query.
type StatsQuery struct {
	QueryString string
	Start       time.Time
	End         time.Time
}

// DoStats executes the stats query and prints the results
func (q *StatsQuery) DoStats(c client.QueryClient) error {
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
func (q *StatsQuery) Stats(c client.QueryClient) (*logproto.IndexStatsResponse, error) {
	var statsResponse *logproto.IndexStatsResponse
	var err error

	statsResponse, err = c.GetStats(q.QueryString, q.Start, q.End)

	if err != nil {
		return nil, err
	}
	return statsResponse, nil
}
