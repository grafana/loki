package commands

import (
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/index"
)

const queryStatsHelp = `Run a stats query.

The "query stats" command will take the provided query and return statistics
from the index on how much data is contained in the matching stream(s).
This only works against Loki instances using the TSDB index format.

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.

Example:

	lokitool query stats
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   'my-query'`

// queryStatsCommand returns statistics about the data of the streams that match a query.
type queryStatsCommand struct {
	*QueryCommand

	query index.StatsQuery

	since time.Duration
	from  string
	to    string
}

func (c *queryStatsCommand) register(parent *kingpin.CmdClause) {
	cmd := parent.Command("stats", queryStatsHelp).Action(c.run)

	cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"} |~ \".*error.*\"'").Required().StringVar(&c.query.QueryString)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&c.since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&c.from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&c.to)
}

func (c *queryStatsCommand) run(_ *kingpin.ParseContext) error {
	var err error
	if c.query.Start, c.query.End, err = timeRange(c.since, c.from, c.to); err != nil {
		return err
	}

	cli, err := c.newClient()
	if err != nil {
		return err
	}

	return c.query.DoStats(cli)
}
