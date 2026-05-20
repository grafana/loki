package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/loghttp"
	bench "github.com/grafana/loki/v3/pkg/logql/bench"
	discover "github.com/grafana/loki/v3/pkg/logql/bench/discover/pkg"
	discovertsdb "github.com/grafana/loki/v3/pkg/logql/bench/discover/pkg/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

// storageOpts bundles the CLI flags needed by the storage-mode pipeline
// beyond the core StorageConfig. These are passed through from run() to
// runStorageMode so the function can build content-probe clients and invoke
// assembly/save/validate.
type storageOpts struct {
	address         string
	username        string
	password        string
	bearerToken     string
	bearerTokenFile string
	tenant          string
	concurrency     int
	output          string
	queriesDir      string
	suites          []bench.Suite
	selector        string
	maxStreams      int
}

var (
	newIndexStorageClient        = discovertsdb.NewIndexStorageClient
	discoverAndDownloadIndexesFn = discovertsdb.DiscoverAndDownloadIndexes
	openAndInspectIndexesFn      = discovertsdb.OpenAndInspectIndexes
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// classifyClientAdapter adapts logcli.DefaultClient to bench.ClassifyAPI.
// DefaultClient.GetDetectedFields has a more complex signature (fieldName,
// lineLimit, step, quiet) than the simplified ClassifyAPI interface, so this
// adapter fills in sensible defaults for the extra parameters.
type classifyClientAdapter struct {
	c *client.DefaultClient
}

func (a *classifyClientAdapter) GetDetectedFields(
	queryStr string,
	limit int,
	start, end time.Time,
) (*loghttp.DetectedFieldsResponse, error) {
	return a.c.GetDetectedFields(
		queryStr, // queryStr
		"",       // fieldName (empty = all fields)
		limit,    // fieldLimit
		200,      // lineLimit
		start,    // start
		end,      // end
		0,        // step
		true,     // quiet
	)
}

// envOr returns the value of the environment variable named by key,
// or fallback if the variable is unset or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// run parses the provided arguments and invokes runStorageMode. It returns an
// exit code: 0 on success, 1 on fatal startup/config failure.
//
// Using a fresh FlagSet per invocation makes run() safe to call multiple times
// in tests without hitting "flag redefined" panics.
//
// Partial discovery/classification failures (per-query errors) are surfaced as
// warnings in the summary on stderr and do not produce a non-zero exit code.
func run(args []string) int {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	storageCfg := discovertsdb.StorageConfig{Tenant: envOr("LOKI_ORG_ID", "")}
	storageCfg.RegisterFlags(fs)

	var (
		address         = fs.String("address", envOr("LOKI_ADDR", ""), "Loki base URL for content probes, e.g. http://localhost:3100. Required. Can also be set using LOKI_ADDR env var.")
		username        = fs.String("username", envOr("LOKI_USERNAME", ""), "Username for HTTP basic auth (Grafana Cloud public endpoint). Can also be set using LOKI_USERNAME env var.")
		password        = fs.String("password", envOr("LOKI_PASSWORD", ""), "Password for HTTP basic auth (Grafana Cloud public endpoint). Can also be set using LOKI_PASSWORD env var.")
		bearerToken     = fs.String("bearer-token", envOr("LOKI_BEARER_TOKEN", ""), "Bearer token added to Authorization header. Can also be set using LOKI_BEARER_TOKEN env var.")
		bearerTokenFile = fs.String("bearer-token-file", envOr("LOKI_BEARER_TOKEN_FILE", ""), "File containing bearer token added to Authorization header. Can also be set using LOKI_BEARER_TOKEN_FILE env var.")
		fromStr         = fs.String("from", "", "Query start time (RFC3339). Defaults to 24h before --to.")
		toStr           = fs.String("to", "", "Query end time (RFC3339). Defaults to now.")
		output          = fs.String("output", "", "Directory where dataset_metadata.json will be written. Empty means do not write.")
		maxStreams      = fs.Int("max-streams", discover.DefaultMaxStreams, "Maximum number of streams to include in output.")
		queriesDir      = fs.String("queries-dir", "", "Directory containing LogQL query YAML files. Empty means skip validation.")
		concurrency     = fs.Int("concurrency", discover.DefaultParallelism, "Maximum concurrent API calls for keyword and range probing (default 5).")
		suitesStr       = fs.String("suites", "", "Comma-separated suites to validate: fast,regression,exhaustive (default: all)")
		selector        = fs.String("selector", "", `Required label matchers used to scope stream discovery and keyword probing, without outer braces. Examples: 'namespace=~"loki-ops-002|mimir-ops-03"' or 'namespace=~"loki.*", cluster="prod-us-central-0"'.`)
		tablePrefix     = fs.String("table-prefix", "", `TSDB index table name prefix (e.g. "loki_dev_005_tsdb_index_"). Defaults to "index_". Must match the schema_config.configs[].index.prefix in the target Loki deployment.`)
	)

	if err := fs.Parse(args); err != nil {
		return 1
	}

	// Parse suites.
	var suites []bench.Suite
	if *suitesStr == "" {
		suites = []bench.Suite{bench.SuiteFast, bench.SuiteRegression, bench.SuiteExhaustive}
	} else {
		for _, s := range strings.Split(*suitesStr, ",") {
			suites = append(suites, bench.Suite(strings.TrimSpace(s)))
		}
	}

	if err := validateStorageConfig(storageCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fs.Usage()
		return 1
	}

	if *address == "" {
		fmt.Fprintln(os.Stderr, "Error: --address is required (or set LOKI_ADDR).")
		fs.Usage()
		return 1
	}
	if *selector == "" {
		fmt.Fprintln(os.Stderr, "Error: --selector is required.")
		fs.Usage()
		return 1
	}

	// Parse optional time range flags.
	var from, to time.Time
	if *fromStr != "" {
		t, err := time.Parse(time.RFC3339, *fromStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --from value %q: %v\n", *fromStr, err)
			return 1
		}
		from = t
	}
	if *toStr != "" {
		t, err := time.Parse(time.RFC3339, *toStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid --to value %q: %v\n", *toStr, err)
			return 1
		}
		to = t
	}

	return runAgainstStorage(storageCfg, from, to, *tablePrefix, storageOpts{
		address:         *address,
		username:        *username,
		password:        *password,
		bearerToken:     *bearerToken,
		bearerTokenFile: *bearerTokenFile,
		tenant:          storageCfg.Tenant,
		concurrency:     *concurrency,
		output:          *output,
		queriesDir:      *queriesDir,
		suites:          suites,
		selector:        *selector,
		maxStreams:      *maxStreams,
	})
}

func runAgainstStorage(storageCfg discovertsdb.StorageConfig, from, to time.Time, tablePrefix string, opts storageOpts) int {
	idxClient, err := newIndexStorageClient(storageCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create index storage client: %v\n", err)
		return 1
	}
	defer idxClient.Stop()

	tmpDir, err := os.MkdirTemp("", "loki-discover-tsdb-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create temp dir: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmpDir)

	downloadResult, err := discoverAndDownloadIndexesFn(context.Background(), idxClient, discovertsdb.DownloadConfig{
		Tenant:      storageCfg.Tenant,
		From:        from,
		To:          to,
		TmpDir:      tmpDir,
		TablePrefix: tablePrefix,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: storage index discovery/download failed: %v\n", err)
		return 1
	}

	paths := make([]string, 0, len(downloadResult.Files))
	tables := map[string]struct{}{}
	for _, file := range downloadResult.Files {
		paths = append(paths, file.LocalPath)
		tables[file.Table] = struct{}{}
	}

	inspectResult, err := openAndInspectIndexesFn(paths)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: storage index open/inspect failed: %v\n", err)
		return 1
	}
	defer func() {
		for _, r := range inspectResult {
			if r.Index != nil {
				_ = r.Index.Close()
			}
		}
	}()

	fmt.Fprintln(os.Stderr, "\n--- Storage Discovery Summary ---")
	fmt.Fprintf(os.Stderr, "Tables scanned: %d\n", len(tables))
	fmt.Fprintf(os.Stderr, "Files downloaded: %d\n", len(downloadResult.Files))
	if len(inspectResult) > 0 {
		minBound := inspectResult[0].Bounds[0]
		maxBound := inspectResult[0].Bounds[1]
		for _, r := range inspectResult[1:] {
			if r.Bounds[0] < minBound {
				minBound = r.Bounds[0]
			}
			if r.Bounds[1] > maxBound {
				maxBound = r.Bounds[1]
			}
		}
		fmt.Fprintf(os.Stderr, "Opened indexes: %d (bounds %s -> %s)\n", len(inspectResult), minBound.Time().UTC().Format(time.RFC3339), maxBound.Time().UTC().Format(time.RFC3339))
	} else {
		fmt.Fprintln(os.Stderr, "Opened indexes: 0")
	}

	// No indexes → nothing to discover. Return early with structural-only summary.
	if len(paths) == 0 {
		return 0
	}

	indexes := make([]tsdb.Index, 0, len(inspectResult))
	for _, r := range inspectResult {
		indexes = append(indexes, r.Index)
	}

	tsdbCfg := discovertsdb.StructuralConfig{
		UserID:     storageCfg.Tenant,
		From:       from,
		To:         to,
		MaxStreams: opts.maxStreams,
		Selector:   opts.selector,
		ProgressWriter: func(rawCount, uniqueCount int) {
			fmt.Fprintf(os.Stderr, "\rEnumerating series... %d raw, %d unique", rawCount, uniqueCount)
		},
	}
	fmt.Fprintln(os.Stderr, "\nEnumerating series from TSDB indexes...")
	tsdbResult, err := discovertsdb.RunStructuralDiscovery(tsdbCfg, indexes)
	fmt.Fprintln(os.Stderr) // newline after progress
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: TSDB structural discovery failed: %v\n", err)
		return 1
	}

	// Derive range metadata from TSDB chunk evidence.
	rangeResult := discovertsdb.RunRangeDerivation(tsdbResult.MergedStreams)

	// Print structural discovery summary.
	fmt.Fprintf(os.Stderr, "\n--- Structural Discovery ---\n")
	fmt.Fprintf(os.Stderr, "Total streams selected: %d (from %d unique, %d raw)\n",
		tsdbResult.TotalSelected, tsdbResult.TotalUnique, tsdbResult.TotalRaw)

	if len(tsdbResult.ByServiceName) > 0 {
		fmt.Fprintln(os.Stderr, "Per service:")
		svcNames := make([]string, 0, len(tsdbResult.ByServiceName))
		for svc := range tsdbResult.ByServiceName {
			svcNames = append(svcNames, svc)
		}
		sort.Strings(svcNames)
		for _, svc := range svcNames {
			fmt.Fprintf(os.Stderr, "  %s: %d\n", svc, len(tsdbResult.ByServiceName[svc]))
		}
	}

	// ── Content Probes ─────────────────────────────────────────────────────
	fmt.Fprintln(os.Stderr, "\nProbing content... classification → keywords")

	// Build logcli DefaultClient for content probe API calls.
	loki := &client.DefaultClient{
		Address:         opts.address,
		OrgID:           opts.tenant,
		Username:        opts.username,
		Password:        opts.password,
		BearerToken:     opts.bearerToken,
		BearerTokenFile: opts.bearerTokenFile,
	}

	// Build classify client with categorize-labels Tripperware.
	classifyLoki := *loki
	classifyLoki.Tripperware = func(rt http.RoundTripper) http.RoundTripper {
		return &discover.CategorizeLabelsTripperware{Wrapped: rt}
	}

	// Default time range for content probes: when --from/--to are not
	// specified, use a 24h window ending at now (same as Config).
	// Zero times would cause the detected_fields API to receive epoch-0
	// timestamps and return empty results.
	probeFrom, probeTo := from, to
	if probeTo.IsZero() {
		probeTo = time.Now().UTC()
	}
	if probeFrom.IsZero() {
		probeFrom = probeTo.Add(-24 * time.Hour)
	}
	probeCfg := discover.ProbeConfig{
		Parallelism:   opts.concurrency,
		From:          probeFrom,
		To:            probeTo,
		BroadSelector: opts.selector,
	}

	probeResult, err := discover.RunContentProbes(probeCfg, tsdbResult, &classifyClientAdapter{c: &classifyLoki}, loki)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: content probes failed: %v\n", err)
		return 1
	}

	classifyResult := probeResult.Classify
	keywordResult := probeResult.Keywords

	// Print classification summary.
	fmt.Fprintf(os.Stderr, "\n--- Classification Summary ---\n")
	fmt.Fprintf(os.Stderr, "Classification complete: %d classified, %d skipped\n",
		classifyResult.TotalClassified, classifyResult.TotalSkipped)
	for _, format := range []bench.LogFormat{bench.LogFormatJSON, bench.LogFormatLogfmt, bench.LogFormatUnstructured} {
		fmt.Fprintf(os.Stderr, "  %s: %d streams\n", format, len(classifyResult.ByFormat[format]))
	}
	if len(classifyResult.Warnings) > 0 {
		fmt.Fprintf(os.Stderr, "  warnings: %d\n", len(classifyResult.Warnings))
	}

	// Print keyword summary.
	fmt.Fprintf(os.Stderr, "\n--- Keyword Summary ---\n")
	fmt.Fprintf(os.Stderr, "Keywords probed: %d pairs, %d skipped\n",
		keywordResult.TotalProbed, keywordResult.TotalSkipped)
	fmt.Fprintf(os.Stderr, "Keywords found: %d unique keywords across streams\n",
		len(keywordResult.ByKeyword))
	if len(keywordResult.Warnings) > 0 {
		fmt.Fprintf(os.Stderr, "  warnings: %d\n", len(keywordResult.Warnings))
	}

	// ── Assembly ───────────────────────────────────────────────────────────
	cfg := discover.Config{
		From: from,
		To:   to,
	}

	fmt.Fprintln(os.Stderr, "\nAssembling metadata...")
	metadata := discover.AssembleMetadata(tsdbResult, classifyResult, keywordResult, rangeResult, cfg)
	fmt.Fprintf(os.Stderr, "Assembly complete: %d streams, %d selectors with range metadata\n",
		metadata.Statistics.TotalStreams, len(metadata.MetadataBySelector))

	// ── Save ───────────────────────────────────────────────────────────────
	if opts.output != "" {
		if err := os.MkdirAll(opts.output, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to create output directory %q: %v\n", opts.output, err)
			return 1
		}
		if err := bench.SaveMetadata(opts.output, metadata); err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to write metadata: %v\n", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "Metadata written to %s/dataset_metadata.json\n", opts.output)
	}

	// ── Validation ─────────────────────────────────────────────────────────
	if opts.queriesDir == "" {
		fmt.Fprintln(os.Stderr, "\nWarning: --queries-dir not specified; skipping validation.")
		return 0
	}

	fmt.Fprintln(os.Stderr, "\nRunning validation...")
	validationResult, err := discover.RunValidation(metadata, opts.queriesDir, opts.suites)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: validation failed: %v\n", err)
		return 1
	}

	printValidationReport(os.Stderr, validationResult)

	if validationResult.FailedQueries > 0 {
		return 1
	}
	return 0
}

