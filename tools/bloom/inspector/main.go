package main

import (
	"fmt"
	"os"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go BLOCK_DIRECTORY")
		os.Exit(2)
	}

	path := os.Args[1]
	fmt.Printf("Block directory: %s\n", path)

	r := v1.NewDirectoryBlockReader(path)
	b := v1.NewBlock(r, v1.NewMetrics(nil))
	q := v1.NewBlockQuerier(b, true, v1.DefaultMaxPageSize)

	md, err := q.Metadata()
	if err != nil {
		panic(err)
	}

	fmt.Printf("Metadata: %+v\n", md)

	for q.Next() {
		swb := q.At()
		fmt.Printf("%s (%d)\n", swb.Series.Fingerprint, swb.Series.Chunks.Len())
	}
	if q.Err() != nil {
		fmt.Printf("error: %s\n", q.Err())
	}
}
