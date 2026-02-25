package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/fatih/color"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// listStreamsCommand lists the streams in the data object.
type listStreamsCommand struct {
	files  *[]string
	tenant *string
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
		exitWithErr(fmt.Errorf("failed to open file: %w", err))
	}
	defer func() { _ = f.Close() }()
	fi, err := f.Stat()
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read fileinfo: %w", err))
	}
	dataObj, err := dataobj.FromReaderAt(f, fi.Size())
	if err != nil {
		exitWithErr(fmt.Errorf("failed to read dataobj: %w", err))
	}
	cmd.listStreams(context.TODO(), dataObj)
}

func (cmd *listStreamsCommand) listStreams(ctx context.Context, dataObj *dataobj.Object) {
	type key struct {
		tenant string
		id     int64
	}
	var (
		result = make(map[key]streams.Stream)
		tmp    = make([]streams.Stream, 512)
	)
	for _, sec := range dataObj.Sections() {
		if *cmd.tenant != "" && sec.Tenant != *cmd.tenant {
			continue
		}
		if streams.CheckSection(sec) {
			streamsSec, err := streams.Open(ctx, sec)
			if err != nil {
				exitWithErr(fmt.Errorf("failed to open streams section: %w", err))
			}
			r := streams.NewRowReader(streamsSec)
			for {
				n, err := r.Read(ctx, tmp)
				if err != nil && !errors.Is(err, io.EOF) {
					exitWithErr(err)
				}
				if n == 0 && errors.Is(err, io.EOF) {
					break
				}
				for _, s := range tmp[:n] {
					result[key{tenant: sec.Tenant, id: s.ID}] = s
				}
			}
		}
	}
	sorted := make([]key, 0, len(result))
	for id := range result {
		sorted = append(sorted, id)
	}
	slices.SortFunc(sorted, func(a, b key) int { return cmp.Or(strings.Compare(a.tenant, b.tenant), int(a.id-b.id)) })
	bold := color.New(color.Bold)
	for _, k := range sorted {
		s := result[k]
		bold.Printf("tenant: %s, id: %d, from: %s, to: %s, labels: %s\n", k.tenant, k.id, s.MinTimestamp.Format(time.DateTime), s.MaxTimestamp.Format(time.DateTime), s.Labels)
	}
}

func addListStreamsCommand(app *kingpin.Application) {
	cmd := &listStreamsCommand{}
	dump := app.Command("list-streams", "Lists all streams in the data object.").Action(cmd.run)
	cmd.files = dump.Arg("file", "The file to list.").ExistingFiles()
	cmd.tenant = dump.Flag("tenant", "Filter the list to a specific tenant").String()
}
