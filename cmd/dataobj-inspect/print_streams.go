package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alecthomas/kingpin/v2"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

// printStreamsCommand prints the streams in the data object.
type printStreamsCommand struct {
	files     *[]string
	streamIDs *[]int64
}

func (cmd *printStreamsCommand) run(c *kingpin.ParseContext) error {
	for _, f := range *cmd.files {
		cmd.printStreamsInFile(f)
	}
	return nil
}

func (cmd *printStreamsCommand) printStreamsInFile(name string) {
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
	cmd.printStreams(context.TODO(), dataObj)
}

func (cmd *printStreamsCommand) printStreams(ctx context.Context, dataObj *dataobj.Object) {
	var (
		tmp            = make([]logs.Record, 512)
		printAll       = false
		printStreamIDs = make(map[int64]struct{})
	)
	printAll = len(*cmd.streamIDs) == 0
	for _, id := range *cmd.streamIDs {
		printStreamIDs[id] = struct{}{}
	}
	for _, sec := range dataObj.Sections() {
		if logs.CheckSection(sec) {
			logsSec, err := logs.Open(ctx, sec)
			if err != nil {
				exitWithError(fmt.Errorf("failed to open logs section: %w", err))
			}
			r := logs.NewRowReader(logsSec)
			for {
				n, err := r.Read(ctx, tmp)
				if err != nil && !errors.Is(err, io.EOF) {
					exitWithError(err)
				}
				if n == 0 {
					break
				}
				for _, r := range tmp[0:n] {
					if _, ok := printStreamIDs[r.StreamID]; ok || printAll {
						fmt.Printf("%s\n", string(r.Line))
					}
				}
			}
		}
	}
}

func addPrintStreamsCommand(app *kingpin.Application) {
	cmd := &printStreamsCommand{}
	dump := app.Command("print-streams", "Prints the streams in the data object.").Action(cmd.run)
	cmd.streamIDs = dump.Flag("stream-id", "Print lines for these stream IDs.").Int64List()
	cmd.files = dump.Arg("file", "The file to list.").ExistingFiles()
}
