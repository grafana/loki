package main

import (
	"log"
	"os"
	"time"

	"github.com/grafana/loki/pkg/logcli/output"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	app        = kingpin.New("logcli", "A command-line for loki.")
	quiet      = app.Flag("quiet", "suppress everything but log lines").Default("false").Short('q').Bool()
	outputMode = app.Flag("output", "specify output mode [default, raw, jsonl]").Default("default").Short('o').Enum("default", "raw", "jsonl")
	timezone   = app.Flag("timezone", "Specify the timezone to use when formatting output timestamps [Local, UTC]").Default("Local").Short('z').Enum("Local", "UTC")

	addr = app.Flag("addr", "Server address.").Default("https://logs-us-west1.grafana.net").Envar("GRAFANA_ADDR").String()

	username = app.Flag("username", "Username for HTTP basic auth.").Default("").Envar("GRAFANA_USERNAME").String()
	password = app.Flag("password", "Password for HTTP basic auth.").Default("").Envar("GRAFANA_PASSWORD").String()

	tlsCACertPath        = app.Flag("ca-cert", "Path to the server Certificate Authority.").Default("").Envar("LOKI_CA_CERT_PATH").String()
	tlsSkipVerify        = app.Flag("tls-skip-verify", "Server certificate TLS skip verify.").Default("false").Bool()
	tlsClientCertPath    = app.Flag("cert", "Path to the client certificate.").Default("").Envar("LOKI_CLIENT_CERT_PATH").String()
	tlsClientCertKeyPath = app.Flag("key", "Path to the client certificate key.").Default("").Envar("LOKI_CLIENT_KEY_PATH").String()

	queryCmd        = app.Command("query", "Run a LogQL query.")
	queryStr        = queryCmd.Arg("query", "eg '{foo=\"bar\",baz=\"blip\"}'").Required().String()
	limit           = queryCmd.Flag("limit", "Limit on number of entries to print.").Default("30").Int()
	since           = queryCmd.Flag("since", "Lookback window.").Default("1h").Duration()
	from            = queryCmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").String()
	to              = queryCmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").String()
	forward         = queryCmd.Flag("forward", "Scan forwards through logs.").Default("false").Bool()
	tail            = queryCmd.Flag("tail", "Tail the logs").Short('t').Default("false").Bool()
	delayFor        = queryCmd.Flag("delay-for", "Delay in tailing by number of seconds to accumulate logs for re-ordering").Default("0").Int()
	noLabels        = queryCmd.Flag("no-labels", "Do not print any labels").Default("false").Bool()
	ignoreLabelsKey = queryCmd.Flag("exclude-label", "Exclude labels given the provided key during output.").Strings()
	showLabelsKey   = queryCmd.Flag("include-label", "Include labels given the provided key during output.").Strings()
	fixedLabelsLen  = queryCmd.Flag("labels-length", "Set a fixed padding to labels").Default("0").Int()
	labelsCmd       = app.Command("labels", "Find values for a given label.")
	labelName       = labelsCmd.Arg("label", "The name of the label.").HintAction(listLabels).String()
)

func main() {
	log.SetOutput(os.Stderr)

	cmd := kingpin.MustParse(app.Parse(os.Args[1:]))

	if *addr == "" {
		log.Fatalln("Server address cannot be empty")
	}

	switch cmd {
	case queryCmd.FullCommand():
		location, err := time.LoadLocation(*timezone)
		if err != nil {
			log.Fatalf("Unable to load timezone '%s': %s", *timezone, err)
		}

		outputOptions := &output.LogOutputOptions{
			Timezone: location,
			NoLabels: *noLabels,
		}

		out, err := output.NewLogOutput(*outputMode, outputOptions)
		if err != nil {
			log.Fatalf("Unable to create log output: %s", err)
		}

		doQuery(out)
	case labelsCmd.FullCommand():
		doLabels()
	}
}
