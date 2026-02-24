package main

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/config"
	indexstorage "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/cfg"
	"github.com/grafana/loki/v3/pkg/validation"
)

const daySeconds = int64(24 * time.Hour / time.Second)

// log is the package-level logger, configured in main().
var log *slog.Logger

// toolFlags holds the parsed values of flags specific to this tool.
type toolFlags struct {
	start          string
	end            string
	tenant         string
	labels         string
	download       string
	genConfig      string
	verbose        bool
	noNormalizeForFS bool
}

type fileEntry struct {
	key  string
	size int64
}

// customFlagNames lists flags specific to this tool (not part of Loki config).
// They must be extracted from os.Args before passing to DynamicUnmarshal,
// because ConfigFileLoader internally creates a fresh FlagSet that only
// knows about Loki config flags and will reject unknown flags.
var customFlagNames = map[string]bool{
	"start":      true,
	"end":        true,
	"tenant":     true,
	"labels":     true,
	"download":          true,
	"gen-config":        true,
	"verbose":           true,
	"v":                 true,
	"no-normalize-for-filesystem": true,
}

// extractCustomFlags separates custom tool flags from Loki config flags.
// Returns the custom flag values and the remaining args for DynamicUnmarshal.
func extractCustomFlags(args []string) (flags toolFlags, remaining []string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// Strip leading dashes to get the flag name.
		name := strings.TrimLeft(arg, "-")

		// Handle --flag=value form.
		if eqIdx := strings.Index(name, "="); eqIdx >= 0 {
			flagName := name[:eqIdx]
			flagVal := name[eqIdx+1:]
			if customFlagNames[flagName] {
				setToolFlag(&flags, flagName, flagVal)
				continue
			}
			remaining = append(remaining, arg)
			continue
		}

		// Handle boolean flags — no value expected.
		if name == "verbose" || name == "v" {
			flags.verbose = true
			continue
		}
		if name == "no-normalize-for-filesystem" {
			flags.noNormalizeForFS = true
			continue
		}

		// Handle --flag value form.
		if customFlagNames[name] {
			if i+1 < len(args) {
				i++
				setToolFlag(&flags, name, args[i])
			}
			continue
		}

		remaining = append(remaining, arg)
	}
	return
}

func setToolFlag(f *toolFlags, name, val string) {
	switch name {
	case "start":
		f.start = val
	case "end":
		f.end = val
	case "tenant":
		f.tenant = val
	case "labels":
		f.labels = val
	case "download":
		f.download = val
	case "gen-config":
		f.genConfig = val
	}
}

const usage = `loki-storage-finder: find TSDB index and chunk files in Loki storage

Usage:
  loki-storage-finder --config.file=<config> --start=<time> --end=<time> --tenant=<id> [options]
  loki-storage-finder --gen-config=<s3|gcs|azure|filesystem>

Required:
  --config.file <path>   Loki config YAML (only schema_config + storage_config needed)
  --start <time>         Start time (RFC3339 or Unix seconds)
  --end <time>           End time (RFC3339 or Unix seconds)
  --tenant <id>          Tenant ID

Options:
  --labels <matchers>    Label matchers, e.g. '{app="foo",env=~"prod.*"}'
  --download <dir>       Download matching files into this directory
  --no-normalize-for-filesystem
                         Skip chunk key normalization for filesystem (see below). Default: normalize
  --gen-config <type>    Print a minimal config template (s3, gcs, azure, filesystem)
  -v, --verbose          Show debug output

Filesystem normalization (enabled by default when --download is used):
  Cloud object stores and Loki's filesystem client store chunk files differently:
    - Azure replaces ":" with "-" in blob names
    - Loki's filesystem client base64-encodes the chunk filename (from:through:checksum)
  Normalization converts downloaded chunk files to the filesystem format so a local
  Loki instance can read them directly. Use --no-normalize-for-filesystem to preserve
  the original cloud key format (e.g. for inspection or re-upload).

Generate a config template, then edit it:
  loki-storage-finder --gen-config=azure > config.yaml
`

