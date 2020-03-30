package local

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/azure"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"
)

const (
	// ShipperModeReadWrite is to allow both read and write
	ShipperModeReadWrite = iota
	// ShipperModeReadOnly is to allow only read operations
	ShipperModeReadOnly
	// ShipperModeWriteOnly is to allow only write operations
	ShipperModeWriteOnly

	// ShipperFileUploadInterval defines interval for uploading active boltdb files from local which are being written to by ingesters.
	ShipperFileUploadInterval = 15 * time.Second

	cacheCleanupInterval = 24 * time.Hour

	// BoltDBShipperType holds the index type for using boltdb with shipper which keeps flushing them to a shared storage
	BoltDBShipperType = "boltdb-shipper"
)

type BoltDBGetter interface {
	GetDB(name string, operation int) (*bbolt.DB, error)
}

type StoreConfig struct {
	Store            string                  `yaml:"store"`
	AWSStorageConfig aws.StorageConfig       `yaml:"aws"`
	GCSConfig        gcp.GCSConfig           `yaml:"gcs"`
	FSConfig         local.FSConfig          `yaml:"filesystem"`
	Azure            azure.BlobStorageConfig `yaml:"azure"`
}

type ShipperConfig struct {
	ActiveIndexDirectory string        `yaml:"active_index_directory"`
	CacheLocation        string        `yaml:"cache_location"`
	CacheTTL             time.Duration `yaml:"cache_ttl"`
	StoreConfig          StoreConfig   `yaml:"store_config"`
	ResyncInterval       time.Duration `yaml:"resync_interval"`
	IngesterName         string        `yaml:"-"`
	Mode                 int           `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *ShipperConfig) RegisterFlags(f *flag.FlagSet) {
	storeFlagsPrefix := "boltdb.shipper."
	cfg.StoreConfig.AWSStorageConfig.RegisterFlagsWithPrefix(storeFlagsPrefix, f)
	cfg.StoreConfig.GCSConfig.RegisterFlagsWithPrefix(storeFlagsPrefix, f)
	cfg.StoreConfig.FSConfig.RegisterFlagsWithPrefix(storeFlagsPrefix, f)

	f.StringVar(&cfg.ActiveIndexDirectory, "boltdb.active-index-directory", "", "Directory where ingesters would write boltdb files which would then be uploaded by shipper to configured storage")
	f.StringVar(&cfg.StoreConfig.Store, "boltdb.shipper.store", "filesystem", "Store for keeping boltdb files")
	f.StringVar(&cfg.CacheLocation, "boltdb.shipper.cache-location", "", "Cache location for restoring boltDB files for queries")
	f.DurationVar(&cfg.CacheTTL, "boltdb.shipper.cache-ttl", 24*time.Hour, "TTL for boltDB files restored in cache for queries")
	f.DurationVar(&cfg.ResyncInterval, "boltdb.shipper.resync-interval", 5*time.Minute, "Resync downloaded files with the storage")
}

type downloadedFiles struct {
	mtime  time.Time
	boltdb *bbolt.DB
}

// filesCollection holds info about shipped boltdb index files by other uploaders(ingesters).
// It is generally used to hold boltdb files created by all the ingesters for same period i.e with same name.
// In the object store files are uploaded as <boltdb-filename>/<uploader-id> to manage files with same name from different ingesters
type filesCollection struct {
	sync.RWMutex
	lastUsedAt time.Time
	files      map[string]downloadedFiles
}

type Shipper struct {
	cfg          ShipperConfig
	boltDBGetter BoltDBGetter

	// downloadedPeriods holds mapping for period -> filesCollection.
	// Here period is name of the file created by ingesters for a specific period.
	downloadedPeriods    map[string]*filesCollection
	downloadedPeriodsMtx sync.RWMutex
	storageClient        chunk.ObjectClient
	done                 chan struct{}

	uploader              string
	uploadedFilesMtime    map[string]time.Time
	uploadedFilesMtimeMtx sync.RWMutex
}

// NewShipper creates a shipper for syncing local objects with a store
func NewShipper(cfg ShipperConfig, storageClient chunk.ObjectClient, boltDBGetter BoltDBGetter) (*Shipper, error) {
	if err := chunk_util.EnsureDirectory(cfg.CacheLocation); err != nil {
		return nil, err
	}

	shipper := Shipper{
		cfg:               cfg,
		boltDBGetter:      boltDBGetter,
		downloadedPeriods: map[string]*filesCollection{},
		storageClient:     storageClient,
		done:              make(chan struct{}),
		// We would use ingester name and startup timestamp for naming files while uploading so that
		// ingester does not override old files when using same id
		uploader:           fmt.Sprintf("%s-%d", cfg.IngesterName, time.Now().Unix()),
		uploadedFilesMtime: map[string]time.Time{},
	}

	go shipper.loop()

	return &shipper, nil
}

func (a *Shipper) loop() {
	resyncTicker := time.NewTicker(a.cfg.ResyncInterval)
	defer resyncTicker.Stop()

	uploadFilesTicker := time.NewTicker(ShipperFileUploadInterval)
	defer uploadFilesTicker.Stop()

	cacheCleanupTicker := time.NewTicker(cacheCleanupInterval)
	defer cacheCleanupTicker.Stop()

	for {
		select {
		case <-resyncTicker.C:
			err := a.syncLocalWithStorage(context.Background())
			if err != nil {
				level.Error(util.Logger).Log("msg", "error syncing local boltdb files with storage", "err", err)
			}
		case <-uploadFilesTicker.C:
			err := a.uploadFiles(context.Background())
			if err != nil {
				level.Error(util.Logger).Log("msg", "error pushing archivable files to store", "err", err)
			}
		case <-cacheCleanupTicker.C:
			err := a.cleanupCache()
			if err != nil {
				level.Error(util.Logger).Log("msg", "error cleaning up expired tables", "err", err)
			}
		case <-a.done:
			return
		}
	}
}

// Stop the shipper and push all the local files to the store
func (a *Shipper) Stop() {
	close(a.done)

	// Push all boltdb files to storage before returning
	err := a.syncLocalWithStorage(context.Background())
	if err != nil {
		level.Error(util.Logger).Log("msg", "error pushing archivable files to store", "err", err)
	}

	a.downloadedPeriodsMtx.Lock()
	defer a.downloadedPeriodsMtx.Unlock()

	for _, fc := range a.downloadedPeriods {
		fc.Lock()
		for _, fl := range fc.files {
			_ = fl.boltdb.Close()
		}
		fc.Unlock()
	}
}

// cleanupCache removes all the files for a period which has not be queried for using the configured TTL
func (a *Shipper) cleanupCache() error {
	a.downloadedPeriodsMtx.Lock()
	defer a.downloadedPeriodsMtx.Unlock()

	for period, fc := range a.downloadedPeriods {
		if fc.lastUsedAt.Add(a.cfg.CacheTTL).Before(time.Now()) {
			for uploader := range fc.files {
				if err := a.deleteFileFromCache(period, uploader, fc); err != nil {
					return err
				}
			}

			delete(a.downloadedPeriods, period)
		}
	}

	return nil
}

// syncLocalWithStorage syncs all the periods that we have in the cache with the storage
// i.e download new and updated files and remove files which were delete from the storage.
func (a *Shipper) syncLocalWithStorage(ctx context.Context) error {
	a.downloadedPeriodsMtx.RLock()
	defer a.downloadedPeriodsMtx.RUnlock()

	for period := range a.downloadedPeriods {
		if err := a.syncFilesForPeriod(ctx, period, a.downloadedPeriods[period]); err != nil {
			return err
		}
	}

	return nil
}

// deleteFileFromCache removes a file from cache.
// It takes care of locking the filesCollection, closing the boltdb file and removing the file from cache
func (a *Shipper) deleteFileFromCache(period, uploader string, fc *filesCollection) error {
	fc.Lock()
	defer fc.Unlock()

	if err := fc.files[uploader].boltdb.Close(); err != nil {
		return err
	}

	delete(fc.files, uploader)

	return os.Remove(path.Join(a.cfg.CacheLocation, period, uploader))
}

func (a *Shipper) getFilesCollection(ctx context.Context, period string, createIfNotExists bool) (*filesCollection, error) {
	a.downloadedPeriodsMtx.RLock()
	fc, ok := a.downloadedPeriods[period]
	a.downloadedPeriodsMtx.RUnlock()

	if !ok && createIfNotExists {
		a.downloadedPeriodsMtx.Lock()
		fc, ok = a.downloadedPeriods[period]
		if ok {
			a.downloadedPeriodsMtx.Unlock()
		}

		fc = &filesCollection{files: map[string]downloadedFiles{}}
		a.downloadedPeriods[period] = fc
	}

	return fc, nil
}

func (a *Shipper) forEach(ctx context.Context, period string, callback func(db *bbolt.DB) error) error {
	a.downloadedPeriodsMtx.RLock()
	fc, ok := a.downloadedPeriods[period]
	a.downloadedPeriodsMtx.RUnlock()

	if !ok {
		a.downloadedPeriodsMtx.Lock()
		fc, ok = a.downloadedPeriods[period]
		if ok {
			a.downloadedPeriodsMtx.Unlock()
		} else {
			fc = &filesCollection{files: map[string]downloadedFiles{}}
			a.downloadedPeriods[period] = fc
			a.downloadedPeriodsMtx.Unlock()

			if err := a.downloadFilesForPeriod(ctx, period, fc); err != nil {
				return err
			}
		}

	}

	fc.RLock()
	defer fc.RUnlock()

	fc.lastUsedAt = time.Now()

	for uploader := range fc.files {
		if err := callback(fc.files[uploader].boltdb); err != nil {
			return err
		}
	}

	return nil
}
