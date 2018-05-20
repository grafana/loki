package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/grafana/logish/pkg/logproto"
	"github.com/grafana/logish/pkg/parser"
	"github.com/grafana/logish/pkg/querier"
)

var defaultAddr = "https://log-us.grafana.net/api/prom/query"

func main() {
	var (
		limit   = flag.Int("limit", 30, "Limit on number of entries to print.")
		since   = flag.Duration("since", 1*time.Hour, "Lookback window.")
		forward = flag.Bool("forward", false, "Scan forwards through logs.")
	)

	flag.Parse()
	args := flag.Args()
	if len(args) < 1 || len(args) > 2 {
		log.Fatalf("usage: %s '{foo=\"bar\",baz=\"blip\"}''", os.Args[0])
	}

	query := args[0]
	regexp := ""
	if len(args) > 1 {
		regexp = args[1]
	}
	addr := os.Getenv("GRAFANA_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	end := time.Now()
	start := end.Add(-*since)
	username := os.Getenv("GRAFANA_USERNAME")
	password := os.Getenv("GRAFANA_PASSWORD")
	directionStr := "backward"
	if *forward {
		directionStr = "forward"
	}
	url := fmt.Sprintf("%s?query=%s&limit=%d&start=%d&end=%d&direction=%s&regexp=%s",
		addr, url.QueryEscape(query), *limit, start.Unix(), end.Unix(), directionStr, regexp)
	fmt.Println(url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	req.SetBasicAuth(username, password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Error doing request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		buf, err := ioutil.ReadAll(resp.Body)
		log.Fatalf("Error response from server: %s (%v)", string(buf), err)
	}

	var queryResponse logproto.QueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&queryResponse); err != nil {
		log.Fatalf("Error decoding response: %v", err)
	}

	if len(queryResponse.Streams) == 0 {
		return
	}

	labelsCache := make(map[string]labels.Labels, len(queryResponse.Streams))
	lss := make([]labels.Labels, 0, len(queryResponse.Streams))
	for _, stream := range queryResponse.Streams {
		ls, err := parser.Labels(stream.Labels)
		if err != nil {
			log.Fatalf("Error parsing labels: %v", err)
		}
		labelsCache[stream.Labels] = ls
		lss = append(lss, ls)
	}

	commonLabels, err := commonLabels(lss)
	if err != nil {
		log.Fatalf("Error parsing labels: %v", err)
	}

	if len(commonLabels) > 0 {
		fmt.Println("Common labels:", color.RedString(commonLabels.String()))
	}

	maxLabelsLen := 0
	for _, ls := range lss {
		ls = subtract(commonLabels, ls)
		len := len(ls.String())
		if maxLabelsLen < len {
			maxLabelsLen = len
		}
	}

	d := logproto.BACKWARD
	if *forward {
		d = logproto.FORWARD
	}
	iter := querier.NewQueryResponseIterator(&queryResponse, d)
	for iter.Next() {
		ls := labelsCache[iter.Labels()]
		ls = subtract(commonLabels, ls)
		labels := ls.String()
		labels += strings.Repeat(" ", maxLabelsLen-len(labels))

		fmt.Println(
			color.BlueString(iter.Entry().Timestamp.Format(time.RFC3339)),
			color.RedString(labels),
			strings.TrimSpace(iter.Entry().Line),
		)
	}

	if err := iter.Error(); err != nil {
		log.Fatalf("Error from iterator: %v", err)
	}
}

func commonLabels(lss []labels.Labels) (labels.Labels, error) {
	result := lss[0]
	for i := 1; i < len(lss); i++ {
		result = intersect(result, lss[i])
	}
	return result, nil
}

func intersect(a, b labels.Labels) labels.Labels {
	var result labels.Labels
	for i, j := 0, 0; i < len(a) && j < len(b); {
		k := strings.Compare(a[i].Name, b[j].Name)
		switch {
		case k == 0:
			if a[i].Value == b[j].Value {
				result = append(result, a[i])
			}
			i++
			j++
		case k < 0:
			i++
		case k > 0:
			j++
		}
	}
	return result
}

// substract a from b
func subtract(a, b labels.Labels) labels.Labels {
	var result labels.Labels
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		k := strings.Compare(a[i].Name, b[j].Name)
		if k != 0 || a[i].Value != b[j].Value {
			result = append(result, b[j])
		}
		switch {
		case k == 0:
			i++
			j++
		case k < 0:
			i++
		case k > 0:
			j++
		}
	}
	for ; j < len(b); j++ {
		result = append(result, b[j])
	}
	return result
}
