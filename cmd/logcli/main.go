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

	"github.com/grafana/logish/pkg/logproto"
	"github.com/grafana/logish/pkg/querier"
)

var defaultAddr = "https://log-us.grafana.net/api/prom/query"

func main() {
	if len(os.Args) != 2 {
		log.Fatalf("usage: %s foo=bar,baz=blip", os.Args[0])
	}

	query := os.Args[1]
	addr := os.Getenv("GRAFANA_ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	username := os.Getenv("GRAFANA_USERNAME")
	password := os.Getenv("GRAFANA_PASSWORD")

	req, err := http.NewRequest("GET", addr+"?query="+url.QueryEscape(query), nil)
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

	iter := querier.NewQueryResponseIterator(&queryResponse)
	for iter.Next() {
		fmt.Println(
			color.BlueString(iter.Entry().Timestamp.Format(time.RFC822)),
			color.RedString(iter.Labels()),
			strings.TrimSpace(iter.Entry().Line),
		)
	}

	if err := iter.Error(); err != nil {
		log.Fatalf("Error from iterator: %v", err)
	}
}