func main() {
	tf, lokiArgs := extractCustomFlags(os.Args[1:])

	// Handle --help / -help / -h / no args before DynamicUnmarshal, which
	// requires a config file and would error out otherwise.
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
	for _, arg := range os.Args[1:] {
		if arg == "--help" || arg == "-help" || arg == "-h" {
			fmt.Print(usage)
			os.Exit(0)
		}
	}

	// Configure logger.
	logLevel := slog.LevelInfo
	if tf.verbose {
		logLevel = slog.LevelDebug
	}
	log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	// Handle --gen-config before loading Loki config.
	if tf.genConfig != "" {
		generateConfig(tf.genConfig)
		return
	}

	var configWrapper loki.ConfigWrapper
	if err := cfg.DynamicUnmarshal(&configWrapper, lokiArgs, flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	if tf.start == "" || tf.end == "" || tf.tenant == "" {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	startTime, err := parseTime(tf.start)
	exitErr("parsing start time", err)
	endTime, err := parseTime(tf.end)
	exitErr("parsing end time", err)

	from := model.TimeFromUnix(startTime.Unix())
	through := model.TimeFromUnix(endTime.Unix())

	var matchers []*labels.Matcher
	if tf.labels != "" {
		matchers, err = syntax.ParseMatchers(tf.labels, true)
		exitErr("parsing label matchers", err)
		for _, m := range matchers {
			log.Info("parsed matcher", "matcher", m.String())
		}
	}

	tenant := tf.tenant
	download := tf.download
	conf := configWrapper.Config
	ctx := context.Background()

	// Apply defaults that Loki normally sets via Validate() (which DynamicUnmarshal skips).
	for i := range conf.SchemaConfig.Configs {
		if conf.SchemaConfig.Configs[i].IndexTables.PathPrefix == "" {
			conf.SchemaConfig.Configs[i].IndexTables.PathPrefix = "index/"
		}
	}

	// Find period configs covering the time range.
	periodCfgs := findPeriodConfigs(conf.SchemaConfig.Configs, from, through)
	if len(periodCfgs) == 0 {
		fmt.Fprintf(os.Stderr, "error: no period config found for the given time range\n")
		os.Exit(1)
	}

	var clientMetrics storage.ClientMetrics

	// makeObjectClient creates the appropriate object client, using the modern
	// Azure SDK with DefaultAzureCredential when the object store is azure.
	makeObjectClient := func(periodCfg config.PeriodConfig) (client.ObjectClient, error) {
		if periodCfg.ObjectType == "azure" {
			return newAzureObjectClient(
				conf.StorageConfig.AzureStorageConfig.StorageAccountName,
				conf.StorageConfig.AzureStorageConfig.ContainerName,
				conf.StorageConfig.AzureStorageConfig.EndpointSuffix,
			)
		}
		return storage.NewObjectClient(periodCfg.ObjectType, "storage-finder", conf.StorageConfig, clientMetrics)
	}

	var totalIndexFiles int
	var totalIndexBytes int64
	var totalChunkFiles int
	var totalChunkBytes int64

	var indexEntries []fileEntry
	var chunkEntries []fileEntry

	for _, periodCfg := range periodCfgs {
		objectClient, err := makeObjectClient(periodCfg)
		exitErr("creating object client", err)

		indexClient := indexstorage.NewIndexStorageClient(objectClient, periodCfg.IndexTables.PathPrefix)

		// Compute table numbers for the time range.
		startTable := from.Unix() / daySeconds
		endTable := through.Unix() / daySeconds

		// List all tables and filter to those in the time range.
		log.Info("listing tables", "path_prefix", periodCfg.IndexTables.PathPrefix)
		tables, err := indexClient.ListTables(ctx)
		exitErr("listing tables", err)
		log.Debug("tables found", "count", len(tables), "start_table", startTable, "end_table", endTable)

		for _, tableName := range tables {
			tableNum, err := config.ExtractTableNumberFromName(tableName)
			if err != nil {
				log.Debug("skipping table (no number)", "table", tableName)
				continue
			}
			if tableNum < startTable || tableNum > endTable {
				continue
			}
			log.Info("scanning table", "table", tableName, "period", tableNum)

			// List per-tenant files.
			files, err := indexClient.ListUserFiles(ctx, tableName, tenant, true)
			if err != nil {
				log.Warn("listing files failed", "table", tableName, "err", err)
				continue
			}
			log.Debug("tenant files found", "table", tableName, "count", len(files))

			for _, f := range files {
				// Build the full key path for GetAttributes.
				fullKey := path.Join(periodCfg.IndexTables.PathPrefix, tableName, tenant, f.Name)
				attrs, err := objectClient.GetAttributes(ctx, fullKey)
				if err != nil {
					log.Warn("getting attributes failed", "key", fullKey, "err", err)
					continue
				}
				indexEntries = append(indexEntries, fileEntry{key: fullKey, size: attrs.Size})
				totalIndexFiles++
				totalIndexBytes += attrs.Size
			}

			// In verbose mode, dump the labels in this table's index.
			if log.Enabled(ctx, slog.LevelDebug) {
				dumpIndexLabels(ctx, indexClient, tableName, tenant, from, through)
			}

			// If labels are provided, parse TSDB indexes to discover chunks.
			if len(matchers) > 0 {
				chunkRefs, err := discoverChunksFromIndex(ctx, indexClient, objectClient, tableName, tenant, from, through, matchers, periodCfg)
				if err != nil {
					log.Warn("parsing index failed", "table", tableName, "err", err)
					continue
				}
				// Build chunk keys from refs.
				chunkKeys := make([]string, 0, len(chunkRefs))
				for _, ref := range chunkRefs {
					chunkKey := conf.SchemaConfig.ExternalKey(ref)
					if periodCfg.ObjectType == "azure" {
						chunkKey = azureChunkKey(chunkKey)
					}
					chunkKeys = append(chunkKeys, chunkKey)
				}

				log.Info("resolving chunk sizes", "chunks", len(chunkKeys))
				resolved := getAttributesParallel(ctx, objectClient, chunkKeys, 64)
				for _, e := range resolved {
					chunkEntries = append(chunkEntries, e)
					totalChunkFiles++
					totalChunkBytes += e.size
				}
			}
		}

		// If no labels, discover chunks by listing objects under tenant prefix.
		if len(matchers) == 0 {
			log.Info("listing chunks (this may take a while)", "prefix", tenant+"/")
			chunks, err := discoverChunksByListing(ctx, objectClient, tenant, from, through)
			if err != nil {
				log.Warn("listing chunks failed", "err", err)
			} else {
				for _, c := range chunks {
					chunkEntries = append(chunkEntries, c)
					totalChunkFiles++
					totalChunkBytes += c.size
				}
			}
		}

		indexClient.Stop()
		objectClient.Stop()
	}

	// Print results.
	fmt.Println("\n=== Index Files ===")
	for _, e := range indexEntries {
		fmt.Printf("  %s  (%s)\n", e.key, humanize.IBytes(uint64(e.size)))
	}
	fmt.Printf("  Total: %d files, %s\n", totalIndexFiles, humanize.IBytes(uint64(totalIndexBytes)))

	fmt.Println("\n=== Chunk Files ===")
	for _, e := range chunkEntries {
		fmt.Printf("  %s  (%s)\n", e.key, humanize.IBytes(uint64(e.size)))
	}
	fmt.Printf("  Total: %d files, %s\n", totalChunkFiles, humanize.IBytes(uint64(totalChunkBytes)))

	grandTotal := totalIndexFiles + totalChunkFiles
	grandBytes := totalIndexBytes + totalChunkBytes
	fmt.Println("\n=== Grand Total ===")
	fmt.Printf("  %d files, %s\n", grandTotal, humanize.IBytes(uint64(grandBytes)))

	// Download if requested.
	if download != "" {
		fmt.Printf("\nDownloading files to %s...\n", download)
		allEntries := append(indexEntries, chunkEntries...)
		total := len(allEntries)

		for _, periodCfg := range periodCfgs {
			objectClient, err := makeObjectClient(periodCfg)
			exitErr("creating object client for download", err)

			var downloaded atomic.Int64
			var downloadedBytes atomic.Int64
			var failed atomic.Int64
			sem := make(chan struct{}, 64)
			var wg sync.WaitGroup

			for _, e := range allEntries {
				wg.Add(1)
				go func(entry fileEntry) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					err := downloadFile(ctx, objectClient, entry.key, download, !tf.noNormalizeForFS)
					if err != nil {
						log.Warn("download failed", "key", entry.key, "err", err)
						failed.Add(1)
					} else {
						downloaded.Add(1)
						downloadedBytes.Add(entry.size)
					}
					done := int(downloaded.Load() + failed.Load())
					printProgress(done, total, downloadedBytes.Load(), grandBytes)
				}(e)
			}
			wg.Wait()
			objectClient.Stop()
			break // Only need one client since all entries use the same storage
		}
		printProgress(total, total, grandBytes, grandBytes)
		fmt.Println() // newline after progress bar
		fmt.Printf("Downloaded %d files, %s\n", total, humanize.IBytes(uint64(grandBytes)))
	}
}

// generateConfig prints a minimal Loki config template for the given storage provider.
func generateConfig(provider string) {
	schemaHeader := `schema_config:
  configs:
    - from: "2024-01-01"
      store: tsdb
      object_store: %s
      schema: v13
      index:
        prefix: loki_index_
        period: 24h
`

	var storageBlock string
	switch strings.ToLower(provider) {
	case "s3", "aws":
		fmt.Print(fmt.Sprintf(schemaHeader, "s3"))
		storageBlock = `
storage_config:
  aws:
    # S3 bucket URL. Format: s3://<region>/<bucket_name>
    # Or: s3://access_key:secret_key@region/bucket_name
    s3: s3://us-east-1/my-loki-bucket
    # Alternatively, set individual fields:
    # bucketnames: my-loki-bucket
    # region: us-east-1
    # access_key_id: ...     # optional: uses AWS credential chain if unset
    # secret_access_key: ... # optional: uses AWS credential chain if unset
    # endpoint: ...          # optional: for S3-compatible stores (MinIO, etc.)
`
	case "gcs":
		fmt.Print(fmt.Sprintf(schemaHeader, "gcs"))
		storageBlock = `
storage_config:
  gcs:
    bucket_name: my-loki-bucket
    # service_account: ...   # optional: uses GOOGLE_APPLICATION_CREDENTIALS / default credentials if unset
`
	case "azure":
		fmt.Print(fmt.Sprintf(schemaHeader, "azure"))
		storageBlock = `
storage_config:
  azure:
    container_name: my-loki-container
    account_name: my-storage-account
    # endpoint_suffix: blob.core.windows.net  # optional, defaults to blob.core.windows.net
    # No credentials needed — uses Azure DefaultAzureCredential:
    #   1. Environment variables (AZURE_CLIENT_ID, etc.)
    #   2. Workload Identity
    #   3. Managed Identity
    #   4. Azure CLI (az login)
`
	case "filesystem", "fs", "local":
		fmt.Print(fmt.Sprintf(schemaHeader, "filesystem"))
		storageBlock = `
storage_config:
  filesystem:
    directory: /path/to/loki/chunks
`
	default:
		fmt.Fprintf(os.Stderr, "unknown provider %q — use one of: s3, gcs, azure, filesystem\n", provider)
		os.Exit(1)
	}

	fmt.Print(storageBlock)
}

// findPeriodConfigs returns all TSDB period configs that overlap the given time range.
func findPeriodConfigs(configs []config.PeriodConfig, from, through model.Time) []config.PeriodConfig {
	var result []config.PeriodConfig
	for i, cfg := range configs {
		if cfg.IndexType != types.TSDBType {
			continue
		}
		periodStart := cfg.From.Time
		periodEnd := model.Time(math.MaxInt64)
		if i+1 < len(configs) {
			periodEnd = configs[i+1].From.Time.Add(-time.Millisecond)
		}
		// Check overlap: period overlaps [from, through] if periodStart <= through && periodEnd >= from
		if periodStart.After(through) || periodEnd.Before(from) {
			continue
		}
		result = append(result, cfg)
	}
	return result
}

// dumpIndexLabels downloads the tenant's TSDB index for a table, opens it,
// and logs all label names/values found (for debugging).
func dumpIndexLabels(ctx context.Context, indexClient indexstorage.Client, tableName, tenant string, from, through model.Time) {
	files, err := indexClient.ListUserFiles(ctx, tableName, tenant, true)
	if err != nil {
		log.Debug("dumpIndexLabels: listing files failed", "err", err)
		return
	}

	tmpDir, err := os.MkdirTemp("", "loki-storage-finder-dump-*")
	if err != nil {
		return
	}
	defer os.RemoveAll(tmpDir)

	for _, f := range files {
		reader, err := indexClient.GetUserFile(ctx, tableName, tenant, f.Name)
		if err != nil {
			log.Debug("dumpIndexLabels: get file failed", "file", f.Name, "err", err)
			continue
		}

		localPath := filepath.Join(tmpDir, f.Name)
		out, err := os.Create(localPath)
		if err != nil {
			reader.Close()
			continue
		}
		_, err = io.Copy(out, reader)
		out.Close()
		reader.Close()
		if err != nil {
			continue
		}

		indexPath := localPath
		if strings.HasSuffix(f.Name, ".gz") {
			decompressedPath := strings.TrimSuffix(localPath, ".gz")
			if err := decompressGzip(localPath, decompressedPath); err != nil {
				continue
			}
			indexPath = decompressedPath
		}

		idxReader, err := index.NewFileReader(indexPath)
		if err != nil {
			continue
		}

		tsdbIdx := tsdb.NewTSDBIndex(idxReader)

		labelValues := map[string]map[string]struct{}{}
		var totalSeries int
		// An explicit "match everything" matcher is required — ForSeries with
		// no matchers returns 0 results because PostingsForMatchers returns
		// EmptyPostings for an empty matcher slice.
		allMatcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
		_ = tsdbIdx.ForSeries(ctx, "", nil, from, through, func(ls labels.Labels, _ model.Fingerprint, _ []index.ChunkMeta) (stop bool) {
			totalSeries++
			ls.Range(func(l labels.Label) {
				if _, ok := labelValues[l.Name]; !ok {
					labelValues[l.Name] = map[string]struct{}{}
				}
				labelValues[l.Name][l.Value] = struct{}{}
			})
			return false
		}, allMatcher)

		tsdbIdx.Close()

		log.Debug("index label dump", "file", f.Name, "total_series", totalSeries, "label_names", len(labelValues))
		for name, vals := range labelValues {
			samples := make([]string, 0, min(5, len(vals)))
			for v := range vals {
				if len(samples) >= 5 {
					break
				}
				samples = append(samples, v)
			}
			suffix := ""
			if len(vals) > 5 {
				suffix = fmt.Sprintf(" ... +%d more", len(vals)-5)
			}
			log.Debug("  label", "name", name, "unique_values", len(vals), "samples", strings.Join(samples, ", ")+suffix)
		}
	}
}

// discoverChunksFromIndex downloads TSDB index files, opens them, and discovers chunk references
// for series matching the given matchers.
func discoverChunksFromIndex(
	ctx context.Context,
	indexClient indexstorage.Client,
	objectClient client.ObjectClient,
	tableName, tenant string,
	from, through model.Time,
	matchers []*labels.Matcher,
	periodCfg config.PeriodConfig,
) ([]logproto.ChunkRef, error) {
	files, err := indexClient.ListUserFiles(ctx, tableName, tenant, true)
	if err != nil {
		return nil, fmt.Errorf("listing user files: %w", err)
	}

	var allRefs []logproto.ChunkRef

	tmpDir, err := os.MkdirTemp("", "loki-storage-finder-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	for _, f := range files {
		log.Debug("downloading index", "file", f.Name)
		reader, err := indexClient.GetUserFile(ctx, tableName, tenant, f.Name)
		if err != nil {
			return nil, fmt.Errorf("getting file %s: %w", f.Name, err)
		}

		localName := f.Name
		localPath := filepath.Join(tmpDir, localName)

		// Write to disk.
		out, err := os.Create(localPath)
		if err != nil {
			reader.Close()
			return nil, fmt.Errorf("creating local file: %w", err)
		}
		_, err = io.Copy(out, reader)
		out.Close()
		reader.Close()
		if err != nil {
			return nil, fmt.Errorf("writing local file: %w", err)
		}

		// Decompress if gzipped.
		indexPath := localPath
		if strings.HasSuffix(localName, ".gz") {
			decompressedPath := strings.TrimSuffix(localPath, ".gz")
			err = decompressGzip(localPath, decompressedPath)
			if err != nil {
				return nil, fmt.Errorf("decompressing %s: %w", localName, err)
			}
			indexPath = decompressedPath
		}

		// Open with TSDB reader.
		idxReader, err := index.NewFileReader(indexPath)
		if err != nil {
			return nil, fmt.Errorf("opening index %s: %w", f.Name, err)
		}

		tsdbIdx := tsdb.NewTSDBIndex(idxReader)

		var seriesCount, chunkMetaCount int
		err = tsdbIdx.ForSeries(ctx, "", nil, from, through, func(_ labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
			seriesCount++
			chunkMetaCount += len(chks)
			for _, chk := range chks {
				allRefs = append(allRefs, logproto.ChunkRef{
					UserID:      tenant,
					Fingerprint: uint64(fp),
					From:        chk.From(),
					Through:     chk.Through(),
					Checksum:    chk.Checksum,
				})
			}
			return false
		}, matchers...)

		tsdbIdx.Close()

		log.Info("index scanned", "file", f.Name, "matching_series", seriesCount, "chunk_refs", chunkMetaCount)

		if err != nil {
			return nil, fmt.Errorf("iterating series in %s: %w", f.Name, err)
		}
	}

	// Deduplicate chunk refs.
	return deduplicateChunkRefs(allRefs), nil
}

// discoverChunksByListing lists chunk objects under the tenant prefix and filters by time range.
func discoverChunksByListing(ctx context.Context, objectClient client.ObjectClient, tenant string, from, through model.Time) ([]fileEntry, error) {
	objects, _, err := objectClient.List(ctx, tenant+"/", "")
	if err != nil {
		return nil, fmt.Errorf("listing chunks: %w", err)
	}
	log.Debug("chunk objects listed", "count", len(objects))

	// Filter to chunks overlapping the time range.
	var matchingKeys []string
	for _, obj := range objects {
		chunkFrom, chunkThrough, ok := parseChunkTimeFromKey(obj.Key)
		if !ok {
			continue
		}
		if chunkFrom.After(through) || chunkThrough.Before(from) {
			continue
		}
		matchingKeys = append(matchingKeys, obj.Key)
	}
	log.Debug("chunks matching time range", "count", len(matchingKeys))

	// Resolve sizes in parallel.
	result := getAttributesParallel(ctx, objectClient, matchingKeys, 64)

	return result, nil
}

// parseChunkTimeFromKey extracts From and Through times from a chunk object key.
// Key formats (delimiter is ":" normally, "-" on Azure):
//
//	v12+: <tenant>/<fingerprint_hex>/<from_hex><delim><through_hex><delim><checksum_hex>
//	older: <tenant>/<fingerprint_hex><delim><from_hex><delim><through_hex><delim><checksum_hex>
func parseChunkTimeFromKey(key string) (model.Time, model.Time, bool) {
	parts := strings.SplitN(key, "/", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}

	var timePart string
	if len(parts) == 3 {
		// v12+: tenant/fp/from<delim>through<delim>checksum
		timePart = parts[2]
	} else {
		// older: tenant/fp<delim>from<delim>through<delim>checksum
		timePart = parts[1]
	}

	// Try ":" first (standard), then "-" (Azure blob storage replaces ":" with "-").
	for _, delim := range []string{":", "-"} {
		delimParts := strings.Split(timePart, delim)

		var fromHex, throughHex string
		if len(parts) == 3 && len(delimParts) == 3 {
			// v12+: [from, through, checksum]
			fromHex = delimParts[0]
			throughHex = delimParts[1]
		} else if len(parts) == 2 && len(delimParts) == 4 {
			// older: [fp, from, through, checksum]
			fromHex = delimParts[1]
			throughHex = delimParts[2]
		} else {
			continue
		}

		fromInt, err := strconv.ParseInt(fromHex, 16, 64)
		if err != nil {
			continue
		}
		throughInt, err := strconv.ParseInt(throughHex, 16, 64)
		if err != nil {
			continue
		}
		return model.Time(fromInt), model.Time(throughInt), true
	}

	return 0, 0, false
}

// azureChunkKey converts a standard Loki chunk key (with ":") to Azure blob
// format (with "-"), matching Loki's BlobStorage.getBlobURL() behavior.
func azureChunkKey(key string) string {
	return strings.ReplaceAll(key, ":", "-")
}

// getAttributesParallel fetches object attributes for many keys concurrently,
// using up to maxWorkers goroutines. Returns entries for keys that succeeded.
func getAttributesParallel(ctx context.Context, objectClient client.ObjectClient, keys []string, maxWorkers int) []fileEntry {
	if len(keys) == 0 {
		return nil
	}

	type result struct {
		entry fileEntry
		err   error
	}

	results := make([]result, len(keys))
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	var progress atomic.Int64
	total := len(keys)

	for i, key := range keys {
		wg.Add(1)
		go func(idx int, k string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			attrs, err := objectClient.GetAttributes(ctx, k)
			if err != nil {
				results[idx] = result{err: err}
			} else {
				results[idx] = result{entry: fileEntry{key: k, size: attrs.Size}}
			}

			cur := progress.Add(1)
			if cur%500 == 0 || int(cur) == total {
				log.Info("progress", "done", cur, "total", total)
			}
		}(i, key)
	}
	wg.Wait()

	entries := make([]fileEntry, 0, len(keys))
	for _, r := range results {
		if r.err != nil {
			log.Warn("getting chunk attributes failed", "key", r.entry.key, "err", r.err)
			continue
		}
		entries = append(entries, r.entry)
	}
	return entries
}

// printProgress renders an in-place progress bar to stderr.
func printProgress(current, total int, currentBytes, totalBytes int64) {
	const barWidth = 30
	pct := float64(0)
	if total > 0 {
		pct = float64(current) / float64(total)
	}
	filled := int(pct * barWidth)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Fprintf(os.Stderr, "\r  [%s] %d/%d files  %s / %s  (%d%%)",
		bar, current, total,
		humanize.IBytes(uint64(currentBytes)), humanize.IBytes(uint64(totalBytes)),
		int(pct*100))
}

// normalizeChunkKey converts cloud chunk keys to the format Loki's filesystem
// client expects. Two transformations:
//  1. Azure uses "-" instead of ":" — convert back to ":"
//  2. Loki's FSEncoder base64-encodes the chunk filename (the from:through:checksum
//     part after the last "/") for v12+ schemas. Cloud backends store the raw hex,
//     but filesystem expects base64.
//
// Index files (containing ".tsdb") are left unchanged.
func normalizeChunkKey(key string) string {
	dir, file := path.Split(key)
	if strings.Contains(file, ".tsdb") {
		return key
	}

	// First, normalize Azure "-" delimiters to ":".
	// Chunk filenames are hex:hex:hex (from:through:checksum).
	// Try "-" split first (Azure), then check if already ":"-delimited.
	parts := strings.SplitN(file, "-", 3)
	if len(parts) == 3 && isHex(parts[0]) && isHex(parts[1]) {
		file = parts[0] + ":" + parts[1] + ":" + parts[2]
	}

	// Then base64-encode the filename, matching Loki's FSEncoder behavior.
	file = base64.StdEncoding.EncodeToString([]byte(file))
	return dir + file
}

func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// downloadFile downloads a single file from object storage preserving directory structure.
// When normalizeKeys is true, chunk keys are converted to standard ":" delimiter format.
func downloadFile(ctx context.Context, objectClient client.ObjectClient, key, destDir string, normalizeKeys bool) error {
	reader, _, err := objectClient.GetObject(ctx, key)
	if err != nil {
		return err
	}
	defer reader.Close()

	localKey := key
	if normalizeKeys {
		localKey = normalizeChunkKey(key)
	}

	destPath := filepath.Join(destDir, filepath.FromSlash(localKey))
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, reader)
	return err
}

