package main

import (
	"log"
	"os"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	app = kingpin.New("logcli", "A command-line for loki.")

	addr     = app.Flag("addr", "Server address.").Default("https://logs-us-west1.grafana.net").Envar("GRAFANA_ADDR").String()
	username = app.Flag("username", "Username for HTTP basic auth.").Default("").Envar("GRAFANA_USERNAME").String()
	password = app.Flag("password", "Password for HTTP basic auth.").Default("").Envar("GRAFANA_PASSWORD").String()

	queryCmd        = app.Command("query", "Run a LogQL query.")
	queryStr        = queryCmd.Arg("query", "eg '{foo=\"bar\",baz=\"blip\"}'").Required().String()
	regexpStr       = queryCmd.Arg("regex", "").String()
	limit           = queryCmd.Flag("limit", "Limit on number of entries to print.").Default("30").Int()
	since           = queryCmd.Flag("since", "Lookback window.").Default("1h").Duration()
	forward         = queryCmd.Flag("forward", "Scan forwards through logs.").Default("false").Bool()
	tail            = queryCmd.Flag("tail", "Tail the logs").Short('t').Default("false").Bool()
	noLabels        = queryCmd.Flag("no-labels", "Do not print any labels").Default("false").Bool()
	ignoreLabelsKey = queryCmd.Flag("no-label", "Do not print labels given the provided key").Strings()
	showLabelsKey   = queryCmd.Flag("label", "Do print labels given the provided key").Strings()

	labelsCmd = app.Command("labels", "Find values for a given label.")
	labelName = labelsCmd.Arg("label", "The name of the label.").HintAction(listLabels).String()
)

func main() {
	log.SetOutput(os.Stderr)

	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case queryCmd.FullCommand():
		if *addr == "" {
			log.Fatalln("Server address cannot be empty")
		}
		doQuery()
	case labelsCmd.FullCommand():
		doLabels()
	}
}
