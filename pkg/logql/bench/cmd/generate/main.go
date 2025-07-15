package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/grafana/loki/v3/pkg/logql/bench"
)

func main() {
	var (
		size        = flag.Int64("size", 2147483648, "Size in bytes to generate")
		dir         = flag.String("dir", "data", "Output directory")
		tenantIDs   = flag.String("tenant", "test-tenant", "Comma separated list of tenant IDs")
		clearFolder = flag.Bool("clear", true, "Clear output directory before generating data")
	)
	flag.Parse()

	// Clean the output directory if requested
	if *clearFolder {
		if err := os.RemoveAll(*dir); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to clear output directory: %v\n", err)
			os.Exit(1)
		}
	}

	tenantIDsSlice := strings.Split(*tenantIDs, ",")

	var stores []bench.Store
	for _, tenantID := range tenantIDsSlice {
		// Create stores
		dataObjStore, err := bench.NewDataObjStore(*dir, tenantID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create dataobj store: %v\n", err)
			os.Exit(1)
		}
		stores = append(stores, dataObjStore)
	}
	multiTenantStore, err := bench.NewMultiTenantDataObjStore(*dir, tenantIDsSlice)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create dataobj store: %v\n", err)
		os.Exit(1)
	}
	stores = append(stores, multiTenantStore)

	// Create builder with default options and the store
	builder := bench.NewBuilder(*dir, bench.DefaultOpt(), stores...)

	// Generate the data
	ctx := context.Background()
	if err := builder.Generate(ctx, *size); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to generate dataset: %v\n", err)
		os.Exit(1)
	}
}
