package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"net/url"
	"os"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/prometheus/common/config"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/detected"
	"github.com/grafana/loki/v3/pkg/logcli/index"
	"github.com/grafana/loki/v3/pkg/logcli/labelquery"
	"github.com/grafana/loki/v3/pkg/logcli/output"
	"github.com/grafana/loki/v3/pkg/logcli/query"
	"github.com/grafana/loki/v3/pkg/logcli/seriesquery"
	"github.com/grafana/loki/v3/pkg/logcli/volume"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	_ "github.com/grafana/loki/v3/pkg/util/build"
)

var (
	app        = kingpin.New("logcli", "A command-line for loki.").Version(version.Print("logcli"))
	quiet      = app.Flag("quiet", "Suppress query metadata").Default("false").Short('q').Bool()
	statistics = app.Flag("stats", "Show query statistics").Default("false").Bool()
	outputMode = app.Flag("output", "Specify output mode [default, raw, jsonl]. raw suppresses log labels and timestamp.").Default("default").Short('o').Enum("default", "raw", "jsonl")
	timezone   = app.Flag("timezone", "Specify the timezone to use when formatting output timestamps [Local, UTC]").Default("Local").Short('z').Enum("Local", "UTC")
	cpuProfile = app.Flag("cpuprofile", "Specify the location for writing a CPU profile.").Default("").String()
	memProfile = app.Flag("memprofile", "Specify the location for writing a memory profile.").Default("").String()
	stdin      = app.Flag("stdin", "Take input logs from stdin").Bool()

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

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.

Example:

	logcli query
	   --timezone=UTC
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   --output=jsonl
	   'my-query'

The output is limited to 30 entries by default; use --limit to increase.

While "query" does support metrics queries, its output contains multiple
data points between the start and end query time. This output is used to
build graphs, similar to what is seen in the Grafana Explore graph view.
If you are querying metrics and just want the most recent data point
(like what is seen in the Grafana Explore table view), then you should use
the "instant-query" command instead.

Parallelization:

You can download an unlimited number of logs in parallel, there are a few
flags which control this behaviour:

	--parallel-duration
	--parallel-max-workers
	--part-path-prefix
	--overwrite-completed-parts
	--merge-parts
	--keep-parts

Refer to the help for each flag for details about what each of them do.

Example:

	logcli query
	   --timezone=UTC
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   --output=jsonl
	   --parallel-duration="15m"
	   --parallel-max-workers="4"
	   --part-path-prefix="/tmp/my_query"
	   --merge-parts
	   'my-query'

This example will create a queue of jobs to execute, each being 15 minutes in
duration. In this case, that means, for the 10-hour total duration, there will
be forty 15-minute jobs. The --limit flag is ignored.

It will start four workers, and they will each take a job to work on from the
queue until all the jobs have been completed.

Each job will save a "part" file to the location specified by the --part-path-prefix.
Different prefixes can be used to run multiple queries at the same time.
The timestamp of the start and end of the part is in the file name.
While the part is being downloaded, the filename will end in ".part", when it
is complete, the file will be renamed to remove this ".part" extension.
By default, if a completed part file is found, that part will not be downloaded
again. This can be overridden with the --overwrite-completed-parts flag.

Part file example using the previous command, adding --keep-parts so they are
not deleted:

Since we don't have the --forward flag, the parts will be downloaded in reverse.
Two of the workers have finished their jobs (last two files), and have picked
up the next jobs in the queue.
Running ls, this is what we should expect to see.

$ ls -1 /tmp/my_query*
/tmp/my_query_20210119T183000_20210119T184500.part.tmp
/tmp/my_query_20210119T184500_20210119T190000.part.tmp
/tmp/my_query_20210119T190000_20210119T191500.part.tmp
/tmp/my_query_20210119T191500_20210119T193000.part.tmp
/tmp/my_query_20210119T193000_20210119T194500.part
/tmp/my_query_20210119T194500_20210119T200000.part

If you do not specify the --merge-parts flag, the part files will be
downloaded, and logcli will exit, and you can process the files as you wish.
With the flag specified, the part files will be read in order, and the output
printed to the terminal. The lines will be printed as soon as the next part is
complete, you don't have to wait for all the parts to download before getting
output. The --merge-parts flag will remove the part files when it is done
reading each of them. To change this, you can use the --keep-parts flag, and
the part files will not be removed.`)
	rangeQuery = newQuery(false, queryCmd)
	tail       = queryCmd.Flag("tail", "Tail the logs").Short('t').Default("false").Bool()
	follow     = queryCmd.Flag("follow", "Alias for --tail").Short('f').Default("false").Bool()
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

https://grafana.com/docs/loki/latest/logql/`)
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

	fmtCmd = app.Command("fmt", "Formats a LogQL query.")

	statsCmd = app.Command("stats", `Run a stats query.

The "stats" command will take the provided query and return statistics
from the index on how much data is contained in the matching stream(s).
This only works against Loki instances using the TSDB index format.

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.

Example:

	logcli stats
	   --timezone=UTC
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   'my-query'
  `)
	statsQuery = newStatsQuery(statsCmd)

	volumeCmd = app.Command("volume", `Run a volume query.

The "volume" command will take the provided label selector(s) and return aggregate
volumes for series matching those volumes. This only works
against Loki instances using the TSDB index format.

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.

Example:

	logcli volume
	   --timezone=UTC
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   'my-query'
  `)
	volumeQuery = newVolumeQuery(false, volumeCmd)

	volumeRangeCmd = app.Command("volume_range", `Run a volume query and return timeseries data.

The "volume_range" command will take the provided label selector(s) and return aggregate
volumes for series matching those volumes, aggregated into buckets according to the step value.
This only works against Loki instances using the TSDB index format.

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.

Example:

	logcli volume_range
	   --timezone=UTC
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
     --step=1h
	   'my-query'
  `)
	volumeRangeQuery = newVolumeQuery(true, volumeRangeCmd)

	detectedFieldsCmd = app.Command("detected-fields", `Run a query for detected fields..

The "detected-fields" command will return information about fields detected using either
the "logfmt" or "json" parser against the log lines returned by the provided query for the
provided time range.

The "detected-fields" command will output extra information about the query
and its results, such as the API URL, set of common labels, and set
of excluded labels. This extra information can be suppressed with the
--quiet flag.

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.

Example:

	logcli detected-fields
	   --timezone=UTC
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   --output=jsonl
	   'my-query'

The output is limited to 100 fields by default; use --field-limit to increase.
The query is limited to processing 1000 lines per subquery; use --line-limit to increase.
`)

	detectedFieldsQuery = newDetectedFieldsQuery(detectedFieldsCmd)
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

	if *stdin {
		queryClient = client.NewFileClient(os.Stdin)
		if rangeQuery.Step.Seconds() == 0 {
			// Set default value for `step` based on `start` and `end`.
			// In non-stdin case, this is set on Loki server side.
			// If this is not set, then `step` will have default value of 1 nanosecond and `STepEvaluator` will go through every nanosecond when applying aggregation during metric queries.
			rangeQuery.Step = defaultQueryRangeStep(rangeQuery.Start, rangeQuery.End)
		}

		// When `--stdin` flag is set, stream selector is optional in the query.
		// But logQL package throw parser error if stream selector is not provided.
		// So we inject "dummy" stream selector if not provided by user already.
		// Which brings down to two ways of using LogQL query under `--stdin`.
		// 1. Query with stream selector(e.g: `{foo="bar"}|="error"`)
		// 2. Query without stream selector (e.g: `|="error"`)

		qs := strings.TrimSpace(rangeQuery.QueryString)
		if strings.HasPrefix(qs, "|") || strings.HasPrefix(qs, "!") {
			// inject the dummy stream selector
			rangeQuery.QueryString = `{source="logcli"}` + rangeQuery.QueryString
		}

		// `--limit` doesn't make sense when using `--stdin` flag.
		rangeQuery.Limit = 0
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

		if *tail || *follow {
			rangeQuery.TailQuery(time.Duration(*delayFor)*time.Second, queryClient, out)
		} else if rangeQuery.ParallelMaxWorkers == 1 {
			rangeQuery.DoQuery(queryClient, out, *statistics)
		} else {
			// `--limit` doesn't make sense when using parallelism.
			rangeQuery.Limit = 0
			rangeQuery.DoQueryParallel(queryClient, out, *statistics)
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
	case fmtCmd.FullCommand():
		if err := formatLogQL(os.Stdin, os.Stdout); err != nil {
			log.Fatalf("unable to format logql: %s", err)
		}
	case statsCmd.FullCommand():
		statsQuery.DoStats(queryClient)
	case volumeCmd.FullCommand(), volumeRangeCmd.FullCommand():
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

		if cmd == volumeRangeCmd.FullCommand() {
			index.GetVolumeRange(volumeRangeQuery, queryClient, out, *statistics)
		} else {
			index.GetVolume(volumeQuery, queryClient, out, *statistics)
		}
	case detectedFieldsCmd.FullCommand():
		detectedFieldsQuery.Do(queryClient, *outputMode)
	}
}

