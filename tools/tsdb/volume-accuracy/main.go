package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/prometheus/common/config"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/query"
	"github.com/grafana/loki/pkg/logcli/volume"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
)

// go build ./tools/tsdb/volume-accuracy && LOKI_ADDR="https://..." LOKI_USERNAME="***" LOKI_PASSWORD="***" ./tools/tsdb/volume-accuracy/volume-accuracy
func main() {

	client := &client.DefaultClient{
		Address: os.Getenv("LOKI_ADDR"),
		//Username:  os.Getenv("LOKI_USERNAME"),
		//Password:  os.Getenv("LOKI_PASSWORD"),
		OrgID:     "29",
		TLSConfig: config.TLSConfig{},
	}

	intervals := []model.Duration{
		//model.Duration(6 * time.Hour),
		model.Duration(12 * time.Hour),
		//model.Duration(4 * 24 * time.Hour),
	}

	// Starts rounded to the second
	starts := []time.Time{
		time.Unix(1693216800, 0),
		//time.Unix(1693386000, 0),
	}

	acc := &accumulation{}

	series := []string{
		//`{cluster="dev-us-central-0"}`,
		//`{cluster="dev-us-central-0", namespace="cortex-dev-01"}`,
		//	`{cluster="dev-us-central-0", namespace="cortex-dev-01", job="infra-monitoring/eventrouter"}`,
		//	`{cluster="dev-us-central-0", namespace="cortex-dev-01", job="cortex-dev-01/store-gateway-zone-a"}`,
		//	`{cluster="dev-us-central-0", namespace="startup"}`,
		//	`{cluster="dev-us-central-0", namespace="startup", stream="stderr"}`,
		//	`{cluster="dev-us-central-0", namespace="startup", stream="stdout"}`,
		//	`{cluster="dev-us-central-0", namespace="machine-learning-cd", job="integrations/kubernetes/eventhandler"}`,
		`{cluster="dev-us-east-0"}`,
		//	`{cluster="dev-eu-west-2"}`,
	}

	for _, now := range starts {
		for _, interval := range intervals {
			for _, matchers := range series {
				err := getStreamVolume(client, acc, now, interval, matchers)
				if err != nil {
					log.Fatalf("%s", err)
				}
			}
		}
	}

	acc.Write(os.Stdout)
}

func getStreamVolume(client client.Client, acc *accumulation, now time.Time, interval model.Duration, matcherString string) error {
	matchers, err := syntax.ParseMatchers(matcherString, true)
	if err != nil {
		return fmt.Errorf("could not parse matchers: %w", err)
	}
	labels := make([]string, 0)
	for _, m := range matchers {
		labels = append(labels, m.Name)
	}

	instantQueryString := fmt.Sprintf(`sum by (%s) (bytes_over_time(%s[%s]))`, strings.Join(labels, ","), matcherString, interval)
	instantQuery := newQuery(instantQueryString, now)

	resp, err := client.Query(instantQuery.QueryString, instantQuery.Limit, instantQuery.Start, logproto.BACKWARD, false)
	if err != nil {
		return fmt.Errorf("query error: %w", err)
	}
	byteOverTimeResult := resp.Data.Result.(loghttp.Vector)

	volumeQueryString := matcherString
	volumeQuery := newVolumeQuery(volumeQueryString, now, time.Duration(interval))
	vr, err := client.GetVolume(volumeQuery)
	if err != nil {
		log.Fatalf("query error: %s", err)
	}
	volumeResult := vr.Data.Result.(loghttp.Vector)

	for i := range volumeResult {
		metric := volumeResult[i].Metric.String()
		if metric != byteOverTimeResult[i].Metric.String() {
			return fmt.Errorf("metrics do not match: %s v %s", metric, byteOverTimeResult[i].Metric.String())
		}
		acc.Add(metric, now, interval, float64(byteOverTimeResult[i].Value), float64(volumeResult[i].Value))
	}

	return nil
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

type accumulation struct {
	metric        []string
	start         []time.Time
	interval      []model.Duration
	bytesOverTime []float64
	volume        []float64
}

func (a *accumulation) Add(metric string, start time.Time, interval model.Duration, bytesOverTime, volume float64) {
	a.metric = append(a.metric, metric)
	a.start = append(a.start, start)
	a.interval = append(a.interval, interval)
	a.bytesOverTime = append(a.bytesOverTime, bytesOverTime)
	a.volume = append(a.volume, volume)
}

func (a *accumulation) Write(w io.Writer) {
	// Header
	fmt.Fprintf(w, "%-40s\t%40s\t%10s\t%15s\t%15s\t%15s\n", "stream", "start", "interval", "bytes over time", "volume", "relative error")

	// Content
	for i := range a.metric {
		expected := a.bytesOverTime[i]
		actual := a.volume[i]
		relativeError := math.Abs(1 - actual/expected)

		fmt.Fprintf(w, "%-40s\t%40s\t%10s\t%15.f\t%15.f\t%15f\n", a.metric[i], a.start[i], a.interval[i], expected, actual, relativeError)
	}
}
