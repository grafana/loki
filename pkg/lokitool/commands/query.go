package commands

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/client"
	"github.com/grafana/loki/v3/pkg/lokitool/output"
	"github.com/grafana/loki/v3/pkg/lokitool/query"
)

// timestampFormatNames are the names of the formats of the --output-timestamp-format flag in the order of their documentation.
var timestampFormatNames = []string{"rfc3339", "rfc3339nano", "rfc822z", "rfc1123z", "stampmicro", "stampmilli", "stampnano", "unixdate"}

var timestampFormats = map[string]string{
	"rfc3339":     time.RFC3339,
	"rfc3339nano": time.RFC3339Nano,
	"rfc822z":     time.RFC822Z,
	"rfc1123z":    time.RFC1123Z,
	"stampmicro":  time.StampMicro,
	"stampmilli":  time.StampMilli,
	"stampnano":   time.StampNano,
	"unixdate":    time.UnixDate,
}

// QueryCommand registers the commands that query Loki.
type QueryCommand struct {
	// ClientConfig is the connection configuration shared by all commands.
	ClientConfig *client.Config

	// Flags of the query command group.

	// Stdin makes the commands evaluate the queries on log lines from stdin instead of querying Loki.
	Stdin bool
	// Stats prints the query statistics to stderr.
	Stats bool
	// Timezone is the time zone of timestamps in the output.
	Timezone string
	// TimestampFormat is the name of the format of timestamps in the default output format.
	TimestampFormat string
	// Format is the output format of log entries.
	Format string

	// out is the destination of the output, it defaults to stdout.
	out io.Writer
}

// Register the query commands and flags with the kingpin application.
func (q *QueryCommand) Register(app *kingpin.Application) {
	queryCmd := app.Command("query", "Query logs and metrics, and explore the data of Loki.")

	queryCmd.Flag("stdin", "Take input logs from stdin").BoolVar(&q.Stdin)
	queryCmd.Flag("stats", "Show query statistics").BoolVar(&q.Stats)
	queryCmd.Flag("timezone", "Specify the timezone to use when formatting output timestamps [Local, UTC]").
		Short('z').Default("Local").EnumVar(&q.Timezone, "Local", "UTC")
	queryCmd.Flag("output-timestamp-format", "Specify the format of timestamps in the default output format").
		Default("rfc3339").EnumVar(&q.TimestampFormat, timestampFormatNames...)
	queryCmd.Flag("format", "Specify the output format of log entries [default, raw, jsonl]. raw suppresses log labels and timestamp.").
		Short('o').Default("default").EnumVar(&q.Format, "default", "raw", "jsonl")
	queryCmd.Flag("output", "Alias for --format.").Hidden().EnumVar(&q.Format, "default", "raw", "jsonl")

	(&queryRangeCommand{QueryCommand: q}).register(queryCmd)
	(&queryInstantCommand{QueryCommand: q}).register(queryCmd)
	(&queryLabelsCommand{QueryCommand: q}).register(queryCmd)
	(&querySeriesCommand{QueryCommand: q}).register(queryCmd)
	(&queryVolumeCommand{QueryCommand: q}).register(queryCmd)
}

func (q *QueryCommand) stdout() io.Writer {
	if q.out != nil {
		return q.out
	}
	return os.Stdout
}

// newClient returns the client that executes the queries.
func (q *QueryCommand) newClient() (client.QueryClient, error) {
	if q.Stdin {
		return client.NewFileClient(os.Stdin, newKitLogger()), nil
	}
	return client.New(*q.ClientConfig)
}

// newLogOutput returns the output for log entries.
func (q *QueryCommand) newLogOutput(noLabels, coloredOutput bool) (output.LogOutput, error) {
	location, err := time.LoadLocation(q.Timezone)
	if err != nil {
		return nil, fmt.Errorf("unable to load timezone '%s': %w", q.Timezone, err)
	}

	options := &output.LogOutputOptions{
		Timezone:        location,
		NoLabels:        noLabels,
		ColoredOutput:   coloredOutput,
		TimestampFormat: timestampFormats[q.TimestampFormat],
	}

	out, err := output.NewLogOutput(q.stdout(), q.Format, options)
	if err != nil {
		return nil, fmt.Errorf("unable to create log output: %w", err)
	}
	return out, nil
}

// parseTime parses an absolute time in RFC3339 format. It returns the default if the value is empty.
func parseTime(value string, defaultTime time.Time) (time.Time, error) {
	if value == "" {
		return defaultTime, nil
	}

	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("unable to parse time: %w", err)
	}
	return t, nil
}

// registerEntryFlags registers the flags that select and print the log entries of a query.
func registerEntryFlags(cmd *kingpin.CmdClause, q *query.Query) {
	cmd.Flag("limit", "Limit on number of entries to print. Setting it to 0 will fetch all entries.").Default("30").IntVar(&q.Limit)
	cmd.Flag("forward", "Scan forwards through logs.").BoolVar(&q.Forward)
	cmd.Flag("no-labels", "Do not print any labels").BoolVar(&q.NoLabels)
	cmd.Flag("exclude-label", "Exclude labels given the provided key during output.").StringsVar(&q.IgnoreLabelsKey)
	cmd.Flag("include-label", "Include labels given the provided key during output.").StringsVar(&q.ShowLabelsKey)
	cmd.Flag("include-common-labels", "Include common labels in output for each log line.").BoolVar(&q.IncludeCommonLabels)
	cmd.Flag("labels-length", "Set a fixed padding to labels").Default("0").IntVar(&q.FixedLabelsLen)
	cmd.Flag("colored-output", "Show output with colored labels").BoolVar(&q.ColoredOutput)
}

// timeRange returns the start and end time of a query. The end time defaults to now and
// the start time defaults to the duration since before the end time.
func timeRange(since time.Duration, from, to string) (start, end time.Time, err error) {
	now := time.Now()

	if start, err = parseTime(from, now.Add(-since)); err != nil {
		return start, end, err
	}
	if end, err = parseTime(to, now); err != nil {
		return start, end, err
	}
	return start, end, nil
}
