package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/bits"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	lokicfg "github.com/grafana/loki/v3/pkg/util/cfg"
)

// postingKey identifies a label posting by its column name and value. The
// value is empty for the name-only rollup.
type postingKey struct {
	name  string
	value string
}

// keyAgg holds the global, cross-section aggregate for a single label key.
type keyAgg struct {
	// sectionSpread is the number of distinct physical postings sections the
	// key appears in ("clustering depth").
	sectionSpread int64
	// objectSpread is the number of distinct index objects the key appears in.
	objectSpread int64
	// seenObjects tracks which index object paths have been folded so that
	// objectSpread is counted correctly even when sections arrive out of order
	// (e.g. from concurrent goroutines).
	seenObjects map[string]struct{}
	// postingEntries is the number of KindLabel rows referencing the key
	// (= logs-section references).
	postingEntries int64
}

// localCounts holds the section-local sums for a single key before they are
// folded into the global [keyAgg].
type localCounts struct {
	entries int64
}

// collectorOptions controls which rollups the collector maintains.
type collectorOptions struct {
	byNameValue bool
	byName      bool
}

// collector accumulates label locality metrics across postings sections.
// foldSection may be called concurrently; all shared state is guarded by mu.
type collector struct {
	opts          collectorOptions
	mu            sync.Mutex
	totalSections int64
	nameValue     map[postingKey]*keyAgg
	name          map[string]*keyAgg
}

// newCollector returns a collector configured for the requested rollups.
func newCollector(opts collectorOptions) *collector {
	c := &collector{opts: opts}
	if opts.byNameValue {
		c.nameValue = make(map[postingKey]*keyAgg)
	}
	if opts.byName {
		c.name = make(map[string]*keyAgg)
	}
	return c
}

// sectionSource yields postings sections to the collector. Implementations
// exist for object storage and local directories. fn may be called
// concurrently; the sectionOrdinal argument is unused and always 0.
type sectionSource interface {
	each(ctx context.Context, fn func(sec *dataobj.Section, objPath string, sectionOrdinal int) error) error
}

// collect drives src, folding every tenant-owned postings section into the
// collector's global maps.
func (c *collector) collect(ctx context.Context, src sectionSource) error {
	return src.each(ctx, func(sec *dataobj.Section, objPath string, _ int) error {
		return c.foldSection(ctx, sec, objPath)
	})
}

// foldSection reads all KindLabel rows from a single postings section into
// section-local counts, then folds those counts into the global maps.
func (c *collector) foldSection(ctx context.Context, sec *dataobj.Section, objPath string) error {
	psec, err := postings.Open(ctx, sec)
	if err != nil {
		return fmt.Errorf("opening postings section %s: %w", objPath, err)
	}

	kindCol := findColumn(psec, postings.ColumnTypeKind)
	if kindCol == nil {
		// A section without a kind column has no label postings to attribute.
		return nil
	}

	pred := postings.EqualPredicate{Column: kindCol, Value: scalar.NewInt64Scalar(int64(postings.KindLabel))}
	rr := postings.NewRowReader(ctx, psec, []postings.Predicate{pred})
	defer rr.Close()

	var (
		nvLocal   map[postingKey]*localCounts
		nameLocal map[string]*localCounts
	)
	if c.opts.byNameValue {
		nvLocal = make(map[postingKey]*localCounts)
	}
	if c.opts.byName {
		nameLocal = make(map[string]*localCounts)
	}

	for rr.Next() {
		row := rr.At()
		if row.Kind != postings.KindLabel {
			continue
		}
		if c.opts.byNameValue {
			addLocal(nvLocal, postingKey{name: row.ColumnName, value: row.LabelValue})
		}
		if c.opts.byName {
			addLocal(nameLocal, row.ColumnName)
		}
	}
	if err := rr.Err(); err != nil {
		return fmt.Errorf("reading postings section %s: %w", objPath, err)
	}

	// I/O is done; merge section-local counts into the global maps under lock.
	c.mu.Lock()
	c.totalSections++
	for k, lc := range nvLocal {
		foldInto(c.nameValue, k, objPath, lc)
	}
	for k, lc := range nameLocal {
		foldInto(c.name, k, objPath, lc)
	}
	c.mu.Unlock()
	return nil
}

// addLocal accumulates one row's counts into the section-local map for key.
func addLocal[K comparable](m map[K]*localCounts, key K) {
	lc := m[key]
	if lc == nil {
		lc = &localCounts{}
		m[key] = lc
	}
	lc.entries++
}

