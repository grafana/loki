package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kingpin/v2"
	"github.com/fatih/color"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// dumpCommand dumps the contents of the data object.
type dumpCommand struct {
	files      *[]string
	printLines *bool
}

func (cmd *dumpCommand) run(c *kingpin.ParseContext) error {
	for _, f := range *cmd.files {
		cmd.dumpFile(f)
	}
	return nil
}

func (cmd *dumpCommand) dumpFile(name string) {
	f, err := os.Open(name)
	if err != nil {
		exitWithError(fmt.Errorf("failed to open file: %w", err))
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		exitWithError(fmt.Errorf("failed to read fileinfo: %w", err))
	}
	dataObj, err := dataobj.FromReaderAt(f, fi.Size())
	if err != nil {
		exitWithError(fmt.Errorf("failed to read dataobj: %w", err))
	}
	for offset, sec := range dataObj.Sections() {
		switch {
		case streams.CheckSection(sec):
			cmd.dumpStreamsSection(context.TODO(), offset, sec)
		case logs.CheckSection(sec):
			cmd.dumpLogsSection(context.TODO(), offset, sec)
		default:
			fmt.Printf("unknown section: %s\n", sec.Type)
		}
	}
}

func (cmd *dumpCommand) dumpStreamsSection(ctx context.Context, offset int, sec *dataobj.Section) {
	streamsSec, err := streams.Open(ctx, sec)
	if err != nil {
		exitWithError(err)
	}
	bold := color.New(color.Bold)
	bold.Println("Streams section:")
	bold.Printf("\toffset: %d\n", offset)

	tmp := make([]streams.Stream, 512)
	r := streams.NewRowReader(streamsSec)
	for {
		n, err := r.Read(ctx, tmp)
		if err != nil && !errors.Is(err, io.EOF) {
			exitWithError(err)
		}
		if n == 0 && errors.Is(err, io.EOF) {
			return
		}
		for _, s := range tmp[:n] {
			bold.Printf("\t\tid: %d, labels:\n", s.ID)
			s.Labels.Range(func(l labels.Label) {
				fmt.Printf("\t\t\t%s=%s\n", l.Name, l.Value)
			})
		}
	}
}

func (cmd *dumpCommand) dumpLogsSection(ctx context.Context, offset int, sec *dataobj.Section) {
	logsSec, err := logs.Open(ctx, sec)
	if err != nil {
		exitWithError(err)
	}
	bold := color.New(color.Bold)
	bold.Println("Logs section:")
	bold.Printf("\toffset: %d\n", offset)
	tmp := make([]logs.Record, 512)
	r := logs.NewRowReader(logsSec)
	for {
		n, err := r.Read(ctx, tmp)
		if err != nil && !errors.Is(err, io.EOF) {
			exitWithError(err)
		}
		if n == 0 && errors.Is(err, io.EOF) {
			return
		}
		for _, r := range tmp[0:n] {
			bold.Printf("\t\tid: %d, timestamp: %s, metadata:\n", r.StreamID, r.Timestamp)
			r.Metadata.Range(func(l labels.Label) {
				fmt.Printf("\t\t\t%s=%s\n", l.Name, l.Value)
			})
			if *cmd.printLines && len(r.Line) > 0 {
				bold.Printf("\t\t> ")
				for pos, char := range string(r.Line) {
					fmt.Printf("%c", char)
					if pos > 0 && pos%100 == 0 {
						bold.Printf("\n\t\t> ")
					}
				}
				fmt.Println("")
			}
		}
	}
}

func addDumpCommand(app *kingpin.Application) {
	cmd := &dumpCommand{}
	dump := app.Command("dump", "Dump the contents of the data object.").Action(cmd.run)
	cmd.printLines = dump.Flag("print-lines", "Prints the lines of each column.").Bool()
	cmd.files = dump.Arg("file", "The file to dump.").ExistingFiles()
}