// validateStorageConfig validates that the required storage configuration is
// present.
func validateStorageConfig(storageCfg discovertsdb.StorageConfig) error {
	if strings.TrimSpace(string(storageCfg.StorageType)) == "" {
		return fmt.Errorf("--storage-type is required")
	}
	return storageCfg.NormalizeAndValidate()
}

// printValidationReport writes the three-section validation report to w.
func printValidationReport(w io.Writer, r *discover.ValidationResult) {
	fmt.Fprintln(w, "\n=== Validation Report ===")

	// Section 1: Resolution results summary.
	fmt.Fprintln(w, "\nResolution Results:")
	fmt.Fprintf(w, "  Total queries:  %d", r.TotalQueries)
	for _, suite := range []bench.Suite{bench.SuiteFast, bench.SuiteRegression, bench.SuiteExhaustive} {
		if s, ok := r.BySuite[suite]; ok && s.Total > 0 {
			fmt.Fprintf(w, " (%s: %d)", suite, s.Total)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Passed:         %d\n", r.PassedQueries)
	fmt.Fprintf(w, "  Failed:         %d\n", r.FailedQueries)

	// Section 2: Failed queries with root causes.
	if r.FailedQueries > 0 {
		fmt.Fprintln(w, "\nFailed Queries:")
		for _, res := range r.Results {
			if res.Err == nil {
				continue
			}
			fmt.Fprintf(w, "  [%s] %q\n", res.Suite, res.Definition.Description)
			fmt.Fprintf(w, "    Source: %s\n", res.Definition.Source)
			fmt.Fprintf(w, "    Error:  %v\n", res.Err)
		}
	}

	// Section 3: Unreferenced bounded set members.
	fmt.Fprintln(w, "\nUnreferenced Bounded Set Members:")
	bySet := make(map[string][]string)
	for _, u := range r.UnreferencedKeys {
		bySet[u.BoundedSet] = append(bySet[u.BoundedSet], u.Key)
	}
	for _, setName := range []string{"keywords", "unwrappable_fields", "structured_metadata", "label_keys"} {
		keys := bySet[setName]
		if len(keys) == 0 {
			fmt.Fprintf(w, "  %s: (none — all members referenced)\n", setName)
		} else {
			fmt.Fprintf(w, "  %s: %q\n", setName, keys)
		}
	}
}