func formatLogQL(r io.Reader, w io.Writer) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	expr, err := syntax.ParseExpr(string(b))
	if err != nil {
		return fmt.Errorf("failed to parse the query: %w", err)
	}

	fmt.Fprintf(w, "%s\n", syntax.Prettify(expr))

	return nil
}

func newQueryClient(app *kingpin.Application) client.Client {

	client := &client.DefaultClient{
		TLSConfig: config.TLSConfig{},
	}

	// extract host
	addressAction := func(_ *kingpin.ParseContext) error {
		// If a proxy is to be used do not set TLS ServerName. In the case of HTTPS proxy this ensures
		// the http client validates both the proxy's cert and the cert used by loki behind the proxy
		// using the ServerName's from the provided --addr and --proxy-url flags.
		if client.ProxyURL != "" {
			return nil
		}

		u, err := url.Parse(client.Address)
		if err != nil {
			return err
		}
		client.TLSConfig.ServerName = strings.Split(u.Host, ":")[0]
		return nil
	}

	app.Flag("addr", "Server address. Can also be set using LOKI_ADDR env var.").Default("http://localhost:3100").Envar("LOKI_ADDR").Action(addressAction).StringVar(&client.Address)
	app.Flag("username", "Username for HTTP basic auth. Can also be set using LOKI_USERNAME env var.").Default("").Envar("LOKI_USERNAME").StringVar(&client.Username)
	app.Flag("password", "Password for HTTP basic auth. Can also be set using LOKI_PASSWORD env var.").Default("").Envar("LOKI_PASSWORD").StringVar(&client.Password)
	app.Flag("ca-cert", "Path to the server Certificate Authority. Can also be set using LOKI_CA_CERT_PATH env var.").Default("").Envar("LOKI_CA_CERT_PATH").StringVar(&client.TLSConfig.CAFile)
	app.Flag("tls-skip-verify", "Server certificate TLS skip verify. Can also be set using LOKI_TLS_SKIP_VERIFY env var.").Default("false").Envar("LOKI_TLS_SKIP_VERIFY").BoolVar(&client.TLSConfig.InsecureSkipVerify)
	app.Flag("cert", "Path to the client certificate. Can also be set using LOKI_CLIENT_CERT_PATH env var.").Default("").Envar("LOKI_CLIENT_CERT_PATH").StringVar(&client.TLSConfig.CertFile)
	app.Flag("key", "Path to the client certificate key. Can also be set using LOKI_CLIENT_KEY_PATH env var.").Default("").Envar("LOKI_CLIENT_KEY_PATH").StringVar(&client.TLSConfig.KeyFile)
	app.Flag("org-id", "adds X-Scope-OrgID to API requests for representing tenant ID. Useful for requesting tenant data when bypassing an auth gateway. Can also be set using LOKI_ORG_ID env var.").Default("").Envar("LOKI_ORG_ID").StringVar(&client.OrgID)
	app.Flag("query-tags", "adds X-Query-Tags http header to API requests. This header value will be part of `metrics.go` statistics. Useful for tracking the query. Can also be set using LOKI_QUERY_TAGS env var.").Default("").Envar("LOKI_QUERY_TAGS").StringVar(&client.QueryTags)
	app.Flag("nocache", "adds Cache-Control: no-cache http header to API requests. Can also be set using LOKI_NO_CACHE env var.").Default("false").Envar("LOKI_NO_CACHE").BoolVar(&client.NoCache)
	app.Flag("bearer-token", "adds the Authorization header to API requests for authentication purposes. Can also be set using LOKI_BEARER_TOKEN env var.").Default("").Envar("LOKI_BEARER_TOKEN").StringVar(&client.BearerToken)
	app.Flag("bearer-token-file", "adds the Authorization header to API requests for authentication purposes. Can also be set using LOKI_BEARER_TOKEN_FILE env var.").Default("").Envar("LOKI_BEARER_TOKEN_FILE").StringVar(&client.BearerTokenFile)
	app.Flag("retries", "How many times to retry each query when getting an error response from Loki. Can also be set using LOKI_CLIENT_RETRIES env var.").Default("0").Envar("LOKI_CLIENT_RETRIES").IntVar(&client.Retries)
	app.Flag("min-backoff", "Minimum backoff time between retries. Can also be set using LOKI_CLIENT_MIN_BACKOFF env var.").Default("0").Envar("LOKI_CLIENT_MIN_BACKOFF").IntVar(&client.BackoffConfig.MinBackoff)
	app.Flag("max-backoff", "Maximum backoff time between retries. Can also be set using LOKI_CLIENT_MAX_BACKOFF env var.").Default("0").Envar("LOKI_CLIENT_MAX_BACKOFF").IntVar(&client.BackoffConfig.MaxBackoff)
	app.Flag("auth-header", "The authorization header used. Can also be set using LOKI_AUTH_HEADER env var.").Default("Authorization").Envar("LOKI_AUTH_HEADER").StringVar(&client.AuthHeader)
	app.Flag("proxy-url", "The http or https proxy to use when making requests. Can also be set using LOKI_HTTP_PROXY_URL env var.").Default("").Envar("LOKI_HTTP_PROXY_URL").StringVar(&client.ProxyURL)

	return client
}

