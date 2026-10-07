package indexshipper

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	indexstore "github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/downloads"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/uploads"
	"github.com/grafana/loki/v3/pkg/util/flagext"
)

type Mode string

const (
	// ModeReadWrite is to allow both read and write
	ModeReadWrite = Mode("RW")
	// ModeReadOnly is to allow only read operations
	ModeReadOnly = Mode("RO")
	// ModeWriteOnly is to allow only write operations
	ModeWriteOnly = Mode("WO")
	// ModeDisabled is a no-op implementation which does nothing & does not error.
	// It's used by the blockbuilder which handles index operations independently.
	ModeDisabled = Mode("NO")

	// FilesystemObjectStoreType holds the periodic config type for the filesystem store
	FilesystemObjectStoreType = "filesystem"

	// UploadInterval defines interval for when we check if there are new index files to upload.
	// It's also used to snapshot the currently written index tables so the snapshots can be used for reads.
	UploadInterval = 1 * time.Minute
)

// IndexReaderMode selects the implementation used to read TSDB index files from disk.
type IndexReaderMode string

const (
	// IndexReaderModeMmap memory-maps the index file.
	// This is the historical default.
	// Mmap page faults are invisible to the Go runtime, which causes a
	// thread to be locked for the duration of the page fault.
	IndexReaderModeMmap IndexReaderMode = "mmap"
	// IndexReaderModeStream serves reads via schedulable file I/O so the
	// runtime can observe blocking.
	IndexReaderModeStream IndexReaderMode = "stream"
)

// DefaultIndexReaderMode is the mode used when none is configured.
const DefaultIndexReaderMode = IndexReaderModeMmap

// DefaultStreamingIndexMaxIdleFileHandles is the number of idle file handles
// the stream reader keeps per index file when none is configured.
const DefaultStreamingIndexMaxIdleFileHandles = tsdbindex.DefaultMaxIdleFileHandles

type Index interface {
	Close() error
}

type IndexShipper interface {
	// AddIndex adds an immutable index to a logical table which would eventually get uploaded to the object store.
	AddIndex(tableName, userID string, index index.Index) error
	// ForEach lets us iterates through each index file in a table for a specific user.
	// On the write path, it would iterate on the files given to the shipper for uploading, until they eventually get dropped from local disk.
	// On the read path, it would iterate through the files if already downloaded else it would download and iterate through them.
	ForEach(ctx context.Context, tableName, userID string, callback index.ForEachIndexCallback) error
	ForEachConcurrent(ctx context.Context, tableName, userID string, callback index.ForEachIndexCallback) error
	// FlushIndexes synchronously uploads any pending index files to object storage.
	FlushIndexes(ctx context.Context) error
	// TriggerSync starts a background sync (refreshing the list cache first) if
	// none is already in progress. It returns true if a new sync was started.
	TriggerSync() bool
	// SyncStatus reports the current/last sync status.
	SyncStatus() indexstore.SyncStatus
	// PreloadIndexes runs the initial query readiness. It is needed only with
	// Config.DelayQueryReadinessUntilPreload; otherwise that runs at
	// construction.
	PreloadIndexes(ctx context.Context) error
	Stop()
}

// InMemoryPlacement selects which downloaded TSDB index files may be held in memory.
type InMemoryPlacement string

const (
	// InMemoryPlacementAll allows every downloaded index file to be held in
	// memory, as long as the budget has room.
	InMemoryPlacementAll InMemoryPlacement = "all"
	// InMemoryPlacementOwnedQueryReady allows only the files of index sets
	// kept ready for queries (preloaded, or loaded by the periodic query
	// readiness run) to be held in memory. With per-index ownership on the
	// index gateway, those are the indexes it owns. Files downloaded on demand
	// by a query are read with index_reader_mode.
	InMemoryPlacementOwnedQueryReady InMemoryPlacement = "owned_query_ready"
)

// InMemoryIndexConfig configures the experimental tier that holds downloaded
// TSDB index files in memory, up to a byte budget. Files that are not held in
// memory are read with the configured index_reader_mode.
type InMemoryIndexConfig struct {
	Enabled   bool              `yaml:"enabled"`
	MaxBytes  flagext.ByteSize  `yaml:"max_bytes"`
	Placement InMemoryPlacement `yaml:"placement"`
}

