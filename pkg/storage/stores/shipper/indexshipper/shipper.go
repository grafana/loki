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
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	indexstore "github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/downloads"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/uploads"
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
	Stop()
}

// IndexReaderMode selects the strategy the index-gateway uses to open on-disk
// TSDB index files.
type IndexReaderMode string

const (
	// IndexReaderModeMmap is the historical default: fileutil.OpenMmapFile.
	// Fast, memory-efficient (page-cache-evictable) but page faults block
	// goroutines invisibly to the Go runtime.
	IndexReaderModeMmap IndexReaderMode = "mmap"
	// IndexReaderModeBuffered reads the whole index file into a []byte on
	// open. Trades heap residency for schedulable I/O. See Phase 1 of the
	// nommap plan.
	IndexReaderModeBuffered IndexReaderMode = "buffered"
	// IndexReaderModeStreaming reads bytes from disk on demand through a
	// streamenc.DecbufFactory. Neither mmap'd nor fully resident. Phase 2.
	IndexReaderModeStreaming IndexReaderMode = "streaming"
)

type Config struct {
	ActiveIndexDirectory     string                    `yaml:"active_index_directory"`
	CacheLocation            string                    `yaml:"cache_location"`
	CacheTTL                 time.Duration             `yaml:"cache_ttl"`
	ResyncInterval           time.Duration             `yaml:"resync_interval"`
	QueryReadyNumDays        int                       `yaml:"query_ready_num_days"`
	IndexGatewayClientConfig indexgateway.ClientConfig `yaml:"index_gateway_client"`
	// IndexReaderMode selects the index-file access strategy: "mmap"
	// (default), "buffered" (os.ReadFile-backed byte slice), or "streaming"
	// (file-backed streamenc.DecbufFactory). Experimental — the two
	// non-mmap modes are still in ramp-up. Default is "mmap".
	IndexReaderMode IndexReaderMode `yaml:"index_reader_mode"`
	// DisableIndexMmap is a deprecated alias for IndexReaderMode="buffered".
	// If set alongside IndexReaderMode, IndexReaderMode wins. Remove once
	// downstream configs have migrated.
	DisableIndexMmap bool `yaml:"disable_index_mmap"`

	IngesterName           string
	Mode                   Mode
	IngesterDBRetainPeriod time.Duration
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.IndexGatewayClientConfig.RegisterFlagsWithPrefix(prefix+"shipper.index-gateway-client", f)

	f.StringVar(&cfg.ActiveIndexDirectory, prefix+"shipper.active-index-directory", "", "Directory where ingesters would write index files which would then be uploaded by shipper to configured storage")
	f.StringVar(&cfg.CacheLocation, prefix+"shipper.cache-location", "", "Cache location for restoring index files from storage for queries")
	f.DurationVar(&cfg.CacheTTL, prefix+"shipper.cache-ttl", 24*time.Hour, "TTL for index files restored in cache for queries")
	f.DurationVar(&cfg.ResyncInterval, prefix+"shipper.resync-interval", 5*time.Minute, "Resync downloaded files with the storage")
	f.IntVar(&cfg.QueryReadyNumDays, prefix+"shipper.query-ready-num-days", 0, "Number of days of common index to be kept downloaded for queries. For per tenant index query readiness, use limits overrides config.")
	f.BoolVar(&cfg.DisableIndexMmap, prefix+"shipper.disable-index-mmap", false, "Deprecated alias for -"+prefix+"shipper.index-reader-mode=buffered. Prefer setting index-reader-mode directly.")
	f.Var((*indexReaderModeValue)(&cfg.IndexReaderMode), prefix+"shipper.index-reader-mode", "Experimental: how the index-gateway opens on-disk TSDB index files. One of: mmap (default), buffered (os.ReadFile), streaming (file-backed on-demand reads). The non-mmap modes avoid invisible goroutine stalls from page faults; buffered uses more heap, streaming uses more syscalls.")
}

// indexReaderModeValue wraps IndexReaderMode so it can be used with flag.Var.
type indexReaderModeValue IndexReaderMode

func (m *indexReaderModeValue) String() string { return string(*m) }
func (m *indexReaderModeValue) Set(v string) error {
	switch IndexReaderMode(v) {
	case IndexReaderModeMmap, IndexReaderModeBuffered, IndexReaderModeStreaming:
		*m = indexReaderModeValue(v)
		return nil
	case "":
		*m = ""
		return nil
	default:
		return fmt.Errorf("invalid index-reader-mode %q (want mmap, buffered, or streaming)", v)
	}
}

func (cfg *Config) Validate() error {
	// set the default value for mode
	if cfg.Mode == "" {
		cfg.Mode = ModeReadWrite
	}

	// Backwards compatibility: DisableIndexMmap=true == IndexReaderMode=buffered.
	if cfg.IndexReaderMode == "" {
		if cfg.DisableIndexMmap {
			cfg.IndexReaderMode = IndexReaderModeBuffered
		} else {
			cfg.IndexReaderMode = IndexReaderModeMmap
		}
	}
	switch cfg.IndexReaderMode {
	case IndexReaderModeMmap, IndexReaderModeBuffered, IndexReaderModeStreaming:
	default:
		return fmt.Errorf("invalid index-reader-mode %q", cfg.IndexReaderMode)
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
			Limits:            limits,
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
func (Noop) FlushIndexes(_ context.Context) error { return nil }
func (Noop) TriggerSync() bool                    { return false }
func (Noop) SyncStatus() indexstore.SyncStatus    { return indexstore.SyncStatus{} }
func (Noop) Stop()                                {}