// foldInto folds one section's local counts for key into the global aggregate:
// sectionSpread grows by one per section, sizes accumulate, and objectSpread
// grows only when the key is seen in a new object. Must be called with the
// collector's mu held.
func foldInto[K comparable](global map[K]*keyAgg, key K, objPath string, lc *localCounts) {
	agg := global[key]
	if agg == nil {
		agg = &keyAgg{seenObjects: make(map[string]struct{})}
		global[key] = agg
	}
	agg.sectionSpread++
	agg.postingEntries += lc.entries
	if _, seen := agg.seenObjects[objPath]; !seen {
		agg.seenObjects[objPath] = struct{}{}
		agg.objectSpread++
	}
}

// popcount returns the number of set bits in b.
func popcount(b []byte) int {
	var n int
	for _, x := range b {
		n += bits.OnesCount8(x)
	}
	return n
}

// findColumn returns the section's column of type ct, or nil if absent.
func findColumn(sec *postings.Section, ct postings.ColumnType) *postings.Column {
	for _, c := range sec.Columns() {
		if c.Type == ct {
			return c
		}
	}
	return nil
}

// reportRow is a flattened, display-ready view of a key's aggregate together
// with its derived locality fields.
type reportRow struct {
	key               string
	agg               *keyAgg
	entriesPerSection float64
}

// report writes the fleet summary and top-N tables for every enabled rollup.
func (c *collector) report(w io.Writer, top int) {
	if c.opts.byNameValue {
		rows := buildRows(c.nameValue, func(k postingKey) string { return k.name + "=" + k.value })
		writeReport(w, "name-value", rows, top, c.totalSections)
	}
	if c.opts.byName {
		rows := buildRows(c.name, func(k string) string { return k })
		writeReport(w, "name", rows, top, c.totalSections)
	}
}

// buildRows flattens a global map into display rows with derived fields.
func buildRows[K comparable](m map[K]*keyAgg, keyStr func(K) string) []reportRow {
	rows := make([]reportRow, 0, len(m))
	for k, agg := range m {
		var eps float64
		if agg.sectionSpread > 0 {
			eps = float64(agg.postingEntries) / float64(agg.sectionSpread)
		}
		rows = append(rows, reportRow{
			key:               keyStr(k),
			agg:               agg,
			entriesPerSection: eps,
		})
	}
	return rows
}

// writeReport prints the summary and top-N tables for a single rollup.
func writeReport(w io.Writer, title string, rows []reportRow, top int, totalSections int64) {
	fmt.Fprintf(w, "\n=== %s locality (%d keys, %d total postings sections) ===\n", title, len(rows), totalSections)
	if len(rows) == 0 {
		fmt.Fprintln(w, "  no label postings found")
		return
	}

	sectionSpread := make([]int64, len(rows))
	objectSpread := make([]int64, len(rows))
	for i, r := range rows {
		sectionSpread[i] = r.agg.sectionSpread
		objectSpread[i] = r.agg.objectSpread
	}

	var spreadStats, objectStats PercentileStats
	calcPercentiles(sectionSpread, &spreadStats)
	calcPercentiles(objectSpread, &objectStats)

	fmt.Fprintf(w, "  sectionSpread: p50=%.2f p95=%.2f p99=%.2f max=%.0f\n", spreadStats.Median, spreadStats.P95, spreadStats.P99, spreadStats.Max)
	fmt.Fprintf(w, "  objectSpread:  p50=%.2f p95=%.2f p99=%.2f max=%.0f\n", objectStats.Median, objectStats.P95, objectStats.P99, objectStats.Max)

	writeTopTable(w, fmt.Sprintf("Top %d by sectionSpread", top), rows, top, totalSections, func(a, b reportRow) bool {
		if a.agg.sectionSpread != b.agg.sectionSpread {
			return a.agg.sectionSpread > b.agg.sectionSpread
		}
		return a.agg.postingEntries > b.agg.postingEntries
	})
}

// writeTopTable prints the top rows ranked by less, using a tab-aligned table.
func writeTopTable(w io.Writer, header string, rows []reportRow, top int, totalSections int64, less func(a, b reportRow) bool) {
	fmt.Fprintf(w, "\n%s:\n", header)

	ranked := make([]reportRow, len(rows))
	copy(ranked, rows)
	sort.Slice(ranked, func(i, j int) bool { return less(ranked[i], ranked[j]) })
	if top > 0 && len(ranked) > top {
		ranked = ranked[:top]
	}

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "KEY\tSECTIONS\tTOTAL\tOBJECTS\tENTRIES\tENTRIES/SEC")
	for _, r := range ranked {
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%.1f\n",
			r.key,
			r.agg.sectionSpread,
			totalSections,
			r.agg.objectSpread,
			r.agg.postingEntries,
			r.entriesPerSection,
		)
	}
	_ = tw.Flush()
}

