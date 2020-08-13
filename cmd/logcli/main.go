package main

import (
	"log"
	"net/url"
	"os"
	"runtime/pprof"
	"time"

	"github.com/prometheus/common/config"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"

	_ "github.com/grafana/loki/pkg/build"
	"github.com/grafana/loki/pkg/logcli/client"
	"github.com/grafana/loki/pkg/logcli/labelquery"
	"github.com/grafana/loki/pkg/logcli/output"
	"github.com/grafana/loki/pkg/logcli/query"
	"github.com/grafana/loki/pkg/logcli/seriesquery"
)

var (
	app        = kingpin.New("logcli", "A command-line for loki.").Version(version.Print("logcli"))
	quiet      = app.Flag("quiet", "Suppress query metadata").Default("false").Short('q').Bool()
	statistics = app.Flag("stats", "Show query statistics").Default("false").Bool()
	outputMode = app.Flag("output", "Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.").Default("default").Short('o').Enum("default", "raw", "jsonl")
	timezone   = app.Flag("timezone", "Specify the timezone to use when formatting output timestamps [Local, UTC]").Default("Local").Short('z').Enum("Local", "UTC")
	cpuProfile = app.Flag("cpuprofile", "Specify the location for writing a CPU profile.").Default("").String()
	memProfile = app.Flag("memprofile", "Specify the location for writing a memory profile.").Default("").String()

	queryClient = newQueryClient(app)

	queryCmd = app.Command("query", `Run a LogQL query.

The "query" command is useful for querying for logs. Logs can be
returned in a few output modes:

	raw: log line
	default: log timestamp + log labels + log line
	jsonl: JSON response from Loki API of log line

The output of the log can be specified with the "-o" flag, for
example, "-o raw" for the raw output format.

The "query" command will output extra information about the query
and its results, such as the API URL, set of common labels, and set
of excluded labels. This extra information can be suppressed with the
--quiet flag.

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
you should always use the "query" command when you are running log queries.

For more information about log queries and metric queries, refer to the
LogQL documentation:

https://github.com/grafana/loki/blob/master/docs/logql.md`)
	instantQuery = newQuery(true, instantQueryCmd)

	labelsCmd   = app.Command("labels", "Find values for a given label.")
	labelsQuery = newLabelQuery(labelsCmd)

	seriesCmd = app.Command("series", `Run series query.

The "series" command will take the provided label matcher 
and return all the log streams found in the time window.

It is possible to send an empty label matcher '{}' to return all streams.

Use the --analyze-labels flag to get a summary of the labels found in all streams.
This is helpful to find high cardinality labels. 
`)
	seriesQuery = newSeriesQuery(seriesCmd)
)

func main() {
	log.SetOutput(os.Stderr)

	cmd := kingpin.MustParse(app.Parse(os.Args[1:]))

	if cpuProfile != nil && *cpuProfile != "" {
		cpuFile, err := os.Create(*cpuProfile)
		if err != nil {
			log.Fatal("could not create CPU profile: ", err)
		}
		defer cpuFile.Close()
		if err := pprof.StartCPUProfile(cpuFile); err != nil {
			log.Fatal("could not start CPU profile: ", err)
		}
		defer pprof.StopCPUProfile()
	}

	if memProfile != nil && *memProfile != "" {
		memFile, err := os.Create(*memProfile)
		if err != nil {
			log.Fatal("could not create memory profile: ", err)
		}
		defer memFile.Close()
		defer func() {
			if err := pprof.WriteHeapProfile(memFile); err != nil {
				log.Fatal("could not write memory profile: ", err)
			}
		}()
	}

	switch cmd {
	case queryCmd.FullCommand():
		location, err := time.LoadLocation(*timezone)
		if err != nil {
			log.Fatalf("Unable to load timezone '%s': %s", *timezone, err)
		}

		outputOptions := &output.LogOutputOptions{
			Timezone:      location,
			NoLabels:      rangeQuery.NoLabels,
			ColoredOutput: rangeQuery.ColoredOutput,
		}

		out, err := output.NewLogOutput(os.Stdout, *outputMode, outputOptions)
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
			Timezone:      location,
			NoLabels:      instantQuery.NoLabels,
			ColoredOutput: instantQuery.ColoredOutput,
		}

		out, err := output.NewLogOutput(os.Stdout, *outputMode, outputOptions)
		if err != nil {
			log.Fatalf("Unable to create log output: %s", err)
		}

		instantQuery.DoQuery(queryClient, out, *statistics)
	case labelsCmd.FullCommand():
		labelsQuery.DoLabels(queryClient)
	case seriesCmd.FullCommand():
		seriesQuery.DoSeries(queryClient)
	}
}

