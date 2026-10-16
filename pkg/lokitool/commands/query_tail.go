package commands

import (
	"fmt"
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/client"
	"github.com/grafana/loki/v3/pkg/lokitool/query"
)

const queryTailHelp = `Tail the logs that match a LogQL query.

The "query tail" command streams new log lines as Loki receives them,
until it is interrupted. Use --since or --from to start with entries
from the past.

Use --delay-for to delay the tailing for a number of seconds, so that
Loki can order the entries that it receives from different ingesters.

Example:

	lokitool query tail --since=5m '{app="foo"} |= "error"'`

// queryTailCommand streams the logs that match a query.
type queryTailCommand struct {
	*QueryCommand

	query query.Query

	since    time.Duration
	from     string
	delayFor int
}

func (c *queryTailCommand) register(parent *kingpin.CmdClause) {
	cmd := parent.Command("tail", queryTailHelp).Action(c.run)
	q := &c.query

	cmd.Arg("query", "eg '{foo=\"bar\",baz=~\".*blip\"} |~ \".*error.*\"'").Required().StringVar(&q.QueryString)
	cmd.Flag("limit", "Limit on number of entries to return.").Default("30").IntVar(&q.Limit)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&c.since)
	cmd.Flag("from", "Start tailing at this absolute time (inclusive)").StringVar(&c.from)
	cmd.Flag("delay-for", "Delay in tailing by number of seconds to accumulate logs for re-ordering").Default("0").IntVar(&c.delayFor)
	cmd.Flag("no-labels", "Do not print any labels").BoolVar(&q.NoLabels)
	cmd.Flag("exclude-label", "Exclude labels given the provided key during output.").StringsVar(&q.IgnoreLabelsKey)
	cmd.Flag("include-label", "Include labels given the provided key during output.").StringsVar(&q.ShowLabelsKey)
	cmd.Flag("colored-output", "Show output with colored labels").BoolVar(&q.ColoredOutput)
}

func (c *queryTailCommand) run(_ *kingpin.ParseContext) error {
	if c.Stdin {
		return fmt.Errorf("tail: %w", client.ErrNotSupported)
	}

	var err error
	if c.query.Start, err = parseTime(c.from, time.Now().Add(-c.since)); err != nil {
		return err
	}

	cli, err := client.New(*c.ClientConfig)
	if err != nil {
		return err
	}

	out, err := c.newLogOutput(c.query.NoLabels, c.query.ColoredOutput)
	if err != nil {
		return err
	}

	if err := c.query.TailQuery(time.Duration(c.delayFor)*time.Second, cli, out); err != nil {
		return fmt.Errorf("tail failed: %w", err)
	}
	return nil
}
