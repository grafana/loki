package commands

import (
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/labelquery"
)

// queryLabelsCommand lists label names and label values.
type queryLabelsCommand struct {
	*QueryCommand

	query labelquery.LabelQuery

	since time.Duration
	from  string
	to    string
}

func (c *queryLabelsCommand) register(parent *kingpin.CmdClause) {
	cmd := parent.Command("labels", "List the label names, or the values of a label.").Action(c.run)

	cmd.Arg("label", "The name of the label. The values of this label are listed, if it is set.").Default("").StringVar(&c.query.LabelName)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&c.since)
	cmd.Flag("from", "Start looking for labels at this absolute time (inclusive)").StringVar(&c.from)
	cmd.Flag("to", "Stop looking for labels at this absolute time (exclusive)").StringVar(&c.to)
}

func (c *queryLabelsCommand) run(_ *kingpin.ParseContext) error {
	start, end, err := timeRange(c.since, c.from, c.to)
	if err != nil {
		return err
	}
	c.query.Start, c.query.End = start, end

	cli, err := c.newClient()
	if err != nil {
		return err
	}

	return c.query.DoLabels(cli)
}
