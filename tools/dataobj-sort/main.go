package main

import (
	"context"
	"io"
	"log"
	"os"
	"time"

	gokitlog "github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
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

	cfg := logsobj.BuilderConfig{
		BuilderBaseConfig: logsobj.BuilderBaseConfig{
			TargetPageSize:          64 << 10,
			MaxPageRows:             1000,
			TargetObjectSize:        512 << 20,
			TargetSectionSize:       512 << 20,
			BufferSize:              16 << 20,
			SectionStripeMergeLimit: 8,
		},
	}
	scr, err := scratch.NewFilesystem(gokitlog.NewNopLogger(), os.TempDir())
	if err != nil {
		log.Fatal(err)
	}
	b, err := logsobj.NewBuilder(cfg, scr)
	if err != nil {
		log.Fatal(err)
	}

	start := time.Now()
	sortedObj, closer, err := b.CopyAndSort(ctx, orig)
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
