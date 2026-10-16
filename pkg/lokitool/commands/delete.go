package commands

import (
	"time"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/lokitool/client"
	"github.com/grafana/loki/v3/pkg/lokitool/delete"
)

const deleteCreateHelp = `Create a new log deletion request.

The "delete create" command creates a new log deletion request for the specified query and time range.

Example:

	lokitool delete create '{job="app"}' --from="2023-01-01T00:00:00Z" --to="2023-01-02T00:00:00Z"`

// DeleteCommand manages log deletion requests.
type DeleteCommand struct {
	// ClientConfig is the connection configuration shared by all commands.
	ClientConfig *client.Config

	// Format is the output format of the list command.
	Format string

	create delete.Query
	list   delete.Query
	cancel delete.Query

	since time.Duration
	from  string
	to    string
}

// Register the delete commands and flags with the kingpin application.
func (d *DeleteCommand) Register(app *kingpin.Application) {
	deleteCmd := app.Command("delete", "Manage log deletion requests.")
	deleteCmd.Flag("format", "Output format of the list command [default, jsonl].").
		Short('o').Default("default").EnumVar(&d.Format, "default", "jsonl")
	deleteCmd.Flag("output", "Alias for --format.").Hidden().EnumVar(&d.Format, "default", "jsonl")

	createCmd := deleteCmd.Command("create", deleteCreateHelp).Action(d.createRequest)
	createCmd.Arg("query", "LogQL query to match log lines for deletion (e.g. '{job=\"app\"}')").Required().StringVar(&d.create.QueryString)
	createCmd.Flag("since", "Lookback window.").Default("1h").DurationVar(&d.since)
	createCmd.Flag("from", "Start time for deletion (inclusive)").StringVar(&d.from)
	createCmd.Flag("to", "End time for deletion (exclusive)").StringVar(&d.to)
	createCmd.Flag("max-interval", "Maximum time interval for delete request").StringVar(&d.create.MaxInterval)

	deleteCmd.Command("list", "List existing log deletion requests.").Action(d.listRequests)

	cancelCmd := deleteCmd.Command("cancel", "Cancel a log deletion request.").Action(d.cancelRequest)
	cancelCmd.Flag("request-id", "ID of the delete request to cancel").Required().StringVar(&d.cancel.RequestID)
	cancelCmd.Flag("force", "Force cancellation of partially completed request").BoolVar(&d.cancel.Force)
}

func (d *DeleteCommand) createRequest(_ *kingpin.ParseContext) error {
	var err error
	if d.create.Start, d.create.End, err = timeRange(d.since, d.from, d.to); err != nil {
		return err
	}

	cli, err := client.New(*d.ClientConfig)
	if err != nil {
		return err
	}

	return d.create.CreateQuery(cli)
}

func (d *DeleteCommand) listRequests(_ *kingpin.ParseContext) error {
	cli, err := client.New(*d.ClientConfig)
	if err != nil {
		return err
	}

	if d.Format == "jsonl" {
		return d.list.ListQueryJSON(cli)
	}
	return d.list.ListQuery(cli)
}

func (d *DeleteCommand) cancelRequest(_ *kingpin.ParseContext) error {
	cli, err := client.New(*d.ClientConfig)
	if err != nil {
		return err
	}

	return d.cancel.CancelQuery(cli, d.cancel.RequestID, d.cancel.Force)
}
