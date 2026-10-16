package commands

import (
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/index"
	"github.com/grafana/loki/v3/pkg/lokitool/volume"
)

const queryVolumeHelp = `Run a volume query.

The "query volume" command will take the provided label selector(s) and return aggregate
volumes for series matching those volumes. This only works
against Loki instances using the TSDB index format.

By default we look over the last hour of data; use --since to modify
or provide specific start and end times with --from and --to respectively.

Notice that when using --from and --to then ensure to use RFC3339Nano
time format, but without timezone at the end. The local timezone will be added
automatically or if using  --timezone flag.

Example:

	lokitool query volume
	   --from="2021-01-19T10:00:00Z"
	   --to="2021-01-19T20:00:00Z"
	   'my-query'`

// queryVolumeCommand returns the volume of the streams that match a query.
type queryVolumeCommand struct {
	*QueryCommand

	query volume.Query

	since time.Duration
	from  string
	to    string
}

func (c *queryVolumeCommand) register(parent *kingpin.CmdClause) {
	cmd := parent.Command("volume", queryVolumeHelp).Action(c.run)
	q := &c.query

	cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"}").Required().StringVar(&q.QueryString)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&c.since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&c.from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&c.to)
	cmd.Flag("limit", "Limit on number of series to return volumes for.").Default("30").IntVar(&q.Limit)
	cmd.Flag("targetLabels", "List of labels to aggregate results into.").StringsVar(&q.TargetLabels)
	cmd.Flag("aggregateByLabels", "Whether to aggregate results by label name only.").BoolVar(&q.AggregateByLabels)
}

func (c *queryVolumeCommand) run(_ *kingpin.ParseContext) error {
	var err error
	if c.query.Start, c.query.End, err = timeRange(c.since, c.from, c.to); err != nil {
		return err
	}

	cli, err := c.newClient()
	if err != nil {
		return err
	}

	out, err := c.newLogOutput(false, false)
	if err != nil {
		return err
	}

	return index.GetVolume(&c.query, cli, out, c.Stats)
}