func newLabelQuery(cmd *kingpin.CmdClause) *labelquery.LabelQuery {
	var labelName, from, to string
	var since time.Duration

	q := &labelquery.LabelQuery{}

	// executed after all command flags are parsed
	cmd.Action(func(_ *kingpin.ParseContext) error {

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
	cmd.Action(func(_ *kingpin.ParseContext) error {

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
	cmd.Action(func(_ *kingpin.ParseContext) error {

		if instant {
			q.SetInstant(mustParse(now, time.Now()))
		} else {
			defaultEnd := time.Now()
			defaultStart := defaultEnd.Add(-since)

			q.Start = mustParse(from, defaultStart)
			q.End = mustParse(to, defaultEnd)

			if q.ParallelMaxWorkers < 1 {
				log.Println("parallel-max-workers must be greater than 0, defaulting to 1.")
				q.ParallelMaxWorkers = 1
			}
		}
		q.Quiet = *quiet

		return nil
	})

	cmd.Flag("limit", "Limit on number of entries to print. Setting it to 0 will fetch all entries.").Default("30").IntVar(&q.Limit)
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
		cmd.Flag("parallel-duration", "Split the range into jobs of this length to download the logs in parallel. This will result in the logs being out of order. Use --part-path-prefix to create a file per job to maintain ordering.").Default("1h").DurationVar(&q.ParallelDuration)
		cmd.Flag("parallel-max-workers", "Max number of workers to start up for parallel jobs. A value of 1 will not create any parallel workers. When using parallel workers, limit is ignored.").Default("1").IntVar(&q.ParallelMaxWorkers)
		cmd.Flag("part-path-prefix", "When set, each server response will be saved to a file with this prefix. Creates files in the format: 'prefix-utc_start-utc_end.part'. Intended to be used with the parallel-* flags so that you can combine the files to maintain ordering based on the filename. Default is to write to stdout.").StringVar(&q.PartPathPrefix)
		cmd.Flag("overwrite-completed-parts", "Overwrites completed part files. This will download the range again, and replace the original completed part file. Default will skip a range if it's part file is already downloaded.").Default("false").BoolVar(&q.OverwriteCompleted)
		cmd.Flag("merge-parts", "Reads the part files in order and writes the output to stdout. Original part files will be deleted with this option.").Default("false").BoolVar(&q.MergeParts)
		cmd.Flag("keep-parts", "Overrides the default behaviour of --merge-parts which will delete the part files once all the files have been read. This option will keep the part files.").Default("false").BoolVar(&q.KeepParts)
	}

	cmd.Flag("forward", "Scan forwards through logs.").Default("false").BoolVar(&q.Forward)
	cmd.Flag("no-labels", "Do not print any labels").Default("false").BoolVar(&q.NoLabels)
	cmd.Flag("exclude-label", "Exclude labels given the provided key during output.").StringsVar(&q.IgnoreLabelsKey)
	cmd.Flag("include-label", "Include labels given the provided key during output.").StringsVar(&q.ShowLabelsKey)
	cmd.Flag("labels-length", "Set a fixed padding to labels").Default("0").IntVar(&q.FixedLabelsLen)
	cmd.Flag("store-config", "Execute the current query using a configured storage from a given Loki configuration file.").Default("").StringVar(&q.LocalConfig)
	cmd.Flag("remote-schema", "Execute the current query using a remote schema retrieved from the configured -schema-store.").Default("false").BoolVar(&q.FetchSchemaFromStorage)
	cmd.Flag("schema-store", "Store used for retrieving remote schema.").Default("").StringVar(&q.SchemaStore)
	cmd.Flag("colored-output", "Show output with colored labels").Default("false").BoolVar(&q.ColoredOutput)

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

// This method is to duplicate the same logic of `step` value from `start` and `end`
// done on the loki server side.
// https://github.com/grafana/loki/blob/main/pkg/loghttp/params.go
func defaultQueryRangeStep(start, end time.Time) time.Duration {
	step := int(math.Max(math.Floor(end.Sub(start).Seconds()/250), 1))
	return time.Duration(step) * time.Second
}

func newStatsQuery(cmd *kingpin.CmdClause) *index.StatsQuery {
	// calculate query range from cli params
	var from, to string
	var since time.Duration

	q := &index.StatsQuery{}

	// executed after all command flags are parsed
	cmd.Action(func(_ *kingpin.ParseContext) error {
		defaultEnd := time.Now()
		defaultStart := defaultEnd.Add(-since)

		q.Start = mustParse(from, defaultStart)
		q.End = mustParse(to, defaultEnd)

		q.Quiet = *quiet

		return nil
	})

	cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"} |~ \".*error.*\"'").Required().StringVar(&q.QueryString)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&to)

	return q
}

func newVolumeQuery(rangeQuery bool, cmd *kingpin.CmdClause) *volume.Query {
	// calculate query range from cli params
	var from, to string
	var since time.Duration

	q := &volume.Query{}

	// executed after all command flags are parsed
	cmd.Action(func(_ *kingpin.ParseContext) error {
		defaultEnd := time.Now()
		defaultStart := defaultEnd.Add(-since)

		q.Start = mustParse(from, defaultStart)
		q.End = mustParse(to, defaultEnd)

		q.Quiet = *quiet

		return nil
	})

	cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"}").Required().StringVar(&q.QueryString)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&to)

	cmd.Flag("limit", "Limit on number of series to return volumes for.").Default("30").IntVar(&q.Limit)
	cmd.Flag("targetLabels", "List of labels to aggregate results into.").StringsVar(&q.TargetLabels)
	cmd.Flag("aggregateByLabels", "Whether to aggregate results by label name only.").BoolVar(&q.AggregateByLabels)

	if rangeQuery {
		cmd.Flag("step", "Query resolution step width, roll up volumes into buckets cover step time each.").Default("1h").DurationVar(&q.Step)
	}

	return q
}

func newDetectedFieldsQuery(cmd *kingpin.CmdClause) *detected.FieldsQuery {
	// calculate query range from cli params
	var from, to string
	var since time.Duration

	q := &detected.FieldsQuery{}

	// executed after all command flags are parsed
	cmd.Action(func(_ *kingpin.ParseContext) error {
		defaultEnd := time.Now()
		defaultStart := defaultEnd.Add(-since)

		q.Start = mustParse(from, defaultStart)
		q.End = mustParse(to, defaultEnd)

		q.Quiet = *quiet

		return nil
	})

	cmd.Flag("field-limit", "Limit on number of fields to return.").
		Default("100").
		IntVar(&q.FieldLimit)
	cmd.Flag("line-limit", "Limit the number of lines each subquery is allowed to process.").
		Default("1000").
		IntVar(&q.LineLimit)
	cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"} |~ \".*error.*\"'").
		Required().
		StringVar(&q.QueryString)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&to)
	cmd.Flag("step", "Query resolution step width, for metric queries. Evaluate the query at the specified step over the time range.").
		DurationVar(&q.Step)

	return q
}