func newQueryClient(app *kingpin.Application) client.Client {

	client := &client.DefaultClient{
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
	app.Flag("tls-skip-verify", "Server certificate TLS skip verify.").Default("false").Envar("LOKI_TLS_SKIP_VERIFY").BoolVar(&client.TLSConfig.InsecureSkipVerify)
	app.Flag("cert", "Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.").Default("").Envar("LOKI_CLIENT_CERT_PATH").StringVar(&client.TLSConfig.CertFile)
	app.Flag("key", "Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.").Default("").Envar("LOKI_CLIENT_KEY_PATH").StringVar(&client.TLSConfig.KeyFile)
	app.Flag("org-id", "adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing an auth gateway.").Default("").Envar("LOKI_ORG_ID").StringVar(&client.OrgID)

	return client
}

func newLabelQuery(cmd *kingpin.CmdClause) *labelquery.LabelQuery {
	var labelName, from, to string
	var since time.Duration

	q := &labelquery.LabelQuery{}

	// executed after all command flags are parsed
	cmd.Action(func(c *kingpin.ParseContext) error {

		defaultEnd := time.Now()
		defaultStart := defaultEnd.Add(-since)

		q.Start = mustParse(from, defaultStart)
		q.End = mustParse(to, defaultEnd)
		q.LabelName = labelName
		q.Quiet = *quiet
		return nil
	})

	cmd.Arg("label", "The name of the label.").Default("").StringVar(&labelName)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&since)
	cmd.Flag("from", "Start looking for labels at this absolute time (inclusive)").StringVar(&from)
	cmd.Flag("to", "Stop looking for labels at this absolute time (exclusive)").StringVar(&to)

	return q
}

func newSeriesQuery(cmd *kingpin.CmdClause) *seriesquery.SeriesQuery {
	// calculate series range from cli params
	var from, to string
	var since time.Duration

	q := &seriesquery.SeriesQuery{}

	// executed after all command flags are parsed
	cmd.Action(func(c *kingpin.ParseContext) error {

		defaultEnd := time.Now()
		defaultStart := defaultEnd.Add(-since)

		q.Start = mustParse(from, defaultStart)
		q.End = mustParse(to, defaultEnd)
		q.Quiet = *quiet
		return nil
	})

	cmd.Arg("matcher", "eg '{foo=\"bar\",baz=~\".*blip\"}'").Required().StringVar(&q.Matcher)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&to)
	cmd.Flag("analyze-labels", "Printout a summary of labels including count of label value combinations, useful for debugging high cardinality series").BoolVar(&q.AnalyzeLabels)

	return q
}

func newQuery(instant bool, cmd *kingpin.CmdClause) *query.Query {
	// calculate query range from cli params
	var now, from, to string
	var since time.Duration

	q := &query.Query{}

	// executed after all command flags are parsed
	cmd.Action(func(c *kingpin.ParseContext) error {

		if instant {
			q.SetInstant(mustParse(now, time.Now()))
		} else {
			defaultEnd := time.Now()
			defaultStart := defaultEnd.Add(-since)

			q.Start = mustParse(from, defaultStart)
			q.End = mustParse(to, defaultEnd)
		}
		q.Quiet = *quiet
		return nil
	})

	cmd.Flag("limit", "Limit on number of entries to print.").Default("30").IntVar(&q.Limit)
	if instant {
		cmd.Arg("query", "eg 'rate({foo=\"bar\"} |~ \".*error.*\" [5m])'").Required().StringVar(&q.QueryString)
		cmd.Flag("now", "Time at which to execute the instant query.").StringVar(&now)
	} else {
		cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"} |~ \".*error.*\"'").Required().StringVar(&q.QueryString)
		cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&since)
		cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&from)
		cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&to)
		cmd.Flag("step", "Query resolution step width, for metric queries. Evaluate the query at the specified step over the time range.").DurationVar(&q.Step)
		cmd.Flag("interval", "Query interval, for log queries. Return entries at the specified interval, ignoring those between. **This parameter is experimental, please see Issue 1779**").DurationVar(&q.Interval)
		cmd.Flag("batch", "Query batch size to use until 'limit' is reached").Default("1000").IntVar(&q.BatchSize)
	}

	cmd.Flag("forward", "Scan forwards through logs.").Default("false").BoolVar(&q.Forward)
	cmd.Flag("no-labels", "Do not print any labels").Default("false").BoolVar(&q.NoLabels)
	cmd.Flag("exclude-label", "Exclude labels given the provided key during output.").StringsVar(&q.IgnoreLabelsKey)
	cmd.Flag("include-label", "Include labels given the provided key during output.").StringsVar(&q.ShowLabelsKey)
	cmd.Flag("labels-length", "Set a fixed padding to labels").Default("0").IntVar(&q.FixedLabelsLen)
	cmd.Flag("store-config", "Execute the current query using a configured storage from a given Loki configuration file.").Default("").StringVar(&q.LocalConfig)
	cmd.Flag("colored-output", "Show ouput with colored labels").Default("false").BoolVar(&q.ColoredOutput)

	return q
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