// indexObjectSource enumerates tenant-owned postings sections from index
// objects referenced by the metastore's Tables of Contents over [from, to].
// It backs both the object-storage and local-directory sources; only the
// underlying bucket differs.
type indexObjectSource struct {
	rawBucket    objstore.Bucket
	metastoreCfg metastore.Config
	tenant       string
	from, to     time.Time
	logger       log.Logger
}

func (s *indexObjectSource) each(ctx context.Context, fn func(sec *dataobj.Section, objPath string, sectionOrdinal int) error) error {
	ctx = user.InjectOrgID(ctx, s.tenant)

	// metastore.NewObjectMetastore applies IndexStoragePrefix internally, so it
	// receives the raw (un-index-prefixed) bucket.
	store := metastore.NewObjectMetastore(
		s.rawBucket,
		s.metastoreCfg,
		s.logger,
		metastore.NewObjectMetastoreMetrics(nil),
	)

	indexes, err := store.GetIndexes(ctx, metastore.GetIndexesRequest{Start: s.from, End: s.to})
	if err != nil {
		return fmt.Errorf("listing index objects: %w", err)
	}

	total := len(indexes.Indexes)
	level.Info(s.logger).Log("msg", "index objects found", "count", total)

	// The metastore hands back index-pointer paths relative to the
	// IndexStoragePrefix view, so direct object reads must use the same view.
	idxBucket := s.rawBucket
	if p := s.metastoreCfg.IndexStoragePrefix; p != "" {
		idxBucket = objstore.NewPrefixedBucket(s.rawBucket, p)
	}

	var done atomic.Int64
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(8)

	for _, entry := range indexes.Indexes {
		entry := entry
		g.Go(func() error {
			n := done.Add(1)
			// Log the first object, the last, and every 50th as a heartbeat.
			if n == 1 || n%50 == 0 || n == int64(total) {
				level.Info(s.logger).Log("msg", "processing index object", "n", n, "total", total, "path", entry.Path)
			}

			obj, err := dataobj.FromBucket(gCtx, idxBucket, entry.Path, 0)
			if err != nil {
				return fmt.Errorf("opening index object %s: %w", entry.Path, err)
			}
			for _, sec := range obj.Sections() {
				if !postings.CheckSection(sec) || sec.Tenant != s.tenant {
					continue
				}
				if err := fn(sec, entry.Path, 0); err != nil {
					return err
				}
			}
			return nil
		})
	}
	return g.Wait()
}

// postingsLocalityCommand implements the "postings-locality" subcommand.
type postingsLocalityCommand struct {
	lokiConfigFile *string
	expandEnv      *bool
	dir            *string
	tenant         *string
	from           *string
	to             *string
	by             *string
	top            *int
}

func (cmd *postingsLocalityCommand) run(_ *kingpin.ParseContext) error {
	ctx := context.Background()
	logger := log.NewLogfmtLogger(os.Stdout)

	from, to, err := parseTimeRange(*cmd.from, *cmd.to)
	if err != nil {
		return err
	}

	opts, err := parseByMode(*cmd.by)
	if err != nil {
		return err
	}

	src, err := cmd.buildSource(ctx, logger, from, to)
	if err != nil {
		return err
	}

	level.Info(logger).Log("msg", "collecting postings locality", "tenant", *cmd.tenant, "from", from.Format(time.RFC3339), "to", to.Format(time.RFC3339))
	start := time.Now()
	c := newCollector(opts)
	if err := c.collect(ctx, src); err != nil {
		return err
	}
	level.Info(logger).Log("msg", "collection complete", "elapsed", time.Since(start).Round(time.Millisecond))

	c.report(os.Stdout, *cmd.top)
	return nil
}

// buildSource constructs the section source from the mutually-exclusive
// --dir (local) or --config.file (bucket) flags.
func (cmd *postingsLocalityCommand) buildSource(ctx context.Context, logger log.Logger, from, to time.Time) (sectionSource, error) {
	switch {
	case *cmd.dir != "":
		if *cmd.lokiConfigFile != "" {
			return nil, fmt.Errorf("--dir cannot be combined with --config.file")
		}
		b, err := filesystem.NewBucket(*cmd.dir)
		if err != nil {
			return nil, fmt.Errorf("opening local directory %s: %w", *cmd.dir, err)
		}
		// Use the same defaults as metastore.Config.RegisterFlagsWithPrefix.
		// --dir assumes no StorageBucketPrefix is in play.
		mCfg := metastore.Config{IndexStoragePrefix: "index/v0", PartitionRatio: 10}
		return &indexObjectSource{
			rawBucket:    b,
			metastoreCfg: mCfg,
			tenant:       *cmd.tenant,
			from:         from,
			to:           to,
			logger:       logger,
		}, nil

	case *cmd.lokiConfigFile != "":
		if *cmd.tenant == "" {
			return nil, fmt.Errorf("--tenant is required for the bucket source")
		}
		b, mCfg, err := buildBucketFromLokiConfig(ctx, *cmd.lokiConfigFile, *cmd.expandEnv, logger)
		if err != nil {
			return nil, err
		}
		return &indexObjectSource{
			rawBucket:    b,
			metastoreCfg: mCfg,
			tenant:       *cmd.tenant,
			from:         from,
			to:           to,
			logger:       logger,
		}, nil

	default:
		return nil, fmt.Errorf("one of --dir or --config.file must be provided")
	}
}

