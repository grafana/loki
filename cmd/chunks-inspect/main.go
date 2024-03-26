package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/chunkenc"

	logql "github.com/grafana/loki/pkg/logql/log"
)

const format = "2006-01-02 15:04:05.000000 MST"

var timezone = time.UTC

func main() {
	blocks := flag.Bool("b", false, "print block details")
	lines := flag.Bool("l", false, "print log lines")
	export := flag.Bool("e", false, "export log lines to file")
	flag.Parse()

	for _, f := range flag.Args() {
		printFile(f, *blocks, *lines, *export)
	}
}

func printFile(filename string, blockDetails, printLines bool, exportLogLines bool) {
	f, err := os.Open(filename)
	if err != nil {
		log.Printf("%s: %v", filename, err)
		return
	}
	defer f.Close()

	h, err := DecodeHeader(f)
	if err != nil {
		log.Printf("%s: %v", filename, err)
		return
	}

	fmt.Println()
	fmt.Println("Chunks file:", filename)
	fmt.Println("Metadata length:", h.MetadataLength)
	fmt.Println("Data length:", h.DataLength)
	fmt.Println("UserID:", h.UserID)
	from, through := h.From.Time().In(timezone), h.Through.Time().In(timezone)
	fmt.Println("From:", from.Format(format))
	fmt.Println("Through:", through.Format(format), "("+through.Sub(from).String()+")")
	fmt.Println("Labels:")

	for _, l := range h.Metric {
		fmt.Println("\t", l.Name, "=", l.Value)
	}

	lokiChunk, err := parseLokiChunk(h, f)
	if err != nil {
		log.Printf("%s: %v", filename, err)
		return
	}

	fmt.Println("Format (Version):", lokiChunk.format)
	fmt.Println("Encoding:", lokiChunk.encoding)
	if blockDetails {
		fmt.Println("Found", len(lokiChunk.blocks), "block(s)")
	} else {
		fmt.Println("Found", len(lokiChunk.blocks), "block(s), use -b to show block details")
	}

	if blockDetails {
		fmt.Println()
	}

	pipeline := logql.NewNoopPipeline()
	for ix, b := range lokiChunk.blocks {
		if blockDetails {
			fmt.Printf("Block %4d: position: %8d, minT: %v maxT: %v\n",
				ix, b.Offset(),
				time.Unix(0, b.MinTime()).In(timezone).Format(format),
				time.Unix(0, b.MaxTime()).In(timezone).Format(format),
			)
		}

		if printLines {
			iter := b.Iterator(context.Background(), pipeline.ForStream(nil))
			for iter.Next() {
				e := iter.Entry()
				fmt.Printf("%v\t%s\n", e.Timestamp.In(timezone).Format(format), strings.TrimSpace(e.Line))
				if e.StructuredMetadata != nil {
					fmt.Println("Structured Metadata:")
					for _, meta := range e.StructuredMetadata {
						fmt.Println("\t", meta.Name, "=", meta.Value)
					}
				}
			}
		}
	}

	if exportLogLines {
		exportLogLinesToFile(lokiChunk.blocks, fmt.Sprintf("%s.log", filename))
	}
}

func exportLogLinesToFile(blocks []chunkenc.Block, filename string) {

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Failed to open (create) file '%s' for writing.\n", filename)
	}
	defer f.Close()

	pipeline := logql.NewNoopPipeline()
	for _, b := range blocks {
		iter := b.Iterator(context.Background(), pipeline.ForStream(nil))
		for iter.Next() {
			e := iter.Entry()
			_, err := f.Write([]byte(e.Line + "\n"))
			if err != nil {
				log.Printf("Failed to write to file '%s'.\n", filename)
				return
			}
		}
	}
}
