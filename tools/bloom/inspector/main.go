package main

import (
	"fmt"
	"os"
	"strings"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" {
		fmt.Println("Usage: go run main.go BLOCK_DIRECTORY")
		os.Exit(2)
	}
	path := os.Args[1]

	fmt.Printf("Block:    %s\n", path)

	r := v1.NewDirectoryBlockReader(path)
	b := v1.NewBlock(r, v1.NewMetrics(nil))
	q := v1.NewBlockQuerier(b, &mempool.SimpleHeapAllocator{}, 256<<20)
	qIter := q.Iter()

	md, err := q.Metadata()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Checksum: 0x%x\n", md.Checksum)
	fmt.Printf("Series:   %+v\n", md.Series)
	fmt.Printf("Options:  %+v\n", md.Options)
	fmt.Println("")

	count := 0
	for qIter.Next() {
		swb := qIter.At()
		series := swb.Series
		fmt.Printf(
			"%s chunks=%d fields=%+v\n",
			series.Fingerprint,
			series.Chunks.Len(),
			series.Meta.Fields.Items(),
		)
		p := 0
		for swb.Blooms.Next() {
			bloom := swb.Blooms.At()
			fmt.Printf(
				"%s page=%d size=%v count=%v fill=%v\n",
				strings.Repeat(" ", 16), // padding
				p,
				bloom.Capacity()/8,
				bloom.Count(),
				bloom.FillRatio(),
			)
			p++
		}
		count++
	}

	if qIter.Err() != nil {
		fmt.Printf("error: %s\n", q.Err())
	}

	fmt.Println("")
	fmt.Printf("Stream count: %4d\n", count)
}