// decompressGzip decompresses a gzip file to the destination path.
func decompressGzip(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	gzReader, err := gzip.NewReader(srcFile)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, gzReader)
	return err
}

// parseTime parses a time string as RFC3339 or Unix seconds.
func parseTime(s string) (time.Time, error) {
	// Try RFC3339 first.
	t, err := time.Parse(time.RFC3339, s)
	if err == nil {
		return t, nil
	}

	// Try Unix seconds.
	unix, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse %q as RFC3339 or Unix seconds", s)
	}
	return time.Unix(unix, 0), nil
}

// deduplicateChunkRefs removes duplicate chunk references.
func deduplicateChunkRefs(refs []logproto.ChunkRef) []logproto.ChunkRef {
	type key struct {
		Fingerprint uint64
		From        model.Time
		Through     model.Time
		Checksum    uint32
	}
	seen := make(map[key]struct{}, len(refs))
	result := make([]logproto.ChunkRef, 0, len(refs))
	for _, ref := range refs {
		k := key{ref.Fingerprint, ref.From, ref.Through, ref.Checksum}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		result = append(result, ref)
	}
	return result
}

func exitErr(during string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "error during %s: %v\n", during, err)
		errStr := err.Error()
		if strings.Contains(errStr, "oauth2") ||
			strings.Contains(errStr, "invalid_grant") ||
			strings.Contains(errStr, "invalid_rapt") ||
			strings.Contains(errStr, "reauth") ||
			strings.Contains(errStr, "credentials") ||
			strings.Contains(errStr, "token") {
			fmt.Fprintf(os.Stderr, "\nhint: for GCS, try running 'gcloud auth application-default login' to set up Application Default Credentials\n")
			fmt.Fprintf(os.Stderr, "      (note: 'gcloud auth login' alone is NOT sufficient for Go client libraries)\n")
		}
		os.Exit(1)
	}
}

// Ensure we import prometheus for side effects and validation.
var _ = prometheus.DefaultRegisterer
var _ = validation.Limits{}