// RegisterFlagsWithPrefix registers flags.
func (cfg *InMemoryIndexConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false,
		"Experimental. Hold downloaded TSDB index files in memory, up to max-bytes. Files that do not fit are read with -shipper.index-reader-mode.")
	f.Var(&cfg.MaxBytes, prefix+"max-bytes",
		"Experimental. Maximum total size of the TSDB index files held in memory. Must be greater than zero when the in-memory index is enabled.")
	f.StringVar((*string)(&cfg.Placement), prefix+"placement", string(InMemoryPlacementAll),
		"Experimental. Which downloaded TSDB index files may be held in memory. Supported values: all (every file), owned_query_ready (only files of index sets kept ready for queries, which with per-index ownership are the indexes the index gateway owns; files downloaded on demand by a query are read with -shipper.index-reader-mode).")
}

// Validate validates the config.
func (cfg *InMemoryIndexConfig) Validate() error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.MaxBytes == 0 {
		return fmt.Errorf("shipper.in-memory-index.max-bytes must be greater than zero when the in-memory index is enabled")
	}
	if _, err := cfg.placementFunc(); err != nil {
		return err
	}
	return nil
}

func (cfg *InMemoryIndexConfig) placementFunc() (tsdbindex.PlacementFunc, error) {
	switch cfg.Placement {
	case InMemoryPlacementAll, InMemoryPlacementOwnedQueryReady:
		// owned_query_ready places every query ready file; the others are
		// opened with OnDemandReaderOptions.
		return tsdbindex.PlaceAll, nil
	default:
		return nil, fmt.Errorf("invalid shipper.in-memory-index.placement %q, must be one of: all, owned_query_ready", cfg.Placement)
	}
}

// placementDependsOnQueryReadiness reports whether files are opened
// differently depending on index.OpenOptions.QueryReady.
func (cfg *InMemoryIndexConfig) placementDependsOnQueryReadiness() bool {
	return cfg.Enabled && cfg.Placement == InMemoryPlacementOwnedQueryReady
}

// OnDemandReaderOptions returns the reader options for downloaded TSDB index
// files that are not query ready (index.OpenOptions.QueryReady is false),
// given opts, the ones NewReaderOptions returned. With placement
// owned_query_ready these never go into memory, but still count in the
// in-memory index metrics. Otherwise they are opts.
func OnDemandReaderOptions(cfg Config, opts tsdbindex.ReaderOptions) tsdbindex.ReaderOptions {
	inMemory, ok := opts.(tsdbindex.InMemoryOptions)
	if !ok || !cfg.InMemoryIndex.placementDependsOnQueryReadiness() {
		return opts
	}
	inMemory.Placement = tsdbindex.PlaceNone
	return inMemory
}

