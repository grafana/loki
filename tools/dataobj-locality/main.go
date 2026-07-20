package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

func main() {
	app := kingpin.New("dataobj-locality", "Measure label clustering depth across postings sections and logs-section locality over a time range.")
	report := &localityCommand{}
	serve := &serveCommand{}
	reportCmd := app.Command("report", "Run a one-shot locality report for one tenant.")
	serveCmd := app.Command("serve", "Continuously scan locality windows and expose Prometheus metrics.")
	registerReportFlags(reportCmd, report)
	registerServeFlags(serveCmd, serve)

	selected, err := app.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var runErr error
	switch selected {
	case reportCmd.FullCommand():
		runErr = report.run()
	case serveCmd.FullCommand():
		runErr = serve.run()
	default:
		runErr = fmt.Errorf("expected a subcommand")
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
}

// localityCommand holds the parsed flags for the one-shot report command.
type localityCommand struct {
	lokiConfigFile         *string
	expandEnv              *bool
	dir                    *string
	tenant                 *string
	from                   *string
	to                     *string
	locality               *string
	top                    *int
	sortKey                *string
	logsSectionTargetBytes *int64
	concurrency            *int
	exportPath             *string
	exportFormat           *string
}

func (cmd *localityCommand) run() error {
	// Scans over a large fleet can take minutes; let Ctrl-C/SIGTERM unwind the
	// errgroup and flush the export sink instead of killing the process outright.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if *cmd.tenant == "" {
		return fmt.Errorf("--tenant is required for report")
	}

	// Diagnostics go to stderr so stdout stays clean for the report (or a
	// redirected `> report.txt`).
	logger := log.NewLogfmtLogger(os.Stderr)

	from, to, err := parseTimeRange(*cmd.from, *cmd.to)
	if err != nil {
		return err
	}

	opts, err := parseMode(*cmd.locality)
	if err != nil {
		return err
	}
	opts.sortKey = *cmd.sortKey
	opts.logsSectionTargetBytes = *cmd.logsSectionTargetBytes
	opts.tenant = *cmd.tenant
	if opts.logsLocality && opts.sortKey == "" {
		return fmt.Errorf("--sort-key must be set when --locality includes logs")
	}

	b, mCfg, err := cmd.buildBucket(ctx, logger)
	if err != nil {
		return err
	}
	src := newIndexObjectSource(b, mCfg, *cmd.tenant, from, to, logger, *cmd.concurrency)

	c := newCollector(opts)

	if *cmd.exportPath != "" {
		sink, err := newFactSink(*cmd.exportPath, *cmd.exportFormat)
		if err != nil {
			return fmt.Errorf("opening export sink: %w", err)
		}
		c.sink = sink
		defer func() {
			if cerr := sink.close(); cerr != nil {
				level.Error(logger).Log("msg", "closing export sink", "err", cerr)
			}
		}()
		level.Info(logger).Log(
			"msg", "fact export enabled",
			"prefix", *cmd.exportPath,
			"format", *cmd.exportFormat,
		)
	}

	level.Info(logger).Log("msg", "collecting locality", "tenant", *cmd.tenant, "from", from.Format(time.RFC3339), "to", to.Format(time.RFC3339))
	start := time.Now()
	if err := c.collect(ctx, src); err != nil {
		return err
	}
	level.Info(logger).Log("msg", "collection complete", "elapsed", time.Since(start).Round(time.Millisecond))

	c.report(os.Stdout, *cmd.top)
	return nil
}

// buildBucket constructs the bucket and metastore configuration from the
// mutually-exclusive --dir (local) or --config.file (bucket) flags.
func (cmd *localityCommand) buildBucket(ctx context.Context, logger log.Logger) (objstore.Bucket, metastore.Config, error) {
	var (
		b    objstore.Bucket
		mCfg metastore.Config
	)

	switch {
	case *cmd.dir != "":
		if *cmd.lokiConfigFile != "" {
			return nil, metastore.Config{}, fmt.Errorf("--dir cannot be combined with --config.file")
		}
		fsBucket, err := filesystem.NewBucket(*cmd.dir)
		if err != nil {
			return nil, metastore.Config{}, fmt.Errorf("opening local directory %s: %w", *cmd.dir, err)
		}
		b = fsBucket
		// Use the same defaults as metastore.Config.RegisterFlagsWithPrefix.
		// --dir assumes no StorageBucketPrefix is in play.
		mCfg = metastore.Config{IndexStoragePrefix: "index/v0", PartitionRatio: 10}

	case *cmd.lokiConfigFile != "":
		var err error
		b, mCfg, err = buildBucketFromLokiConfig(ctx, *cmd.lokiConfigFile, *cmd.expandEnv, logger)
		if err != nil {
			return nil, metastore.Config{}, err
		}

	default:
		return nil, metastore.Config{}, fmt.Errorf("one of --dir or --config.file must be provided")
	}

	return b, mCfg, nil
}

