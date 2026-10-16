package commands

import (
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/query"
)

const queryInstantHelp = `Run an instant LogQL query.

The "query instant" command is useful for evaluating a metric query for
a single point in time. This is equivalent to the Grafana Explore table
view; if you want a metrics query that is used to build a Grafana graph,
you should use the "query range" command instead.

This command does not produce useful output when querying for log lines;
you should always use the "query range" command when you are running log queries.

For more information about log queries and metric queries, refer to the
LogQL documentation:

https://grafana.com/docs/loki/latest/logql/`

// queryInstantCommand runs a LogQL instant query.
type queryInstantCommand struct {
	*QueryCommand

	query query.Query

	now string
}

func (c *queryInstantCommand) register(parent *kingpin.CmdClause) {
	cmd := parent.Command("instant", queryInstantHelp).Action(c.run)

	cmd.Arg("query", "eg 'rate({foo=\"bar\"} |~ \".*error.*\" [5m])'").Required().StringVar(&c.query.QueryString)
	cmd.Flag("now", "Time at which to execute the instant query.").StringVar(&c.now)
	registerEntryFlags(cmd, &c.query)
}

func (c *queryInstantCommand) run(_ *kingpin.ParseContext) error {
	now, err := parseTime(c.now, time.Now())
	if err != nil {
		return err
	}
	c.query.SetInstant(now)

	cli, err := c.newClient()
	if err != nil {
		return err
	}

	out, err := c.newLogOutput(c.query.NoLabels, c.query.ColoredOutput)
	if err != nil {
		return err
	}

	return c.query.DoQuery(cli, out, c.Stats)
}
