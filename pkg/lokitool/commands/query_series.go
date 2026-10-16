package commands

import (
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/seriesquery"
)

const querySeriesHelp = `Run series query.

The "query series" command will take the provided label matcher
and return all the log streams found in the time window.

It is possible to send an empty label matcher '{}' to return all streams.

Use the --analyze-labels flag to get a summary of the labels found in all streams.
This is helpful to find high cardinality labels.`

// querySeriesCommand lists the streams that match a label matcher.
type querySeriesCommand struct {
	*QueryCommand

	query seriesquery.SeriesQuery

	since time.Duration
	from  string
	to    string
}

func (c *querySeriesCommand) register(parent *kingpin.CmdClause) {
	cmd := parent.Command("series", querySeriesHelp).Action(c.run)

	cmd.Arg("matcher", "eg '{foo=\"bar\",baz=~\".*blip\"}'").Required().StringVar(&c.query.Matcher)
	cmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&c.since)
	cmd.Flag("from", "Start looking for logs at this absolute time (inclusive)").StringVar(&c.from)
	cmd.Flag("to", "Stop looking for logs at this absolute time (exclusive)").StringVar(&c.to)
	cmd.Flag("analyze-labels", "Printout a summary of labels including count of label value combinations, useful for debugging high cardinality series").BoolVar(&c.query.AnalyzeLabels)
}

func (c *querySeriesCommand) run(_ *kingpin.ParseContext) error {
	start, end, err := timeRange(c.since, c.from, c.to)
	if err != nil {
		return err
	}
	c.query.Start, c.query.End = start, end

	cli, err := c.newClient()
	if err != nil {
		return err
	}

	return c.query.DoSeries(cli)
}