type Config struct {
	ActiveIndexDirectory     string                    `yaml:"active_index_directory"`
	CacheLocation            string                    `yaml:"cache_location"`
	CacheTTL                 time.Duration             `yaml:"cache_ttl"`
	ResyncInterval           time.Duration             `yaml:"resync_interval"`
	QueryReadyNumDays        int                       `yaml:"query_ready_num_days"`
	DownloadTimeout          time.Duration             `yaml:"download_timeout"`
	IndexReaderMode          IndexReaderMode           `yaml:"index_reader_mode" category:"experimental"`
	IndexGatewayClientConfig indexgateway.ClientConfig `yaml:"index_gateway_client"`

	StreamingIndexMaxIdleFileHandles uint         `yaml:"streaming_index_max_idle_file_handles" category:"experimental"`
	PostingsCache                    cache.Config `yaml:"postings_cache" category:"experimental" doc:"description=Experimental. Caches expanded postings for downloaded per-tenant TSDB index files."`

	InMemoryIndex InMemoryIndexConfig `yaml:"in_memory_index" category:"experimental" doc:"hidden"`

	QueryReadyOverrides downloads.QueryReadyOverrides `yaml:"query_ready_overrides" category:"experimental" doc:"hidden"`

	IngesterName           string
	Mode                   Mode
	IngesterDBRetainPeriod time.Duration

	// TenantFilter, if set, limits query readiness to the tenants it returns
	// for each table. It is set by the index gateway with per-index ownership.
	TenantFilter downloads.TenantFilter `yaml:"-"`
	// DelayQueryReadinessUntilPreload moves the initial query readiness run
	// from construction to the first PreloadIndexes call, once TenantFilter can
	// answer. It is set by the index gateway with per-index ownership.
	DelayQueryReadinessUntilPreload bool `yaml:"-"`
	// DropFilter, if set, answers which tenants in a table this instance no
	// longer owns, so that their query ready index sets are evicted (see
	// downloads.Config.DropFilter). It is set by the index gateway with
	// per-index ownership.
	DropFilter downloads.TenantFilter `yaml:"-"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.PostingsCache.RegisterFlagsWithPrefix(prefix+"shipper.postings-cache.", "", f)
	cfg.IndexGatewayClientConfig.RegisterFlagsWithPrefix(prefix+"shipper.index-gateway-client", f)
	cfg.InMemoryIndex.RegisterFlagsWithPrefix(prefix+"shipper.in-memory-index.", f)
	cfg.QueryReadyOverrides.RegisterFlagsWithPrefix(prefix+"shipper.query-ready-overrides.", f)

	f.StringVar(&cfg.ActiveIndexDirectory, prefix+"shipper.active-index-directory", "", "Directory where ingesters would write index files which would then be uploaded by shipper to configured storage")
	f.StringVar(&cfg.CacheLocation, prefix+"shipper.cache-location", "", "Cache location for restoring index files from storage for queries")
	f.DurationVar(&cfg.CacheTTL, prefix+"shipper.cache-ttl", 24*time.Hour, "TTL for index files restored in cache for queries")
	f.DurationVar(&cfg.ResyncInterval, prefix+"shipper.resync-interval", 5*time.Minute, "Resync downloaded files with the storage")
	f.IntVar(&cfg.QueryReadyNumDays, prefix+"shipper.query-ready-num-days", 0, "Number of days of common index to be kept downloaded for queries. For per tenant index query readiness, use limits overrides config.")
	f.DurationVar(&cfg.DownloadTimeout, prefix+"shipper.download-timeout", time.Minute, "Timeout for downloading a table's initial set of index files from object storage when serving a query. "+
		"Raise this for tenants with large indexes when slow object-storage responses cause downloads to hit the deadline; lower it to fail queries faster when storage is degraded.")
	f.StringVar((*string)(&cfg.IndexReaderMode), prefix+"shipper.index-reader-mode", string(DefaultIndexReaderMode),
		"Experimental. Implementation used to read TSDB index files off disk. Supported values: mmap (memory-map the file, the historical default) or stream (experimental, not yet fully implemented).")
	f.UintVar(&cfg.StreamingIndexMaxIdleFileHandles, prefix+"shipper.streaming-index-max-idle-file-handles", DefaultStreamingIndexMaxIdleFileHandles,
		"Experimental. Number of idle file handles the stream index reader keeps open per index file. "+
			"Only applies when -shipper.index-reader-mode=stream. "+
			"Set to 0 to disable pooling.")
}

// IndexReaderOptions translates the flat, user-facing config into the reader options it describes.
func (cfg *Config) IndexReaderOptions() (tsdbindex.ReaderOptions, error) {
	switch cfg.IndexReaderMode {
	case IndexReaderModeStream:
		return tsdbindex.StreamOptions{MaxIdleFileHandles: cfg.StreamingIndexMaxIdleFileHandles}, nil
	case IndexReaderModeMmap:
		return tsdbindex.MmapOptions{}, nil
	default:
		return nil, fmt.Errorf("invalid shipper.index-reader-mode %q, must be one of mmap|stream", cfg.IndexReaderMode)
	}
}

// NewReaderOptions returns the reader options for downloaded TSDB index files.
// With the in-memory index disabled, these are exactly IndexReaderOptions.
// Otherwise they hold files in memory under a new budget, whose metrics are
// registered with reg, and fall back to IndexReaderOptions.
//
// Call it once per process: every caller gets its own budget.
func NewReaderOptions(cfg Config, reg prometheus.Registerer) (tsdbindex.ReaderOptions, error) {
	fallback, err := cfg.IndexReaderOptions()
	if err != nil {
		return nil, err
	}
	if !cfg.InMemoryIndex.Enabled {
		return fallback, nil
	}
	if err := cfg.InMemoryIndex.Validate(); err != nil {
		return nil, err
	}
	placement, err := cfg.InMemoryIndex.placementFunc()
	if err != nil {
		return nil, err
	}
	return tsdbindex.InMemoryOptions{
		Budget:    tsdbindex.NewMemoryBudget(int64(cfg.InMemoryIndex.MaxBytes), reg),
		Placement: placement,
		Fallback:  fallback,
	}, nil
}

func (cfg *Config) Validate() error {
	// set the default value for mode
	if cfg.Mode == "" {
		cfg.Mode = ModeReadWrite
	}

	if _, err := cfg.IndexReaderOptions(); err != nil {
		return err
	}

	if err := cfg.InMemoryIndex.Validate(); err != nil {
		return err
	}

	if err := cfg.QueryReadyOverrides.Validate(); err != nil {
		return err
	}

	if cfg.DownloadTimeout <= 0 {
		return fmt.Errorf("shipper.download-timeout must be greater than zero, got %s", cfg.DownloadTimeout)
	}

	if err := cfg.IndexGatewayClientConfig.Validate(); err != nil {
		return fmt.Errorf("shipper.index-gateway-client: %w", err)
	}

	return nil
}

// GetUniqueUploaderName builds a unique uploader name using IngesterName + `-` + <nanosecond-timestamp>.
// The name is persisted in the configured ActiveIndexDirectory and reused when already exists.
func (cfg *Config) GetUniqueUploaderName() (string, error) {
	uploader := fmt.Sprintf("%s-%d", cfg.IngesterName, time.Now().UnixNano())

	uploaderFilePath := path.Join(cfg.ActiveIndexDirectory, "uploader", "name")
	if err := util.EnsureDirectory(path.Dir(uploaderFilePath)); err != nil {
		return "", err
	}

	_, err := os.Stat(uploaderFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		if err := os.WriteFile(uploaderFilePath, []byte(uploader), 0640); err != nil { // #nosec G306 -- this is fencing off the "other" permissions -- nosemgrep: incorrect-default-permissions
			return "", err
		}
	} else {
		ub, err := os.ReadFile(uploaderFilePath)
		if err != nil {
			return "", err
		}
		uploader = string(ub)
	}

	return uploader, nil
}

type indexShipper struct {
	cfg               Config
	openIndexFileFunc index.OpenIndexFileFunc
	uploadsManager    uploads.TableManager
	downloadsManager  downloads.TableManager

	logger   log.Logger
	stopOnce sync.Once
}

// NewIndexShipper creates a shipper for providing index store functionality using index files and object storage.
// It manages the whole life cycle of uploading the index and downloading the index at query time.
//
// Since IndexShipper is generic, which means it can be used to manage various index types under the same object storage and/or local disk path,
// it accepts ranges of table numbers(config.TableRanges) to be managed by the shipper.
// This is mostly useful on the read path to sync and manage specific index tables within the given table number ranges.
func NewIndexShipper(prefix string, cfg Config, storageClient client.ObjectClient, limits downloads.Limits,
	tenantFilter downloads.TenantFilter, open index.OpenIndexFileFunc, tableRangeToHandle config.TableRange, reg prometheus.Registerer, logger log.Logger) (IndexShipper, error) {
	switch cfg.Mode {
	case ModeDisabled:
		return Noop{}, nil
	case ModeReadOnly, ModeWriteOnly, ModeReadWrite:
	default:
		return nil, fmt.Errorf("invalid mode: %v", cfg.Mode)
	}
	shipper := indexShipper{
		cfg:               cfg,
		openIndexFileFunc: open,
		logger:            logger,
	}

	err := shipper.init(prefix, storageClient, limits, tenantFilter, tableRangeToHandle, reg)
	if err != nil {
		return nil, err
	}

	level.Info(shipper.logger).Log("msg", fmt.Sprintf("starting index shipper in %s mode", cfg.Mode))

	return &shipper, nil
}

func (s *indexShipper) init(prefix string, storageClient client.ObjectClient, limits downloads.Limits,
	tenantFilter downloads.TenantFilter, tableRangeToHandle config.TableRange, reg prometheus.Registerer) error {
	indexStorageClient := storage.NewIndexStorageClient(storageClient, prefix)

	if s.cfg.Mode != ModeReadOnly {
		cfg := uploads.Config{
			UploadInterval: UploadInterval,
			DBRetainPeriod: s.cfg.IngesterDBRetainPeriod,
		}
		uploadsManager, err := uploads.NewTableManager(cfg, indexStorageClient, reg, s.logger)
		if err != nil {
			return err
		}

		s.uploadsManager = uploadsManager
	}

	if s.cfg.Mode != ModeWriteOnly {
		cfg := downloads.Config{
			CacheDir:          s.cfg.CacheLocation,
			SyncInterval:      s.cfg.ResyncInterval,
			CacheTTL:          s.cfg.CacheTTL,
			QueryReadyNumDays: s.cfg.QueryReadyNumDays,
			DownloadTimeout:   s.cfg.DownloadTimeout,
			Limits:            limits,

			QueryReadyOverrides:             s.cfg.QueryReadyOverrides,
			DelayQueryReadinessUntilPreload: s.cfg.DelayQueryReadinessUntilPreload,
			DropFilter:                      s.cfg.DropFilter,
			ReopenOnQueryReady:              s.cfg.InMemoryIndex.placementDependsOnQueryReadiness(),
		}
		downloadsManager, err := downloads.NewTableManager(cfg, s.openIndexFileFunc, indexStorageClient, tenantFilter, tableRangeToHandle, reg, s.logger)
		if err != nil {
			return err
		}

		s.downloadsManager = downloadsManager
	}

	return nil
}

func (s *indexShipper) AddIndex(tableName, userID string, index index.Index) error {
	return s.uploadsManager.AddIndex(tableName, userID, index)
}

func (s *indexShipper) ForEach(ctx context.Context, tableName, userID string, callback index.ForEachIndexCallback) error {
	if s.downloadsManager != nil {
		if err := s.downloadsManager.ForEach(ctx, tableName, userID, callback); err != nil {
			return err
		}
	}

	if s.uploadsManager != nil {
		if err := s.uploadsManager.ForEach(tableName, userID, callback); err != nil {
			return err
		}
	}

	return nil
}

func (s *indexShipper) ForEachConcurrent(ctx context.Context, tableName, userID string, callback index.ForEachIndexCallback) error {

	g, ctx := errgroup.WithContext(ctx)
	// E.Welch not setting a bound on the errgroup here because we set one inside the downloadsManager.ForEachConcurrent

	if s.downloadsManager != nil {
		g.Go(func() error {
			return s.downloadsManager.ForEachConcurrent(ctx, tableName, userID, callback)
		})
	}

	if s.uploadsManager != nil {
		g.Go(func() error {
			// NB: uploadsManager doesn't yet implement ForEachConcurrent
			return s.uploadsManager.ForEach(tableName, userID, callback)
		})
	}

	return g.Wait()
}

func (s *indexShipper) FlushIndexes(ctx context.Context) error {
	if s.uploadsManager != nil {
		return s.uploadsManager.UploadTables(ctx)
	}
	return nil
}

func (s *indexShipper) TriggerSync() bool {
	if s.downloadsManager != nil {
		return s.downloadsManager.TriggerSync()
	}
	return false
}

func (s *indexShipper) PreloadIndexes(ctx context.Context) error {
	if s.downloadsManager != nil {
		return s.downloadsManager.EnsureQueryReadiness(ctx)
	}
	return nil
}

func (s *indexShipper) SyncStatus() indexstore.SyncStatus {
	if s.downloadsManager != nil {
		return s.downloadsManager.SyncStatus()
	}
	return indexstore.SyncStatus{}
}

func (s *indexShipper) Stop() {
	s.stopOnce.Do(s.stop)
}

func (s *indexShipper) stop() {
	if s.uploadsManager != nil {
		s.uploadsManager.Stop()
	}

	if s.downloadsManager != nil {
		s.downloadsManager.Stop()
	}
}

type Noop struct{}

func (Noop) AddIndex(_, _ string, _ index.Index) error { return nil }
func (Noop) ForEach(_ context.Context, _, _ string, _ index.ForEachIndexCallback) error {
	return nil
}
func (Noop) ForEachConcurrent(_ context.Context, _, _ string, _ index.ForEachIndexCallback) error {
	return nil
}
func (Noop) FlushIndexes(_ context.Context) error   { return nil }
func (Noop) TriggerSync() bool                      { return false }
func (Noop) SyncStatus() indexstore.SyncStatus      { return indexstore.SyncStatus{} }
func (Noop) PreloadIndexes(_ context.Context) error { return nil }
func (Noop) Stop()                                  {}
