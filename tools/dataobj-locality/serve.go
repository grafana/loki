package main

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

type serveCommand struct {
	source               localityCommand
	period               *time.Duration
	lookback             *time.Duration
	metricsListenAddress *string
}

func registerServeFlags(app *kingpin.CmdClause, cmd *serveCommand) {
	cmd.source.lokiConfigFile = app.Flag("config.file", "Path to a Loki config YAML (bucket source). Storage credentials and prefixes are derived from the config, mirroring the engine.").String()
	cmd.source.expandEnv = app.Flag("config.expand-env", "Expand ${VAR} references in the Loki config file.").Default("false").Bool()
	cmd.source.dir = app.Flag("dir", "Local directory holding TOCs and index objects (local source). Mutually exclusive with --config.file.").String()
	cmd.source.tenant = app.Flag("tenant", "Tenant to inspect. Omit to report all tenants.").String()
	cmd.source.locality = app.Flag("locality", "Measure locality for postings section, logs section, or both.").Default("both").Enum("postings", "logs", "both")
	cmd.source.sortKey = app.Flag("sort-key", "Logs section locality is measured as how clustered or inversely how spread out the provided sort-key is.").Default("service_name").String()
	cmd.source.logsSectionTargetBytes = app.Flag("logs-section-target-bytes", "Uncompressed logs-section target size in bytes, used to compute ideal section count.").Default("134217728").Int64()
	cmd.source.concurrency = app.Flag("concurrency", "Maximum number of index objects to open and scan in parallel.").Default("8").Int()
	cmd.period = app.Flag("period", "UTC-aligned interval between scans.").Default("3h").Duration()
	cmd.lookback = app.Flag("lookback", "Window history to scan on every run.").Default("168h").Duration()
	cmd.metricsListenAddress = app.Flag("metrics.listen-address", "Address on which to serve Prometheus metrics.").Default(":9900").String()
}

func (cmd *serveCommand) run() error {
	if *cmd.period <= 0 {
		return fmt.Errorf("--period must be positive")
	}
	if *cmd.lookback <= 0 {
		return fmt.Errorf("--lookback must be positive")
	}

	opts, err := parseMode(*cmd.source.locality)
	if err != nil {
		return err
	}
	opts.sortKey = *cmd.source.sortKey
	opts.logsSectionTargetBytes = *cmd.source.logsSectionTargetBytes
	if opts.logsLocality && opts.sortKey == "" {
		return fmt.Errorf("--sort-key must be set when --locality includes logs")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := log.NewLogfmtLogger(os.Stderr)
	bucket, metastoreCfg, err := cmd.source.buildBucket(ctx, logger)
	if err != nil {
		return err
	}

	collector := newLocalityCollector()
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)
	server := &http.Server{
		Addr:              *cmd.metricsListenAddress,
		Handler:           promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		level.Info(logger).Log("msg", "serving locality metrics", "address", *cmd.metricsListenAddress)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			level.Error(logger).Log("msg", "shutting down metrics server", "err", err)
		}
	}()

	for {
		if err := cmd.scan(ctx, bucket, metastoreCfg, opts, collector, logger); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			level.Error(logger).Log("msg", "locality scan failed", "err", err)
		}

		timer := time.NewTimer(time.Until(nextTick(time.Now().UTC(), *cmd.period)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case err := <-serverErrors:
			timer.Stop()
			return fmt.Errorf("serving metrics: %w", err)
		case <-timer.C:
		}
	}
}