// buildBucketFromLokiConfig loads a full Loki config file and derives the
// object-store bucket and metastore config from it, mirroring the
// getDataObjBucket + compaction-worker wiring in pkg/loki/modules.go.
func buildBucketFromLokiConfig(ctx context.Context, configFile string, expandEnv bool, logger log.Logger) (objstore.Bucket, metastore.Config, error) {
	level.Info(logger).Log("msg", "loading loki config", "file", configFile)

	args := []string{"-config.file=" + configFile}
	if expandEnv {
		args = append(args, "-config.expand-env=true")
	}
	var c loki.ConfigWrapper
	if err := lokicfg.DynamicUnmarshal(&c, args, flag.NewFlagSet("loki-config", flag.ContinueOnError)); err != nil {
		return nil, metastore.Config{}, fmt.Errorf("loading loki config %s: %w", configFile, err)
	}

	// Derive the backend from the schema, mirroring getDataObjBucket.
	schema, err := c.SchemaConfig.SchemaForTime(model.Now())
	if err != nil {
		return nil, metastore.Config{}, fmt.Errorf("resolving schema for current time: %w", err)
	}

	// Apply named-store resolution before creating the client.
	objCfg := c.StorageConfig.ObjectStore
	backend := schema.ObjectType
	if st, ok := objCfg.NamedStores.LookupStoreType(schema.ObjectType); ok {
		backend = st
		if err := objCfg.NamedStores.OverrideConfig(&objCfg.Config, schema.ObjectType); err != nil {
			return nil, metastore.Config{}, fmt.Errorf("resolving named store %q: %w", schema.ObjectType, err)
		}
	}

	mCfg := c.DataObj.Metastore
	level.Info(logger).Log(
		"msg", "config loaded",
		"backend", backend,
		"dataobj_prefix", c.DataObj.StorageBucketPrefix,
		"index_prefix", mCfg.IndexStoragePrefix,
	)

	ib, err := bucket.NewClient(ctx, backend, objCfg.Config, "dataobj-inspect", logger)
	if err != nil {
		return nil, metastore.Config{}, fmt.Errorf("creating bucket client: %w", err)
	}

	// Apply the dataobj-level namespace prefix so paths resolve identically to
	// what the running engine uses. Use a plain Bucket interface so the
	// NewPrefixedBucket wrapper (which is not InstrumentedBucket) is accepted.
	var b objstore.Bucket = ib
	if p := c.DataObj.StorageBucketPrefix; p != "" {
		b = objstore.NewPrefixedBucket(b, p)
	}

	return b, mCfg, nil
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

// parseByMode maps the --by flag to collector options.
func parseByMode(by string) (collectorOptions, error) {
	switch by {
	case "name-value":
		return collectorOptions{byNameValue: true}, nil
	case "name":
		return collectorOptions{byName: true}, nil
	case "both", "":
		return collectorOptions{byNameValue: true, byName: true}, nil
	default:
		return collectorOptions{}, fmt.Errorf("invalid --by value %q (want name-value|name|both)", by)
	}
}

func addPostingsLocalityCommand(app *kingpin.Application) {
	cmd := &postingsLocalityCommand{}
	c := app.Command("postings-locality", "Measure label clustering depth across postings sections over a time range.").Action(cmd.run)

	cmd.lokiConfigFile = c.Flag("config.file", "Path to a Loki config YAML (bucket source). Storage credentials and prefixes are derived from the config, mirroring the engine.").String()
	cmd.expandEnv = c.Flag("config.expand-env", "Expand ${VAR} references in the Loki config file.").Default("false").Bool()
	cmd.dir = c.Flag("dir", "Local directory holding TOCs and index objects (local source). Mutually exclusive with --config.file.").String()
	cmd.tenant = c.Flag("tenant", "Tenant whose postings sections are inspected.").String()
	cmd.from = c.Flag("from", "Start of the time range (RFC3339). Defaults to --to minus 12h.").String()
	cmd.to = c.Flag("to", "End of the time range (RFC3339). Defaults to now.").String()
	cmd.by = c.Flag("by", "Rollup to compute: name-value, name, or both.").Default("both").Enum("name-value", "name", "both")
	cmd.top = c.Flag("top", "Number of rows to print in each top-N table.").Default("20").Int()
}
