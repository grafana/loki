package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/grafana/loki/v3/pkg/storage/wal"
)

const format = "2006-01-02 15:04:05.000000 MST"

func main() {
	streams := flag.Bool("s", false, "print streams")
	flag.Parse()

	for _, f := range flag.Args() {
		printFile(f, *streams)
	}
}

func printFile(filename string, segmentDetails bool) {
	f, err := os.Open(filename)
	if err != nil {
		log.Printf("%s: %v", filename, err)
		return
	}
	defer func() { _ = f.Close() }()

	segmentBytes, err := io.ReadAll(f)
	if err != nil {
		log.Printf("failed to read file: %v", err)
		return
	}
	reader, err := wal.NewReader(segmentBytes)
	if err != nil {
		log.Printf("failed to open segment reader: %v", err)
		return
	}

	iter, err := reader.Series(context.Background())
	if err != nil {
		log.Printf("failed to open series iterator: %v", err)
		return
	}

	segmentFrom := time.Now().Add(time.Hour)
	var segmentTo time.Time

	var actualSeries []string
	tenants := make(map[string]int)

	for iter.Next() {
		actualSeries = append(actualSeries, iter.At().String())
		tenant := iter.At().Get("__loki_tenant__")
		tenants[tenant]++

		chk, err := iter.ChunkReader(nil)
		if err != nil {
			log.Printf("failed to open chunk reader: %v", err)
			return
		}
		for chk.Next() {
			ts, _ := chk.At()
			if segmentFrom.After(time.Unix(0, ts)) {
				segmentFrom = time.Unix(0, ts)
			}
			if segmentTo.Before(time.Unix(0, ts)) {
				segmentTo = time.Unix(0, ts)
			}
		}
	}

	sizes, err := reader.Sizes()
	if err != nil {
		log.Printf("failed to get segment sizes: %v", err)
		return
	}
	seriesSize := int64(0)
	indexSize := sizes.Index
	for _, size := range sizes.Series {
		seriesSize += size
	}

	fmt.Println()
	fmt.Println("Segment file:", filename)
	fmt.Println("Compressed Filesize:", humanize.Bytes(uint64(len(segmentBytes))))
	fmt.Println("Series Size:", humanize.Bytes(uint64(seriesSize)))
	fmt.Println("Index Size:", humanize.Bytes(uint64(indexSize)))
	fmt.Println("Stream count:", len(actualSeries))
	fmt.Println("Tenant count:", len(tenants))
	fmt.Println("From:", segmentFrom.UTC().Format(format))
	fmt.Println("To:", segmentTo.UTC().Format(format))
	fmt.Println("Duration:", segmentTo.Sub(segmentFrom))

	if segmentDetails {
		fmt.Println()
		for _, s := range actualSeries {
			fmt.Println(s)
		}
	}
}
