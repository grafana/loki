package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/grafana/loki/v3/pkg/logql/bench"
)

func main() {
	if len(os.Args) != 5 || os.Args[1] != "-size" || os.Args[3] != "-output" {
		fmt.Fprintf(os.Stderr, "Usage: %s -size <size_in_bytes> -output <output_file>\n", os.Args[0])
		os.Exit(1)
	}

	size, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid size: %v\n", err)
		os.Exit(1)
	}

	outputFile := os.Args[4]
	if err := bench.GenerateDataset(size, outputFile); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate dataset: %v\n", err)
		os.Exit(1)
	}
}
