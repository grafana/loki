package indexshipper

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/gatewayclient"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/uploads"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type Mode string

const (
	// ModeReadWrite is to allow both read and write
	ModeReadWrite = Mode("RW")
	// ModeReadOnly is to allow only read operations
	ModeReadOnly = Mode("RO")
	// ModeWriteOnly is to allow only write operations
	ModeWriteOnly = Mode("WO")

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
	Stop()
}

type Config struct {
	ActiveIndexDirectory     string                                 `yaml:"active_index_directory"`
	SharedStoreType          string                                 `yaml:"shared_store"`
	SharedStoreKeyPrefix     string                                 `yaml:"shared_store_key_prefix"`
	CacheLocation            string                                 `yaml:"cache_location"`
	CacheTTL                 time.Duration                          `yaml:"cache_ttl"`
	ResyncInterval           time.Duration                          `yaml:"resync_interval"`
	QueryReadyNumDays        int                                    `yaml:"query_ready_num_days"`
	IndexGatewayClientConfig gatewayclient.IndexGatewayClientConfig `yaml:"index_gateway_client"`
	UseBoltDBShipperAsBackup bool                                   `yaml:"use_boltdb_shipper_as_backup"`

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
	f.StringVar(&cfg.SharedStoreType, prefix+"shipper.shared-store", "", "Shared store for keeping index files. Supported types: gcs, s3, azure, cos, filesystem")
	f.StringVar(&cfg.SharedStoreKeyPrefix, prefix+"shipper.shared-store.key-prefix", "index/", "Prefix to add to Object Keys in Shared store. Path separator(if any) should always be a '/'. Prefix should never start with a separator but should always end with it")
	f.StringVar(&cfg.CacheLocation, prefix+"shipper.cache-location", "", "Cache location for restoring index files from storage for queries")
	f.DurationVar(&cfg.CacheTTL, prefix+"shipper.cache-ttl", 24*time.Hour, "TTL for index files restored in cache for queries")
	f.DurationVar(&cfg.ResyncInterval, prefix+"shipper.resync-interval", 5*time.Minute, "Resync downloaded files with the storage")
	f.IntVar(&cfg.QueryReadyNumDays, prefix+"shipper.query-ready-num-days", 0, "Number of days of common index to be kept downloaded for queries. For per tenant index query readiness, use limits overrides config.")
	f.BoolVar(&cfg.UseBoltDBShipperAsBackup, prefix+"shipper.use-boltdb-shipper-as-backup", false, "Use boltdb-shipper index store as backup for indexing chunks. When enabled, boltdb-shipper needs to be configured under storage_config")
}

func (cfg *Config) Validate() error {
	// set the default value for mode
	if cfg.Mode == "" {
		cfg.Mode = ModeReadWrite
	}
	return storage.ValidateSharedStoreKeyPrefix(cfg.SharedStoreKeyPrefix)
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
		if err := os.WriteFile(uploaderFilePath, []byte(uploader), 0o666); err != nil {
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

	stopOnce sync.Once
}

// NewIndexShipper creates a shipper for providing index store functionality using index files and object storage.
// It manages the whole life cycle of uploading the index and downloading the index at query time.
//
// Since IndexShipper is generic, which means it can be used to manage various index types under the same object storage and/or local disk path,
// it accepts ranges of table numbers(config.TableRanges) to be managed by the shipper.
// This is mostly useful on the read path to sync and manage specific index tables within the given table number ranges.
func NewIndexShipper(cfg Config, storageClient client.ObjectClient, limits downloads.Limits,
	ownsTenantFn downloads.IndexGatewayOwnsTenant, open index.OpenIndexFileFunc, tableRangesToHandle config.TableRanges, reg prometheus.Registerer) (IndexShipper, error) {
	switch cfg.Mode {
	case ModeReadOnly, ModeWriteOnly, ModeReadWrite:
	default:
		return nil, fmt.Errorf("invalid mode: %v", cfg.Mode)
	}
	shipper := indexShipper{
		cfg:               cfg,
		openIndexFileFunc: open,
	}

	err := shipper.init(storageClient, limits, ownsTenantFn, tableRangesToHandle, reg)
	if err != nil {
		return nil, err
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("starting index shipper in %s mode", cfg.Mode))

	return &shipper, nil
}

func (s *indexShipper) init(storageClient client.ObjectClient, limits downloads.Limits,
	ownsTenantFn downloads.IndexGatewayOwnsTenant, tableRangesToHandle config.TableRanges, reg prometheus.Registerer) error {
	indexStorageClient := storage.NewIndexStorageClient(storageClient, s.cfg.SharedStoreKeyPrefix)

	if s.cfg.Mode != ModeReadOnly {
		cfg := uploads.Config{
			UploadInterval: UploadInterval,
			DBRetainPeriod: s.cfg.IngesterDBRetainPeriod,
		}
		uploadsManager, err := uploads.NewTableManager(cfg, indexStorageClient, reg)
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
		downloadsManager, err := downloads.NewTableManager(cfg, s.openIndexFileFunc, indexStorageClient, ownsTenantFn, tableRangesToHandle, reg)
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
