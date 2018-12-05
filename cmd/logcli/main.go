package main

import (
	"os"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	app      = kingpin.New("logcli", "A command-line for loki.")
	addr     = app.Flag("addr", "Server address.").Default("https://log-us.grafana.net").Envar("GRAFANA_ADDR").String()
	username = app.Flag("username", "Username for HTTP basic auth.").Default("").Envar("GRAFANA_USERNAME").String()
	password = app.Flag("password", "Password for HTTP basic auth.").Default("").Envar("GRAFANA_PASSWORD").String()

	queryCmd  = app.Command("query", "Run a LogQL query.")
	queryStr  = queryCmd.Arg("query", "eg '{foo=\"bar\",baz=\"blip\"}'").Required().String()
	regexpStr = queryCmd.Arg("regex", "").String()
	limit     = queryCmd.Flag("limit", "Limit on number of entries to print.").Default("30").Int()
	since     = queryCmd.Flag("since", "Lookback window.").Default("1h").Duration()
	forward   = queryCmd.Flag("forward", "Scan forwards through logs.").Default("false").Bool()
	tail      = queryCmd.Flag("tail", "Tail the logs").Short('t').Default("false").Bool()

	labelsCmd = app.Command("labels", "Find values for a given label.")
	labelName = labelsCmd.Arg("label", "The name of the label.").HintAction(listLabels).String()
)

func main() {
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case queryCmd.FullCommand():
		doQuery()
	case labelsCmd.FullCommand():
		doLabels()
	}
}
