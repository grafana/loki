package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	gokitlog "github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func main() {
	args := os.Args[1:]
	if len(args) < 1 {
		log.Fatal("requires at least 1 argument: dataobj")
	}

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	fp, err := os.Open(args[0])
	if err != nil {
		log.Fatal(err)
	}
	defer fp.Close()

	fi, err := fp.Stat()
	if err != nil {
		log.Fatal(err)
	}

	orig, err := dataobj.FromReaderAt(fp, fi.Size())
	if err != nil {
		log.Fatal(err)
	}

	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          64 << 10,
		MaxPageRows:             1000,
		TargetObjectSize:        512 << 20,
		TargetSectionSize:       512 << 20,
		BufferSize:              16 << 20,
		SectionStripeMergeLimit: 8,
	}
	scr, err := scratch.NewFilesystem(gokitlog.NewNopLogger(), os.TempDir())
	if err != nil {
		log.Fatal(err)
	}
	b, err := logsobj.NewBuilder(cfg, scr, logsobj.NewBuilderMetrics(), gokitlog.NewNopLogger(), nil)
	if err != nil {
		log.Fatal(err)
	}

	if err := appendObject(ctx, b, orig); err != nil {
		log.Fatal(err)
	}
	start := time.Now()
	sortedObj, closer, err := b.FlushAndSort(ctx)
	duration := time.Since(start)
	if err != nil {
		log.Fatal(err)
	}
	defer closer.Close()

	log.Printf("Took %s\n", duration)

	log.Println("== ORIIGNAL DATAOBJ")
	for _, s := range sortedObj.Sections() {
		log.Println(" ", s.Type.String(), s.Tenant)
	}

	log.Println("== SORTED DATAOBJ")
	for _, s := range sortedObj.Sections() {
		log.Println(" ", s.Type.String(), s.Tenant)
	}

	fw, err := os.CreateTemp("", fi.Name()+"-sorted")
	if err != nil {
		log.Fatal(err)
	}
	defer fw.Close()

	reader, err := sortedObj.Reader(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()

	start = time.Now()
	// Copy the sorted data from reader to the output file
	written, err := io.Copy(fw, reader)
	duration = time.Since(start)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Written %d bytes to %s in %s\n", written, fw.Name(), duration)
}

// appendObject appends every log record of obj to b. The sorted output uses
// the sort schema of b, not the schema of obj.
func appendObject(ctx context.Context, b *logsobj.Builder, obj *dataobj.Object) error {
	for _, tenant := range obj.Tenants() {
		streamLabels := make(map[int64]labels.Labels)
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool { return streams.CheckSection(s) && s.Tenant == tenant }) {
			opened, err := streams.Open(ctx, sec)
			if err != nil {
				return err
			}
			for res := range streams.IterSection(ctx, opened) {
				stream, err := res.Value()
				if err != nil {
					return err
				}
				streamLabels[stream.ID] = stream.Labels.Copy()
			}
		}
		for _, sec := range obj.Sections().Filter(func(s *dataobj.Section) bool { return logs.CheckSection(s) && s.Tenant == tenant }) {
			opened, err := logs.Open(ctx, sec)
			if err != nil {
				return err
			}
			for res := range logs.IterSection(ctx, opened) {
				record, err := res.Value()
				if err != nil {
					return err
				}
				ls, ok := streamLabels[record.StreamID]
				if !ok {
					return fmt.Errorf("tenant %s: record references unknown stream ID %d", tenant, record.StreamID)
				}
				if err := b.AppendRecord(tenant, ls, record.Copy(), record.Timestamp); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
