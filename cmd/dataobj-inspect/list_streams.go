package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/alecthomas/kingpin/v2"
	"github.com/fatih/color"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// listStreamsCommand lists the streams in the data object.
type listStreamsCommand struct {
	files *[]string
}

func (cmd *listStreamsCommand) run(c *kingpin.ParseContext) error {
	for _, f := range *cmd.files {
		cmd.listStreamsInFile(f)
	}
	return nil
}

func (cmd *listStreamsCommand) listStreamsInFile(name string) {
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
	cmd.listStreams(context.TODO(), dataObj)
}

func (cmd *listStreamsCommand) listStreams(ctx context.Context, dataObj *dataobj.Object) {
	var (
		result = make(map[int64]streams.Stream)
		tmp    = make([]streams.Stream, 512)
	)
	for _, sec := range dataObj.Sections() {
		if streams.CheckSection(sec) {
			streamsSec, err := streams.Open(ctx, sec)
			if err != nil {
				exitWithError(fmt.Errorf("failed to open streams section: %w", err))
			}
			r := streams.NewRowReader(streamsSec)
			for {
				n, err := r.Read(ctx, tmp)
				if err != nil && !errors.Is(err, io.EOF) {
					exitWithError(err)
				}
				if n == 0 && errors.Is(err, io.EOF) {
					break
				}
				for _, s := range tmp[:n] {
					result[s.ID] = s
				}
			}
		}
	}
	sorted := make([]int64, 0, len(result))
	for id := range result {
		sorted = append(sorted, id)
	}
	slices.Sort(sorted)
	bold := color.New(color.Bold)
	for _, id := range sorted {
		bold.Printf("id: %d, labels:\n", id)
		result[id].Labels.Range(func(l labels.Label) {
			fmt.Printf("\t%s=%s\n", l.Name, l.Value)
		})
	}
}

func addListStreamsCommand(app *kingpin.Application) {
	cmd := &listStreamsCommand{}
	dump := app.Command("list-streams", "Lists all streams in the data object.").Action(cmd.run)
	cmd.files = dump.Arg("file", "The file to list.").ExistingFiles()
}
