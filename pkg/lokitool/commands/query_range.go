package commands

import (
	"math"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	log "github.com/sirupsen/logrus"

	"github.com/grafana/loki/v3/pkg/lokitool/query"
)

const queryRangeHelp = `Run a LogQL range query.

The "query range" command is useful for querying for logs. Logs can be
returned in a few output formats:

	raw: log line
	default: log timestamp + log labels + log line
	jsonl: JSON response from Loki API of log line

The output format can be specified with the "-o" flag, for
example, "-o raw" for the raw output format.

Information about the query and its results, such as the API URL, the set of
common labels, and the set of excluded labels, is logged at debug level.
Use --log.level=debug to show it.

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.
In default output format the --output-timestamp-format flag can be used to
modify the output timestamp.

Example:

	lokitool query
	   --timezone=UTC
	   --output-timestamp-format=rfc3339nano
	   range
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   'my-query'

The output is limited to 30 entries by default; use --limit to increase.

While "query range" does support metrics queries, its output contains multiple
data points between the start and end query time. This output is used to
build graphs, similar to what is seen in the Grafana Explore graph view.
If you are querying metrics and just want the most recent data point
(like what is seen in the Grafana Explore table view), then you should use
the "query instant" command instead.

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

	lokitool query range
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
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
downloaded, and lokitool will exit, and you can process the files as you wish.
With the flag specified, the part files will be read in order, and the output
printed to the terminal. The lines will be printed as soon as the next part is
complete, you don't have to wait for all the parts to download before getting
output. The --merge-parts flag will remove the part files when it is done
reading each of them. To change this, you can use the --keep-parts flag, and
the part files will not be removed.`

// queryRangeCommand runs a LogQL range query.
type queryRangeCommand struct {
	*QueryCommand

	query query.Query

	since time.Duration
	from  string
	to    string
}

func (c *queryRangeCommand) register(parent *kingpin.CmdClause) {
	cmd := parent.Command("range", queryRangeHelp).Action(c.run)
	q := &c.query

	cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"} |~ \".*error.*\"'").Required().StringVar(&q.QueryString)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&c.since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&c.from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&c.to)
	cmd.Flag("step", "Query resolution step width, for metric queries. Evaluate the query at the specified step over the time range.").DurationVar(&q.Step)
	cmd.Flag("interval", "Query interval, for log queries. Return entries at the specified interval, ignoring those between. **This parameter is experimental, please see Issue 1779**").DurationVar(&q.Interval)
	cmd.Flag("batch", "Query batch size to use until 'limit' is reached").Default("1000").IntVar(&q.BatchSize)
	cmd.Flag("parallel-duration", "Split the range into jobs of this length to download the logs in parallel. This will result in the logs being out of order. Use --part-path-prefix to create a file per job to maintain ordering.").Default("1h").DurationVar(&q.ParallelDuration)
	cmd.Flag("parallel-max-workers", "Max number of workers to start up for parallel jobs. A value of 1 will not create any parallel workers. When using parallel workers, limit is ignored.").Default("1").IntVar(&q.ParallelMaxWorkers)
	cmd.Flag("part-path-prefix", "When set, each server response will be saved to a file with this prefix. Creates files in the format: 'prefix-utc_start-utc_end.part'. Intended to be used with the parallel-* flags so that you can combine the files to maintain ordering based on the filename. Default is to write to stdout.").StringVar(&q.PartPathPrefix)
	cmd.Flag("overwrite-completed-parts", "Overwrites completed part files. This will download the range again, and replace the original completed part file. Default will skip a range if it's part file is already downloaded.").BoolVar(&q.OverwriteCompleted)
	cmd.Flag("merge-parts", "Reads the part files in order and writes the output to stdout. Original part files will be deleted with this option.").BoolVar(&q.MergeParts)
	cmd.Flag("keep-parts", "Overrides the default behaviour of --merge-parts which will delete the part files once all the files have been read. This option will keep the part files.").BoolVar(&q.KeepParts)
	registerEntryFlags(cmd, q)
}

func (c *queryRangeCommand) run(_ *kingpin.ParseContext) error {
	q := &c.query

	now := time.Now()
	var err error
	if q.Start, err = parseTime(c.from, now.Add(-c.since)); err != nil {
		return err
	}
	if q.End, err = parseTime(c.to, now); err != nil {
		return err
	}

	if q.ParallelMaxWorkers < 1 {
		log.Warn("parallel-max-workers must be greater than 0, defaulting to 1.")
		q.ParallelMaxWorkers = 1
	}

	cli, err := c.newClient()
	if err != nil {
		return err
	}

	if c.Stdin {
		prepareStdinQuery(q)
	}

	out, err := c.newLogOutput(q.NoLabels, q.ColoredOutput)
	if err != nil {
		return err
	}

	if q.ParallelMaxWorkers == 1 {
		return q.DoQuery(cli, out, c.Stats)
	}

	// `--limit` doesn't make sense when using parallelism.
	q.Limit = 0
	return q.DoQueryParallel(cli, out, c.Stats)
}

// prepareStdinQuery adjusts the query for the evaluation on log lines from stdin.
func prepareStdinQuery(q *query.Query) {
	if q.Step.Seconds() == 0 {
		// Without stdin, Loki sets the default step on the server side.
		// If it is not set here, the step is 1 nanosecond and the evaluation of metric queries
		// goes through every nanosecond of the range.
		q.Step = defaultQueryRangeStep(q.Start, q.End)
	}

	// The stream selector is optional in queries on stdin, but the LogQL parser requires one.
	// This allows queries with and without a stream selector, for example `{foo="bar"} |= "error"` and `|= "error"`.
	qs := strings.TrimSpace(q.QueryString)
	if strings.HasPrefix(qs, "|") || strings.HasPrefix(qs, "!") {
		q.QueryString = `{source="logcli"}` + q.QueryString
	}

	// `--limit` doesn't make sense when reading from stdin.
	q.Limit = 0
}

// defaultQueryRangeStep duplicates the logic that calculates the step from the start and end time
// on the server side: https://github.com/grafana/loki/blob/main/pkg/loghttp/params.go
func defaultQueryRangeStep(start, end time.Time) time.Duration {
	step := int(math.Max(math.Floor(end.Sub(start).Seconds()/250), 1))
	return time.Duration(step) * time.Second
}
