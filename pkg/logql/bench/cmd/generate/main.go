package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/grafana/loki/v3/pkg/logql/bench"
)

func main() {
	var (
		size     = flag.Int64("size", 1073741824, "Size in bytes to generate")
		dir      = flag.String("dir", "data", "Output directory")
		tenantID = flag.String("tenant", "test-tenant", "Tenant ID")
	)
	flag.Parse()

	// Clean the store-specific output directory
	if err := os.RemoveAll(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to clear output directory: %v\n", err)
		os.Exit(1)
	}

	// Create the store
	store, err := bench.NewDataObjStore(*dir, *tenantID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create store: %v\n", err)
		os.Exit(1)
	}

	// Create builder with default options
	builder := bench.NewBuilder(store, bench.DefaultOpt())

	// Generate the data
	ctx := context.Background()
	if err := builder.Generate(ctx, *size); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate dataset: %v\n", err)
		os.Exit(1)
	}
}
