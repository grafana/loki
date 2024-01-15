package main

import (
	"log"
	"os"
	"time"

	"github.com/prometheus/common/config"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/index"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/logcli/query"
	"github.com/grafana/loki/pkg/logcli/volume"
)

// go build ./tools/tsdb/volume-accuracy && LOKI_ADDR="https://..." LOKI_USERNAME="***" LOKI_PASSWORD="***" ./tools/tsdb/volume-accuracy/volume-accuracy
func main() {

	client := &client.DefaultClient{
		Address:   os.Getenv("LOKI_ADDR"),
		Username:  os.Getenv("LOKI_USERNAME"),
		Password:  os.Getenv("LOKI_PASSWORD"),
		TLSConfig: config.TLSConfig{},
	}

	// Now rounded to the second
	now := time.Unix(time.Now().Unix(), 0).Add(-24 * time.Hour)

	instantQueryString := `bytes_over_time({job="systemd-journal"}[1h])`
	instantQuery := newQuery(instantQueryString, now)

	// TODO: use custom accumulator to store results in table
	outputOptions := &output.LogOutputOptions{
		Timezone: time.UTC,
	}

	out, err := output.NewLogOutput(os.Stdout, "default", outputOptions)
	if err != nil {
		log.Fatalf("Unable to create log output: %s", err)
	}

	instantQuery.DoQuery(client, out, false)

	volumeQueryString := `{job="systemd-journal"}`
	volumeQuery := newVolumeQuery(volumeQueryString, now, time.Hour)
	index.GetVolume(volumeQuery, client, out, false)
}

func newQuery(queryString string, now time.Time) *query.Query {

	q := &query.Query{}

	q.SetInstant(now)
	q.QueryString = queryString
	q.Limit = 10000

	return q
}

func newVolumeQuery(queryString string, now time.Time, interval time.Duration) *volume.Query {
	q := &volume.Query{}

	q.QueryString = queryString

	q.End = now
	q.Start = q.End.Add(-interval)

	return q
}
