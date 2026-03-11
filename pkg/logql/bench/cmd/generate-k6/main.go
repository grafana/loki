package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	bench "github.com/grafana/loki/v3/pkg/logql/bench"
)

func main() {
	os.Exit(run())
}

func run() int {
	metadataDir := flag.String("metadata-dir", "testdata", "Directory containing dataset_metadata.json")
	queriesDir := flag.String("queries-dir", "queries", "Directory containing query YAML suites")
	tenantID := flag.Int("tenant-id", 0, "Tenant ID to embed in test cases (required)")
	outputDir := flag.String("output-dir", "testdata", "Directory to write output JSON files")
	seed := flag.Int64("seed", time.Now().UnixNano(), "Random seed for reproducible resolution")
	buffer := flag.Duration("buffer", 4*time.Hour, "Offset from 'now' for query window end")
	flag.Parse()

	if *tenantID == 0 {
		log.Println("ERROR: --tenant-id is required")
		flag.Usage()
		return 1
	}

	// Load metadata
	metadata, err := bench.LoadMetadata(*metadataDir)
	if err != nil {
		log.Printf("ERROR: failed to load metadata from %s: %v", *metadataDir, err)
		return 1
	}

	// Load query registry (all suites)
	registry := bench.NewQueryRegistry(*queriesDir)
	if err := registry.Load(bench.SuiteFast, bench.SuiteRegression, bench.SuiteExhaustive); err != nil {
		log.Printf("ERROR: failed to load query registry from %s: %v", *queriesDir, err)
		return 1
	}

	allDefs := registry.GetQueries(false) // all loaded suites, exclude skipped

	// Generate range test cases
	rangeCases, rangeErrors := generateCases(registry, allDefs, metadata, *seed, *tenantID, *buffer, false)

	// Generate instant test cases (metric queries only)
	var metricDefs []bench.QueryDefinition
	for _, def := range allDefs {
		if def.Kind == "metric" {
			metricDefs = append(metricDefs, def)
		}
	}
	instantCases, instantErrors := generateCases(registry, metricDefs, metadata, *seed, *tenantID, *buffer, true)

	// Write output files
	rangePath := filepath.Join(*outputDir, "k6_test_cases_range.json")
	if err := writeJSON(rangePath, rangeCases); err != nil {
		log.Printf("ERROR: failed to write range file: %v", err)
		return 1
	}

	instantPath := filepath.Join(*outputDir, "k6_test_cases_instant.json")
	if err := writeJSON(instantPath, instantCases); err != nil {
		log.Printf("ERROR: failed to write instant file: %v", err)
		return 1
	}

	log.Printf("Range:   %d cases generated, %d failed → %s", len(rangeCases), len(rangeErrors), rangePath)
	log.Printf("Instant: %d cases generated, %d failed → %s", len(instantCases), len(instantErrors), instantPath)

	// Fail if any queries could not be resolved
	if len(rangeErrors) > 0 || len(instantErrors) > 0 {
		log.Println("ERROR: some queries could not be resolved against the metadata:")
		for _, e := range rangeErrors {
			log.Printf("  [range]   %s", e)
		}
		for _, e := range instantErrors {
			log.Printf("  [instant] %s", e)
		}
		log.Println("Re-run `make discover` to regenerate metadata, or fix the query requirements.")
		return 1
	}

	return 0
}

func generateCases(
	registry *bench.QueryRegistry,
	defs []bench.QueryDefinition,
	metadata *bench.DatasetMetadata,
	seed int64,
	tenantID int,
	buffer time.Duration,
	isInstant bool,
) ([]bench.K6TestCase, []string) {
	resolver := bench.NewMetadataVariableResolver(metadata, seed)
	var cases []bench.K6TestCase
	var errors []string

	for _, def := range defs {
		expanded, err := registry.ExpandQuery(def, resolver, isInstant)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%q (%s): %v", def.Description, def.Source, err))
			continue
		}

		length, err := def.TimeRange.ParseLength()
		if err != nil {
			errors = append(errors, fmt.Sprintf("%q (%s): invalid length: %v", def.Description, def.Source, err))
			continue
		}

		for _, tc := range expanded {
			cases = append(cases, bench.ConvertTestCaseToK6(tc, tenantID, length, buffer))
		}
	}

	return cases, errors
}

func writeJSON(path string, data interface{}) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}
