package main

import (
	"encoding/json"
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
	kingpin "gopkg.in/alecthomas/kingpin.v2"

	"github.com/grafana/logish/pkg/iter"
	"github.com/grafana/logish/pkg/logproto"
	"github.com/grafana/logish/pkg/parser"
)

var (
	app      = kingpin.New("logcli", "A command-line for logish.")
	addr     = app.Flag("addr", "Server address.").Default("https://log-us.grafana.net").Envar("GRAFANA_ADDR").String()
	username = app.Flag("username", "Username for HTTP basic auth.").Default("").Envar("GRAFANA_USERNAME").String()
	password = app.Flag("password", "Password for HTTP basic auth.").Default("").Envar("GRAFANA_PASSWORD").String()

	queryCmd  = app.Command("query", "Run a LogQL query.")
	queryStr  = queryCmd.Arg("query", "eg '{foo=\"bar\",baz=\"blip\"}'").Required().String()
	regexpStr = queryCmd.Arg("regex", "").String()
	limit     = queryCmd.Flag("limit", "Limit on number of entries to print.").Default("30").Int()
	since     = queryCmd.Flag("since", "Lookback window.").Default("1h").Duration()
	forward   = queryCmd.Flag("forward", "Scan forwards through logs.").Default("false").Bool()

	labelsCmd = app.Command("labels", "Find values for a given label.")
	labelName = labelsCmd.Arg("label", "The name of the label.").String()
)

func main() {
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {

	case queryCmd.FullCommand():
		query()

	case labelsCmd.FullCommand():
		label()
	}
}

func label() {
	var path string
	if len(*labelName) > 0 {
		path = fmt.Sprintf("/api/prom/label/%s/values", url.PathEscape(*labelName))
	} else {
		path = "/api/prom/label"
	}
	var labelResponse logproto.LabelResponse
	doRequest(path, &labelResponse)
	for _, value := range labelResponse.Values {
		fmt.Println(value)
	}
}

func query() {
	end := time.Now()
	start := end.Add(-*since)

	directionStr := "backward"
	if *forward {
		directionStr = "forward"
	}

	path := fmt.Sprintf("/api/prom/query?query=%s&limit=%d&start=%d&end=%d&direction=%s&regexp=%s",
		url.QueryEscape(*queryStr), *limit, start.Unix(), end.Unix(), directionStr, url.QueryEscape(*regexpStr))
	var queryResponse logproto.QueryResponse
	doRequest(path, &queryResponse)

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
	iter := iter.NewQueryResponseIterator(&queryResponse, d)
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

func doRequest(path string, out interface{}) {
	url := *addr + path
	fmt.Println(url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	req.SetBasicAuth(*username, *password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Error doing request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		buf, err := ioutil.ReadAll(resp.Body)
		log.Fatalf("Error response from server: %s (%v)", string(buf), err)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		log.Fatalf("Error decoding response: %v", err)
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
