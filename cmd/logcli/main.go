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

	tlsCACertPath        = app.Flag("ca-cert", "Path to the server Certificate Authority.").Default("").Envar("LOKI_CA_CERT_PATH").String()
	tlsSkipVerify        = app.Flag("tls-skip-verify", "Server certificate TLS skip verify.").Default("false").Bool()
	tlsClientCertPath    = app.Flag("cert", "Path to the client certificate.").Default("").Envar("LOKI_CLIENT_CERT_PATH").String()
	tlsClientCertKeyPath = app.Flag("key", "Path to the client certificate key.").Default("").Envar("LOKI_CLIENT_KEY_PATH").String()
	tlsClientCertKeyPass = app.Flag("key-pass", "Client certificate key password.").Default("").Envar("LOKI_CLIENT_KEY_PASS").String()

	queryCmd  = app.Command("query", "Run a LogQL query.")
	queryStr  = queryCmd.Arg("query", "eg '{foo=\"bar\",baz=\"blip\"}'").Required().String()
	regexpStr = queryCmd.Arg("regex", "").String()
	limit     = queryCmd.Flag("limit", "Limit on number of entries to print.").Default("30").Int()
	since     = queryCmd.Flag("since", "Lookback window.").Default("1h").Duration()
	forward   = queryCmd.Flag("forward", "Scan forwards through logs.").Default("false").Bool()
	tail      = queryCmd.Flag("tail", "Tail the logs").Short('t').Default("false").Bool()
	noLabels  = queryCmd.Flag("no-labels", "Do not print labels").Default("false").Bool()

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
