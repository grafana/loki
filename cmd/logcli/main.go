package main

import (
	"log"
	"net/url"
	"os"
	"time"

	"github.com/prometheus/common/config"
	"github.com/prometheus/common/version"

	_ "github.com/grafana/loki/pkg/build"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/labelquery"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/logcli/query"

	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	app        = kingpin.New("logcli", "A command-line for loki.").Version(version.Print("logcli"))
	quiet      = app.Flag("quiet", "suppress query metadata").Default("false").Short('q').Bool()
	statistics = app.Flag("stats", "show query statistics").Default("false").Bool()
	outputMode = app.Flag("output", "specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.").Default("default").Short('o').Enum("default", "raw", "jsonl")
	timezone   = app.Flag("timezone", "Specify the timezone to use when formatting output timestamps [Local, UTC]").Default("Local").Short('z').Enum("Local", "UTC")

	queryClient = newQueryClient(app)

	queryCmd = app.Command("query", `Run a LogQL query.

The "query" command is useful for querying for log lines. The default
output of this command are log entries (a combination of timestamp,
labels, and a log line) along with various extra information about
the performed query and its results. Raw log lines (i.e., without a
label and timestamp) can be retrieved by passing the "-o raw" flag.
The extra information about the query (API URL, set of common labels,
excluded labels) can be suppressed with the --query flag.

While "query" does support metrics queries, its output contains multiple
data points between the start and end query time. This output is used to
build graphs, like what is seen in the Grafana Explore graph view. If
you are querying metrics and just want the most recent data point
(like what is seen in the Grafana Explore table view), then you should use
the "instant-query" command instead.`)
	rangeQuery = newQuery(false, queryCmd)
	tail       = queryCmd.Flag("tail", "Tail the logs").Short('t').Default("false").Bool()
	delayFor   = queryCmd.Flag("delay-for", "Delay in tailing by number of seconds to accumulate logs for re-ordering").Default("0").Int()

	instantQueryCmd = app.Command("instant-query", `Run an instant LogQL query.

The "instant-query" command is useful for evaluating a metric query for
a single point in time. This is equivalent to the Grafana Explore table
view; if you want a metrics query that is used to build a Grafana graph,
you should use the "query" command instead.

This command does not produce useful output when querying for log lines;
you should always use the "query" command when you are running log queries.`)
	instantQuery = newQuery(true, instantQueryCmd)

	labelsCmd = app.Command("labels", "Find values for a given label.")
	labelName = labelsCmd.Arg("label", "The name of the label.").HintAction(hintActionLabelNames).String()
)

func main() {
	log.SetOutput(os.Stderr)

	cmd := kingpin.MustParse(app.Parse(os.Args[1:]))

	switch cmd {
	case queryCmd.FullCommand():
		location, err := time.LoadLocation(*timezone)
		if err != nil {
			log.Fatalf("Unable to load timezone '%s': %s", *timezone, err)
		}

		outputOptions := &output.LogOutputOptions{
			Timezone: location,
			NoLabels: rangeQuery.NoLabels,
		}

		out, err := output.NewLogOutput(*outputMode, outputOptions)
		if err != nil {
			log.Fatalf("Unable to create log output: %s", err)
		}

		if *tail {
			rangeQuery.TailQuery(*delayFor, queryClient, out)
		} else {
			rangeQuery.DoQuery(queryClient, out, *statistics)
		}
	case instantQueryCmd.FullCommand():
		location, err := time.LoadLocation(*timezone)
		if err != nil {
			log.Fatalf("Unable to load timezone '%s': %s", *timezone, err)
		}

		outputOptions := &output.LogOutputOptions{
			Timezone: location,
			NoLabels: instantQuery.NoLabels,
		}

		out, err := output.NewLogOutput(*outputMode, outputOptions)
		if err != nil {
			log.Fatalf("Unable to create log output: %s", err)
		}

		instantQuery.DoQuery(queryClient, out, *statistics)
	case labelsCmd.FullCommand():
		q := newLabelQuery(*labelName, *quiet)

		q.DoLabels(queryClient)
	}
}