func (cmd *serveCommand) scan(ctx context.Context, bucket objstore.Bucket, metastoreCfg metastore.Config, opts collectorOptions, metricCollector *localityCollector, logger log.Logger) error {
	started := time.Now()
	snapshot := localitySnapshot{windows: make(map[string]windowScanSnapshot)}
	validWindows := make(map[string]struct{})
	for _, window := range localityWindows(started.UTC(), *cmd.lookback) {
		windowSnapshot, err := cmd.scanWindow(ctx, bucket, metastoreCfg, opts, window, logger)
		if err != nil {
			return fmt.Errorf("scanning window %s: %w", window.Format(time.RFC3339), err)
		}
		validWindows[windowSnapshot.window] = struct{}{}
		snapshot.windows[windowSnapshot.window] = windowSnapshot
		metricCollector.setWindow(windowSnapshot.window, windowSnapshot)
	}
	metricCollector.completeScan(validWindows, time.Since(started))
	postingsP95, logsP95 := snapshotHeadlinePercentiles(snapshot)
	compacted, uncompacted := snapshotClassTotals(snapshot)
	compactedSections, uncompactedSections := snapshotClassSections(snapshot)
	postingsDistributions, logsDistributions := snapshotDistributionCounts(snapshot)
	level.Info(logger).Log(
		"msg", "locality scan complete",
		"observed_at", time.Now().UTC().Format(time.RFC3339),
		"windows", len(validWindows),
		"postings_distributions", postingsDistributions,
		"logs_distributions", logsDistributions,
		"postings_spread_p95_upper_bound", postingsP95,
		"logs_spread_factor_p95_upper_bound", logsP95,
		"compacted_objects", compacted.objects,
		"compacted_bytes", compacted.bytes,
		"compacted_postings_sections", compactedSections,
		"uncompacted_objects", uncompacted.objects,
		"uncompacted_bytes", uncompacted.bytes,
		"uncompacted_postings_sections", uncompactedSections,
		"elapsed", time.Since(started).Round(time.Millisecond),
	)
	return nil
}

func snapshotHeadlinePercentiles(snapshot localitySnapshot) (float64, float64) {
	var postings, logs histogramSnapshot
	for _, window := range snapshot.windows {
		for _, distribution := range window.postings {
			postings = mergeHistogramSnapshots(postings, distribution.spread)
		}
		for _, distribution := range window.logs {
			logs = mergeHistogramSnapshots(logs, distribution.spread)
		}
	}
	return histogramP95UpperBound(postings), histogramP95UpperBound(logs)
}

func snapshotDistributionCounts(snapshot localitySnapshot) (int, int) {
	var postings, logs int
	for _, window := range snapshot.windows {
		postings += len(window.postings)
		logs += len(window.logs)
	}
	return postings, logs
}

func snapshotClassTotals(snapshot localitySnapshot) (classTotals, classTotals) {
	var compacted, uncompacted classTotals
	for _, window := range snapshot.windows {
		for class, totals := range window.objects {
			if class == "compacted" {
				compacted.objects += totals.objects
				compacted.bytes += totals.bytes
			} else {
				uncompacted.objects += totals.objects
				uncompacted.bytes += totals.bytes
			}
		}
	}
	return compacted, uncompacted
}

func snapshotClassSections(snapshot localitySnapshot) (int64, int64) {
	var compacted, uncompacted int64
	for _, window := range snapshot.windows {
		for _, distribution := range window.postings {
			if distribution.class == "compacted" {
				compacted += distribution.sections
			} else {
				uncompacted += distribution.sections
			}
		}
	}
	return compacted, uncompacted
}

type windowScanSnapshot struct {
	window   string
	postings []postingsSnapshot
	logs     []logsSnapshot
	objects  map[string]classTotals
}

type windowCollectors struct {
	mu       sync.Mutex
	postings map[string]*collector
	logs     map[string]*collector
	paths    map[string]map[string]struct{}
}

func (cmd *serveCommand) scanWindow(ctx context.Context, bucket objstore.Bucket, metastoreCfg metastore.Config, opts collectorOptions, window time.Time, logger log.Logger) (windowScanSnapshot, error) {
	windowEnd := window.Add(metastore.MetastoreWindowSize).Add(-time.Nanosecond)
	source := newIndexObjectSource(bucket, metastoreCfg, *cmd.source.tenant, window, windowEnd, logger, *cmd.source.concurrency)
	groups := newWindowCollectors()

	err := source.each(ctx, func(sec *dataobj.Section, objPath string, sectionIdx int64) error {
		return groups.foldSection(ctx, sec, objPath, sectionIdx, opts)
	})
	if err != nil {
		return windowScanSnapshot{}, err
	}

	windowLabel := window.Format(time.RFC3339)
	result := windowScanSnapshot{window: windowLabel, objects: make(map[string]classTotals)}
	for key, c := range groups.postings {
		tenant, class := splitWindowClass(key)
		spreads := make([]float64, 0, len(c.nameValue))
		for _, agg := range c.nameValue {
			spreads = append(spreads, float64(agg.sectionSpread))
		}
		result.postings = append(result.postings, postingsSnapshot{
			window: windowLabel, tenant: tenant, class: class, sections: c.totalSections, spread: newHistogramSnapshot(spreads, postingsSpreadBuckets),
		})
	}
	for tenant, c := range groups.logs {
		result.logs = append(result.logs, logsSnapshotFromCollector(windowLabel, tenant, c))
	}
	for class, paths := range groups.paths {
		totals, err := objectTotals(ctx, source.indexBucket(), paths)
		if err != nil {
			return windowScanSnapshot{}, err
		}
		result.objects[class] = totals
	}
	return result, nil
}

