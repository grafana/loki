package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/grafana/logish/pkg/logproto"
	"github.com/weaveworks/cortex/pkg/util"
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

	req, err := http.NewRequest("GET", addr+"?"+url.QueryEscape(query), nil)
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	req.SetBasicAuth(username, password)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("Error doing request: %v", err)
	}
	defer resp.Body.Close()

	var queryResponse logproto.QueryResponse
	if bs, err := util.ParseProtoReader(context.Background(), resp.Body, &queryResponse, util.RawSnappy); err != nil {
		log.Printf("Error decoding response: %v", err)
		log.Fatal(string(bs))
	}

	fmt.Println(queryResponse.String())
}
