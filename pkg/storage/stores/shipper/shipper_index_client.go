package shipper

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/uploads"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

type Mode int

const (
	// ModeReadWrite is to allow both read and write
	ModeReadWrite Mode = iota
	// ModeReadOnly is to allow only read operations
	ModeReadOnly
	// ModeWriteOnly is to allow only write operations
	ModeWriteOnly

	// FilesystemObjectStoreType holds the periodic config type for the filesystem store
	FilesystemObjectStoreType = "filesystem"

	// UploadInterval defines interval for when we check if there are new index files to upload.
	UploadInterval = 1 * time.Minute
)

func (m Mode) String() string {
	switch m {
	case ModeReadWrite:
		return "read-write"
	case ModeReadOnly:
		return "read-only"
	case ModeWriteOnly:
		return "write-only"
	}
	return "unknown"
}

type boltDBIndexClient interface {
	QueryWithCursor(_ context.Context, c *bbolt.Cursor, query index.Query, callback index.QueryPagesCallback) error
	NewWriteBatch() index.WriteBatch
	WriteToDB(ctx context.Context, db *bbolt.DB, bucketName []byte, writes local.TableWrites) error
	Stop()
}

type Config struct {
	ActiveIndexDirectory     string                   `yaml:"active_index_directory"`
	SharedStoreType          string                   `yaml:"shared_store"`
	SharedStoreKeyPrefix     string                   `yaml:"shared_store_key_prefix"`
	CacheLocation            string                   `yaml:"cache_location"`
	CacheTTL                 time.Duration            `yaml:"cache_ttl"`
	ResyncInterval           time.Duration            `yaml:"resync_interval"`
	QueryReadyNumDays        int                      `yaml:"query_ready_num_days"`
	IndexGatewayClientConfig IndexGatewayClientConfig `yaml:"index_gateway_client"`
	BuildPerTenantIndex      bool                     `yaml:"build_per_tenant_index"`
	IngesterName             string                   `yaml:"-"`
	Mode                     Mode                     `yaml:"-"`
	IngesterDBRetainPeriod   time.Duration            `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("boltdb.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.IndexGatewayClientConfig.RegisterFlagsWithPrefix(prefix+"shipper.index-gateway-client", f)

	f.StringVar(&cfg.ActiveIndexDirectory, prefix+"shipper.active-index-directory", "", "Directory where ingesters would write boltdb files which would then be uploaded by shipper to configured storage")
	f.StringVar(&cfg.SharedStoreType, prefix+"shipper.shared-store", "", "Shared store for keeping boltdb files. Supported types: gcs, s3, azure, filesystem")
	f.StringVar(&cfg.SharedStoreKeyPrefix, prefix+"shipper.shared-store.key-prefix", "index/", "Prefix to add to Object Keys in Shared store. Path separator(if any) should always be a '/'. Prefix should never start with a separator but should always end with it")
	f.StringVar(&cfg.CacheLocation, prefix+"shipper.cache-location", "", "Cache location for restoring boltDB files for queries")
	f.DurationVar(&cfg.CacheTTL, prefix+"shipper.cache-ttl", 24*time.Hour, "TTL for boltDB files restored in cache for queries")
	f.DurationVar(&cfg.ResyncInterval, prefix+"shipper.resync-interval", 5*time.Minute, "Resync downloaded files with the storage")
	f.IntVar(&cfg.QueryReadyNumDays, prefix+"shipper.query-ready-num-days", 0, "Number of days of common index to be kept downloaded for queries. For per tenant index query readiness, use limits overrides config.")
	f.BoolVar(&cfg.BuildPerTenantIndex, prefix+"shipper.build-per-tenant-index", false, "Build per tenant index files")
}

func (cfg *Config) Validate() error {
	return shipper_util.ValidateSharedStoreKeyPrefix(cfg.SharedStoreKeyPrefix)
}

type Shipper struct {
	cfg               Config
	boltDBIndexClient boltDBIndexClient
	uploadsManager    *uploads.TableManager
	downloadsManager  *downloads.TableManager

	metrics  *metrics
	stopOnce sync.Once
}

// NewShipper creates a shipper for syncing local objects with a store
func NewShipper(cfg Config, storageClient client.ObjectClient, limits downloads.Limits, registerer prometheus.Registerer) (index.Client, error) {
	shipper := Shipper{
		cfg:     cfg,
		metrics: newMetrics(registerer),
	}

	err := shipper.init(storageClient, limits, registerer)
	if err != nil {
		return nil, err
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("starting boltdb shipper in %s mode", cfg.Mode))

	return &shipper, nil
}

func (s *Shipper) init(storageClient client.ObjectClient, limits downloads.Limits, registerer prometheus.Registerer) error {
	// When we run with target querier we don't have ActiveIndexDirectory set so using CacheLocation instead.
	// Also it doesn't matter which directory we use since BoltDBIndexClient doesn't do anything with it but it is good to have a valid path.
	boltdbIndexClientDir := s.cfg.ActiveIndexDirectory
	if boltdbIndexClientDir == "" {
		boltdbIndexClientDir = s.cfg.CacheLocation
	}

	var err error
	s.boltDBIndexClient, err = local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: boltdbIndexClientDir})
	if err != nil {
		return err
	}

	indexStorageClient := storage.NewIndexStorageClient(storageClient, s.cfg.SharedStoreKeyPrefix)

	if s.cfg.Mode != ModeReadOnly {
		uploader, err := s.getUploaderName()
		if err != nil {
			return err
		}

		cfg := uploads.Config{
			Uploader:             uploader,
			IndexDir:             s.cfg.ActiveIndexDirectory,
			UploadInterval:       UploadInterval,
			DBRetainPeriod:       s.cfg.IngesterDBRetainPeriod,
			MakePerTenantBuckets: s.cfg.BuildPerTenantIndex,
		}
		uploadsManager, err := uploads.NewTableManager(cfg, s.boltDBIndexClient, indexStorageClient, registerer)
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
		downloadsManager, err := downloads.NewTableManager(cfg, s.boltDBIndexClient, indexStorageClient, registerer)
		if err != nil {
			return err
		}

		s.downloadsManager = downloadsManager
	}

	return nil
}

// we would persist uploader name in <active-index-directory>/uploader/name file so that we use same name on subsequent restarts to
// avoid uploading same files again with different name. If the filed does not exist we would create one with uploader name set to
// ingester name and startup timestamp so that we randomise the name and do not override files from other ingesters.
func (s *Shipper) getUploaderName() (string, error) {
	uploader := fmt.Sprintf("%s-%d", s.cfg.IngesterName, time.Now().UnixNano())

	uploaderFilePath := path.Join(s.cfg.ActiveIndexDirectory, "uploader", "name")
	if err := chunk_util.EnsureDirectory(path.Dir(uploaderFilePath)); err != nil {
		return "", err
	}

	_, err := os.Stat(uploaderFilePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		if err := ioutil.WriteFile(uploaderFilePath, []byte(uploader), 0o666); err != nil {
			return "", err
		}
	} else {
		ub, err := ioutil.ReadFile(uploaderFilePath)
		if err != nil {
			return "", err
		}
		uploader = string(ub)
	}

	return uploader, nil
}

func (s *Shipper) Stop() {
	s.stopOnce.Do(s.stop)
}

func (s *Shipper) stop() {
	if s.uploadsManager != nil {
		s.uploadsManager.Stop()
	}

	if s.downloadsManager != nil {
		s.downloadsManager.Stop()
	}

	s.boltDBIndexClient.Stop()
}

func (s *Shipper) NewWriteBatch() index.WriteBatch {
	return s.boltDBIndexClient.NewWriteBatch()
}

func (s *Shipper) BatchWrite(ctx context.Context, batch index.WriteBatch) error {
	return instrument.CollectedRequest(ctx, "WRITE", instrument.NewHistogramCollector(s.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
		return s.uploadsManager.BatchWrite(ctx, batch)
	})
}

func (s *Shipper) QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	return instrument.CollectedRequest(ctx, "Shipper.Query", instrument.NewHistogramCollector(s.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
		spanLogger := spanlogger.FromContext(ctx)

		if s.uploadsManager != nil {
			err := s.uploadsManager.QueryPages(ctx, queries, callback)
			if err != nil {
				return err
			}

			level.Debug(spanLogger).Log("queried", "uploads-manager")
		}

		if s.downloadsManager != nil {
			err := s.downloadsManager.QueryPages(ctx, queries, callback)
			if err != nil {
				return err
			}

			level.Debug(spanLogger).Log("queried", "downloads-manager")
		}

		return nil
	})
}
