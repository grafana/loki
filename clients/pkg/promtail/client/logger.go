package client

import (
	"fmt"
	"os"
	"runtime"
	"text/tabwriter"

	"github.com/fatih/color"
	"github.com/grafana/loki/clients/pkg/promtail/api"
)

var (
	yellow = color.New(color.FgYellow)
	blue   = color.New(color.FgBlue)
)

func init() {
	if runtime.GOOS == "windows" {
		yellow.DisableColor()
		blue.DisableColor()
	}
}

type entryPrinter struct {
	writer *tabwriter.Writer
}

func newEntryPrinter() *entryPrinter {
	return &entryPrinter{
		writer: tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0),
	}
}

func (ep *entryPrinter) Print(entry api.Entry) {
	fmt.Fprint(ep.writer, blue.Sprint(entry.Timestamp.Format("2006-01-02T15:04:05.999999999-0700")))
	fmt.Fprint(ep.writer, "\t")
	fmt.Fprint(ep.writer, yellow.Sprint(entry.Labels.String()))
	fmt.Fprint(ep.writer, "\t")
	fmt.Fprint(ep.writer, entry.Line)
	fmt.Fprint(ep.writer, "\n")
	ep.writer.Flush()
}
