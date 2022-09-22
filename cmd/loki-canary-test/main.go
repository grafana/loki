package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/grafana/loki/cmd/loki-canary-test/canary"
	"github.com/grafana/loki/cmd/loki-canary-test/util"
)

func main() {
	addr := flag.String("address", "", "The prometheus address to query for metrics")
	retries := flag.Int("retires", 3, "Number of times to retry query")
	delay := flag.Duration("delay", 30*time.Second, "Delay between retries")
	printVersion := flag.Bool("version", false, "Print build version information")
	printHelp := flag.Bool("help", false, "Print this help message")

	flag.Parse()

	if *printVersion {
		fmt.Println(util.PrintVersion("loki-canary-test"))
		os.Exit(0)
	}

	if *printHelp {
		flag.Usage()
		os.Exit(0)
	}

	if *addr == "" {
		err := fmt.Errorf("must specify a Prometheus address with -address")
		util.PrintError(err)
		os.Exit(1)
	}

	client, err := canary.DefaultCanaryClient(*addr, *delay)
	if err != nil {
		util.PrintError(err)
		os.Exit(1)
	}

	err = client.Run(*retries)
	if err != nil {
		util.PrintError(err)
		os.Exit(1)
	}

	util.PrintSuccess("Success!")
}
