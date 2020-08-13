package shipper

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	pkg_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/stores/shipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/shipper/uploads"
	"github.com/grafana/loki/pkg/storage/stores/util"
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

	storageKeyPrefix = "index/"

	// UploadInterval defines interval for uploading active boltdb files from local which are being written to by ingesters.
	UploadInterval = 15 * time.Minute
)

type boltDBIndexClient interface {
	QueryDB(ctx context.Context, db *bbolt.DB, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
	NewWriteBatch() chunk.WriteBatch
	WriteToDB(ctx context.Context, db *bbolt.DB, writes local.TableWrites) error
	Stop()
}

type Config struct {
	ActiveIndexDirectory string        `yaml:"active_index_directory"`
	SharedStoreType      string        `yaml:"shared_store"`
	CacheLocation        string        `yaml:"cache_location"`
	CacheTTL             time.Duration `yaml:"cache_ttl"`
	ResyncInterval       time.Duration `yaml:"resync_interval"`
	IngesterName         string        `yaml:"-"`
	Mode                 int           `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.ActiveIndexDirectory, "boltdb.shipper.active-index-directory", "", "Directory where ingesters would write boltdb files which would then be uploaded by shipper to configured storage")
	f.StringVar(&cfg.SharedStoreType, "boltdb.shipper.shared-store", "", "Shared store for keeping boltdb files. Supported types: gcs, s3, azure, filesystem")
	f.StringVar(&cfg.CacheLocation, "boltdb.shipper.cache-location", "", "Cache location for restoring boltDB files for queries")
	f.DurationVar(&cfg.CacheTTL, "boltdb.shipper.cache-ttl", 24*time.Hour, "TTL for boltDB files restored in cache for queries")
	f.DurationVar(&cfg.ResyncInterval, "boltdb.shipper.resync-interval", 5*time.Minute, "Resync downloaded files with the storage")
}

type Shipper struct {
	cfg               Config
	boltDBIndexClient boltDBIndexClient
	uploadsManager    *uploads.TableManager
	downloadsManager  *downloads.TableManager

	metrics *metrics
}

// NewShipper creates a shipper for syncing local objects with a store
func NewShipper(cfg Config, storageClient chunk.ObjectClient, registerer prometheus.Registerer) (chunk.IndexClient, error) {
	err := chunk_util.EnsureDirectory(cfg.CacheLocation)
	if err != nil {
		return nil, err
	}

	shipper := Shipper{
		cfg:     cfg,
		metrics: newMetrics(registerer),
	}

	err = shipper.init(storageClient, registerer)
	if err != nil {
		return nil, err
	}

	level.Info(pkg_util.Logger).Log("msg", fmt.Sprintf("starting boltdb shipper in %d mode", cfg.Mode))

	return &shipper, nil
}

func (s *Shipper) init(storageClient chunk.ObjectClient, registerer prometheus.Registerer) error {
	uploader, err := s.getUploaderName()
	if err != nil {
		return err
	}

	s.boltDBIndexClient, err = local.NewBoltDBIndexClient(local.BoltDBConfig{Directory: s.cfg.ActiveIndexDirectory})
	if err != nil {
		return err
	}

	prefixedObjectClient := util.NewPrefixedObjectClient(storageClient, storageKeyPrefix)

	if s.cfg.Mode != ModeReadOnly {
		cfg := uploads.Config{
			Uploader:       uploader,
			IndexDir:       s.cfg.ActiveIndexDirectory,
			UploadInterval: UploadInterval,
		}
		uploadsManager, err := uploads.NewTableManager(cfg, s.boltDBIndexClient, prefixedObjectClient, registerer)
		if err != nil {
			return err
		}

		s.uploadsManager = uploadsManager
	}

	if s.cfg.Mode != ModeWriteOnly {
		cfg := downloads.Config{
			CacheDir:     s.cfg.CacheLocation,
			SyncInterval: s.cfg.ResyncInterval,
			CacheTTL:     s.cfg.CacheTTL,
		}
		downloadsManager, err := downloads.NewTableManager(cfg, s.boltDBIndexClient, prefixedObjectClient, registerer)
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
	return s.uploadsManager.BatchWrite(ctx, batch)
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