func newWindowCollectors() *windowCollectors {
	return &windowCollectors{
		postings: make(map[string]*collector),
		logs:     make(map[string]*collector),
		paths:    make(map[string]map[string]struct{}),
	}
}

func (groups *windowCollectors) foldSection(ctx context.Context, sec *dataobj.Section, objPath string, sectionIdx int64, opts collectorOptions) error {
	class := "uncompacted"
	if isCompactedIndexPath(objPath) {
		class = "compacted"
	}
	tenant := sec.Tenant
	groups.mu.Lock()
	if groups.paths[class] == nil {
		groups.paths[class] = make(map[string]struct{})
	}
	groups.paths[class][objPath] = struct{}{}
	postingsCollector := groups.postings[tenant+"\x00"+class]
	if postingsCollector == nil && (opts.byNameValue || opts.byName) {
		postingsOpts := opts
		postingsOpts.logsLocality = false
		postingsCollector = newCollector(postingsOpts)
		groups.postings[tenant+"\x00"+class] = postingsCollector
	}
	logsCollector := groups.logs[tenant]
	if logsCollector == nil && opts.logsLocality {
		logsOpts := opts
		logsOpts.byNameValue = false
		logsOpts.byName = false
		logsCollector = newCollector(logsOpts)
		groups.logs[tenant] = logsCollector
	}
	groups.mu.Unlock()

	if postingsCollector != nil {
		if err := postingsCollector.foldSection(ctx, sec, objPath, sectionIdx); err != nil {
			return err
		}
	}
	if logsCollector != nil {
		if err := logsCollector.foldSection(ctx, sec, objPath, sectionIdx); err != nil {
			return err
		}
	}
	return nil
}

func objectTotals(ctx context.Context, bucket objstore.Bucket, paths map[string]struct{}) (classTotals, error) {
	var totals classTotals
	for path := range paths {
		attrs, err := bucket.Attributes(ctx, path)
		if err != nil {
			return classTotals{}, fmt.Errorf("getting attributes for index object %s: %w", path, err)
		}
		totals.objects++
		totals.bytes += attrs.Size
	}
	return totals, nil
}

// localityWindows returns exactly ceil(lookback/window-size) aligned windows,
// ending with the currently active metastore window.
func localityWindows(now time.Time, lookback time.Duration) []time.Time {
	count := int(math.Ceil(float64(lookback) / float64(metastore.MetastoreWindowSize)))
	if count < 1 {
		count = 1
	}
	last := now.UTC().Truncate(metastore.MetastoreWindowSize)
	windows := make([]time.Time, count)
	for i := range windows {
		windows[i] = last.Add(time.Duration(i-count+1) * metastore.MetastoreWindowSize)
	}
	return windows
}

// nextTick returns the next UTC schedule point, resetting the sequence from
// midnight every day. This preserves the midnight anchor even for periods that
// do not divide 24 hours.
func nextTick(now time.Time, period time.Duration) time.Time {
	now = now.UTC()
	midnight := now.Truncate(24 * time.Hour)
	elapsed := now.Sub(midnight)
	next := midnight.Add((elapsed/period + 1) * period)
	if !next.Before(midnight.Add(24 * time.Hour)) {
		return midnight.Add(24 * time.Hour)
	}
	return next
}

func splitWindowClass(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == '\x00' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