// newIndexObjectSource creates a source for one time range over a shared
// bucket, allowing serve to avoid recreating clients for every window.
func newIndexObjectSource(b objstore.Bucket, mCfg metastore.Config, tenant string, from, to time.Time, logger log.Logger, concurrency int) *indexObjectSource {
	return &indexObjectSource{
		rawBucket:    b,
		metastoreCfg: mCfg,
		tenant:       tenant,
		from:         from,
		to:           to,
		logger:       logger,
		concurrency:  concurrency,
	}
}

// parseTimeRange resolves the from/to flags, defaulting to the last 12h.
func parseTimeRange(fromStr, toStr string) (from, to time.Time, err error) {
	now := time.Now().UTC()
	to = now
	if toStr != "" {
		to, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parsing --to: %w", err)
		}
	}
	from = to.Add(-12 * time.Hour)
	if fromStr != "" {
		from, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("parsing --from: %w", err)
		}
	}
	if !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("--from (%s) must be before --to (%s)", from, to)
	}
	return from, to, nil
}

// parseMode maps the --locality flag to collector options.
func parseMode(locality string) (collectorOptions, error) {
	switch locality {
	case "postings":
		return collectorOptions{byNameValue: true, byName: true}, nil
	case "logs":
		return collectorOptions{logsLocality: true}, nil
	case "both", "":
		return collectorOptions{byNameValue: true, byName: true, logsLocality: true}, nil
	default:
		return collectorOptions{}, fmt.Errorf("invalid --locality value %q (want postings|logs|both)", locality)
	}
}

// registerReportFlags registers the existing one-shot report flags.
func registerReportFlags(app *kingpin.CmdClause, cmd *localityCommand) {
	cmd.lokiConfigFile = app.Flag("config.file", "Path to a Loki config YAML (bucket source). Storage credentials and prefixes are derived from the config, mirroring the engine.").String()
	cmd.expandEnv = app.Flag("config.expand-env", "Expand ${VAR} references in the Loki config file.").Default("false").Bool()
	cmd.dir = app.Flag("dir", "Local directory holding TOCs and index objects (local source). Mutually exclusive with --config.file.").String()
	cmd.tenant = app.Flag("tenant", "Tenant whose sections are inspected.").String()
	cmd.from = app.Flag("from", "Start of the time range (RFC3339). Defaults to --to minus 12h.").String()
	cmd.to = app.Flag("to", "End of the time range (RFC3339). Defaults to now.").String()
	cmd.locality = app.Flag("locality", "Measure locality for postings section, logs section, or both.").Default("both").Enum("postings", "logs", "both")
	cmd.top = app.Flag("top", "Number of rows to print in each top-N table.").Default("20").Int()
	cmd.sortKey = app.Flag("sort-key", "Logs section locality is measured as how clustered or inversely how spread out the provided sort-key is.").Default("service_name").String()
	cmd.logsSectionTargetBytes = app.Flag("logs-section-target-bytes", "Uncompressed logs-section target size in bytes, used to compute ideal section count.").Default("134217728").Int64()
	cmd.concurrency = app.Flag("concurrency", "Maximum number of index objects to open and scan in parallel.").Default("8").Int()
	cmd.exportPath = app.Flag("export", "Path prefix for raw-fact export. Writes <prefix>.parquet and/or <prefix>.csv with one row per label posting. Omit to skip export.").Default("").String()
	cmd.exportFormat = app.Flag("export-format", "Output format for --export: parquet, csv, or both.").Default("both").Enum("parquet", "csv", "both")
}
