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

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/uploads"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	// ModeReadWrite is to allow both read and write
	ModeReadWrite = iota
	// ModeReadOnly is to allow only read operations
	ModeReadOnly
	// ModeWriteOnly is to allow only write operations
	ModeWriteOnly

	// BoltDBShipperType holds the index type for using boltdb with shipper which keeps flushing them to a shared storage
	BoltDBShipperType = "boltdb-shipper"

	// FilesystemObjectStoreType holds the periodic config type for the filesystem store
	FilesystemObjectStoreType = "filesystem"

	// UploadInterval defines interval for when we check if there are new index files to upload.
	// It's also used to snapshot the currently written index tables so the snapshots can be used for reads.
	UploadInterval = 1 * time.Minute
)

type boltDBIndexClient interface {
	QueryWithCursor(_ context.Context, c *bbolt.Cursor, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
	NewWriteBatch() chunk.WriteBatch
	WriteToDB(ctx context.Context, db *bbolt.DB, writes local.TableWrites) error
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
	IngesterName             string                   `yaml:"-"`
	Mode                     int                      `yaml:"-"`
	IngesterDBRetainPeriod   time.Duration            `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.IndexGatewayClientConfig.RegisterFlagsWithPrefix("boltdb.shipper.index-gateway-client", f)

	f.StringVar(&cfg.ActiveIndexDirectory, "boltdb.shipper.active-index-directory", "", "Directory where ingesters would write boltdb files which would then be uploaded by shipper to configured storage")
	f.StringVar(&cfg.SharedStoreType, "boltdb.shipper.shared-store", "", "Shared store for keeping boltdb files. Supported types: gcs, s3, azure, filesystem")
	f.StringVar(&cfg.SharedStoreKeyPrefix, "boltdb.shipper.shared-store.key-prefix", "index/", "Prefix to add to Object Keys in Shared store. Path separator(if any) should always be a '/'. Prefix should never start with a separator but should always end with it")
	f.StringVar(&cfg.CacheLocation, "boltdb.shipper.cache-location", "", "Cache location for restoring boltDB files for queries")
	f.DurationVar(&cfg.CacheTTL, "boltdb.shipper.cache-ttl", 24*time.Hour, "TTL for boltDB files restored in cache for queries")
	f.DurationVar(&cfg.ResyncInterval, "boltdb.shipper.resync-interval", 5*time.Minute, "Resync downloaded files with the storage")
	f.IntVar(&cfg.QueryReadyNumDays, "boltdb.shipper.query-ready-num-days", 0, "Number of days of index to be kept downloaded for queries. Works only with tables created with 24h period.")
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
func NewShipper(cfg Config, storageClient chunk.ObjectClient, registerer prometheus.Registerer) (chunk.IndexClient, error) {
	shipper := Shipper{
		cfg:     cfg,
		metrics: newMetrics(registerer),
	}

	err := shipper.init(storageClient, registerer)
	if err != nil {
		return nil, err
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("starting boltdb shipper in %d mode", cfg.Mode))

	return &shipper, nil
}

func (s *Shipper) init(storageClient chunk.ObjectClient, registerer prometheus.Registerer) error {
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
			Uploader:       uploader,
			IndexDir:       s.cfg.ActiveIndexDirectory,
			UploadInterval: UploadInterval,
			DBRetainPeriod: s.cfg.IngesterDBRetainPeriod,
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
		if err := ioutil.WriteFile(uploaderFilePath, []byte(uploader), 0666); err != nil {
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

func (s *Shipper) NewWriteBatch() chunk.WriteBatch {
	return s.boltDBIndexClient.NewWriteBatch()
}

func (s *Shipper) BatchWrite(ctx context.Context, batch chunk.WriteBatch) error {
	return instrument.CollectedRequest(ctx, "WRITE", instrument.NewHistogramCollector(s.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
		return s.uploadsManager.BatchWrite(ctx, batch)
	})
}

func (s *Shipper) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	return instrument.CollectedRequest(ctx, "QUERY", instrument.NewHistogramCollector(s.metrics.requestDurationSeconds), instrument.ErrorCode, func(ctx context.Context) error {
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
