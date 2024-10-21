package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	logql "github.com/grafana/loki/v3/pkg/logql/log"
)

const format = "2006-01-02 15:04:05.000000 MST"

var timezone = time.UTC

func main() {
	blocks := flag.Bool("b", false, "print block details")
	lines := flag.Bool("l", false, "print log lines")
	storeBlocks := flag.Bool("s", false, "store blocks, using input filename, and appending block index to it")
	flag.Parse()

	for _, f := range flag.Args() {
		printFile(f, *blocks, *lines, *storeBlocks)
	}
}

func printFile(filename string, blockDetails, printLines, storeBlocks bool) {
	f, err := os.Open(filename)
	if err != nil {
		log.Printf("%s: %v", filename, err)
		return
	}
	defer func() { _ = f.Close() }()

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

	fmt.Println("Format (Version):", lokiChunk.version)
	fmt.Println("Encoding:", lokiChunk.encoding)
	if blockDetails {
		fmt.Println("Found", len(lokiChunk.blocks), "block(s)")
	} else {
		fmt.Println("Found", len(lokiChunk.blocks), "block(s), use -b to show block details")
	}

	if len(lokiChunk.blocks) > 0 {
		fmt.Println("Minimum time (from first block):", time.Unix(0, lokiChunk.blocks[0].MinTime()).In(timezone).Format(format))
		fmt.Println("Maximum time (from last block):", time.Unix(0, lokiChunk.blocks[len(lokiChunk.blocks)-1].MaxTime()).In(timezone).Format(format))
	}

	if blockDetails {
		fmt.Println()
	}

	pipeline := logql.NewNoopPipeline()
	for ix, b := range lokiChunk.blocks {
		if blockDetails {
			fmt.Printf("Block %4d: position: %8d, original length: %6d, minT: %v maxT: %v\n",
				ix, b.Offset(), len(b.rawData),
				time.Unix(0, b.MinTime()).In(timezone).Format(format),
				time.Unix(0, b.MaxTime()).In(timezone).Format(format),
			)
		}

		if printLines {
			iter := b.Iterator(context.Background(), pipeline.ForStream(nil))
			for iter.Next() {
				e := iter.At()
				fmt.Printf("%v\t%s\n", e.Timestamp.In(timezone).Format(format), strings.TrimSpace(e.Line))
				if e.StructuredMetadata != nil {
					fmt.Println("Structured Metadata:")
					for _, meta := range e.StructuredMetadata {
						fmt.Println("\t", meta.Name, "=", meta.Value)
					}
				}
			}
		}

		if storeBlocks {
			writeBlockToFile(b.rawData, ix, fmt.Sprintf("%s.block.%d", filename, ix))
			writeBlockToFile(b.originalData, ix, fmt.Sprintf("%s.original.%d", filename, ix))
		}
	}
	ratio := float64(lokiChunk.uncompressedSize) / float64(lokiChunk.compressedSize)
	fmt.Println("Total chunk size of uncompressed data:", lokiChunk.uncompressedSize, "compressed data:", lokiChunk.compressedSize, "ratio:", fmt.Sprintf("%0.3g", ratio))
}

func writeBlockToFile(data []byte, blockIndex int, filename string) {
	err := os.WriteFile(filename, data, 0644)
	if err != nil {
		log.Println("Failed to store block", blockIndex, "to file", filename, "due to error:", err)
	} else {
		log.Println("Stored block", blockIndex, "to file", filename)
	}
}
