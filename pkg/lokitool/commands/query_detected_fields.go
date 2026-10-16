package commands

import (
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/detected"
)

const queryDetectedFieldsHelp = `Run a query for detected fields.

The "query detected-fields" command will return information about fields detected using either
the "logfmt" or "json" parser against the log lines returned by the provided query for the
provided time range. If a field name is given, it will return the values of this field.

Information about the query and its results, such as the API URL, is logged at debug level.
Use --log.level=debug to show it.

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.

The output formats raw and jsonl print the response of Loki as JSON.

Example:

	lokitool query
	   --timezone=UTC
	   --format=jsonl
	   detected-fields
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   'my-query'

The output is limited to 100 fields by default; use --limit to increase.
The query is limited to processing 1000 lines per subquery; use --line-limit to increase.`

// queryDetectedFieldsCommand lists the fields that Loki detects in log lines.
type queryDetectedFieldsCommand struct {
	*QueryCommand

	query detected.FieldsQuery

	since time.Duration
	from  string
	to    string
}

func (c *queryDetectedFieldsCommand) register(parent *kingpin.CmdClause) {
	cmd := parent.Command("detected-fields", queryDetectedFieldsHelp).Action(c.run)
	q := &c.query

	cmd.Flag("limit", "Limit on number of fields or values to return.").Default("100").IntVar(&q.Limit)
	cmd.Flag("line-limit", "Limit the number of lines each subquery is allowed to process.").Default("1000").IntVar(&q.LineLimit)
	cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"} |~ \".*error.*\"'").Required().StringVar(&q.QueryString)
	cmd.Arg("field", "The name of the field.").Default("").StringVar(&q.FieldName)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&c.since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&c.from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&c.to)
	cmd.Flag("step", "Query resolution step width, for metric queries. Evaluate the query at the specified step over the time range.").Default("10s").DurationVar(&q.Step)
}

func (c *queryDetectedFieldsCommand) run(_ *kingpin.ParseContext) error {
	var err error
	if c.query.Start, c.query.End, err = timeRange(c.since, c.from, c.to); err != nil {
		return err
	}

	cli, err := c.newClient()
	if err != nil {
		return err
	}

	return c.query.Do(cli, c.Format)
}