func hintActionLabelNames() []string {
	q := newLabelQuery("", *quiet)

	return q.ListLabels(queryClient)
}

func newQueryClient(app *kingpin.Application) *client.Client {
	client := &client.Client{
		TLSConfig: config.TLSConfig{},
	}

	// extract host
	addressAction := func(c *kingpin.ParseContext) error {
		u, err := url.Parse(client.Address)
		if err != nil {
			return err
		}
		client.TLSConfig.ServerName = u.Host
		return nil
	}

	app.Flag("addr", "Server address. Can also be set using LOKI_ADDR env var.").Default("http://localhost:3100").Envar("LOKI_ADDR").Action(addressAction).StringVar(&client.Address)
	app.Flag("username", "Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.").Default("").Envar("LOKI_USERNAME").StringVar(&client.Username)
	app.Flag("password", "Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.").Default("").Envar("LOKI_PASSWORD").StringVar(&client.Password)
	app.Flag("ca-cert", "Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.").Default("").Envar("LOKI_CA_CERT_PATH").StringVar(&client.TLSConfig.CAFile)
	app.Flag("tls-skip-verify", "Server certificate TLS skip verify.").Default("false").BoolVar(&client.TLSConfig.InsecureSkipVerify)
	app.Flag("cert", "Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.").Default("").Envar("LOKI_CLIENT_CERT_PATH").StringVar(&client.TLSConfig.CertFile)
	app.Flag("key", "Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.").Default("").Envar("LOKI_CLIENT_KEY_PATH").StringVar(&client.TLSConfig.KeyFile)
	app.Flag("org-id", "org ID header to be substituted for auth").StringVar(&client.OrgID)

	return client
}

func newLabelQuery(labelName string, quiet bool) *labelquery.LabelQuery {
	return &labelquery.LabelQuery{
		LabelName: labelName,
		Quiet:     quiet,
	}
}

func newQuery(instant bool, cmd *kingpin.CmdClause) *query.Query {
	// calculcate query range from cli params
	var now, from, to string
	var since time.Duration

	query := &query.Query{}

	// executed after all command flags are parsed
	cmd.Action(func(c *kingpin.ParseContext) error {

		if instant {
			query.SetInstant(mustParse(now, time.Now()))
		} else {
			defaultEnd := time.Now()
			defaultStart := defaultEnd.Add(-since)

			query.Start = mustParse(from, defaultStart)
			query.End = mustParse(to, defaultEnd)
		}
		query.Quiet = *quiet
		return nil
	})

	cmd.Flag("limit", "Limit on number of entries to print.").Default("30").IntVar(&query.Limit)
	if instant {
		cmd.Arg("query", "eg 'rate({foo=\"bar\"} |~ \".*error.*\" [5m])'").Required().StringVar(&query.QueryString)
		cmd.Flag("now", "Time at which to execute the instant query.").StringVar(&now)
	} else {
		cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"} |~ \".*error.*\"'").Required().StringVar(&query.QueryString)
		cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&since)
		cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&from)
		cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&to)
		cmd.Flag("step", "Query resolution step width").DurationVar(&query.Step)
	}

	cmd.Flag("forward", "Scan forwards through logs.").Default("false").BoolVar(&query.Forward)
	cmd.Flag("no-labels", "Do not print any labels").Default("false").BoolVar(&query.NoLabels)
	cmd.Flag("exclude-label", "Exclude labels given the provided key during output.").StringsVar(&query.IgnoreLabelsKey)
	cmd.Flag("include-label", "Include labels given the provided key during output.").StringsVar(&query.ShowLabelsKey)
	cmd.Flag("labels-length", "Set a fixed padding to labels").Default("0").IntVar(&query.FixedLabelsLen)
	cmd.Flag("store-config", "Execute the current query using a configured storage from a given Loki configuration file.").Default("").StringVar(&query.LocalConfig)

	return query
}

func mustParse(t string, defaultTime time.Time) time.Time {
	if t == "" {
		return defaultTime
	}

	ret, err := time.Parse(time.RFC3339Nano, t)

	if err != nil {
		log.Fatalf("Unable to parse time %v", err)
	}

	return ret
}
