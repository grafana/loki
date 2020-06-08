package local

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	pkg_util "github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/stores/util"
)

const (
	// ShipperModeReadWrite is to allow both read and write
	ShipperModeReadWrite = iota
	// ShipperModeReadOnly is to allow only read operations
	ShipperModeReadOnly
	// ShipperModeWriteOnly is to allow only write operations
	ShipperModeWriteOnly

	// ShipperFileUploadInterval defines interval for uploading active boltdb files from local which are being written to by ingesters.
	ShipperFileUploadInterval = 15 * time.Minute

	// BoltDBShipperType holds the index type for using boltdb with shipper which keeps flushing them to a shared storage
	BoltDBShipperType = "boltdb-shipper"

	// FilesystemObjectStoreType holds the periodic config type for the filesystem store
	FilesystemObjectStoreType = "filesystem"

	cacheCleanupInterval = 24 * time.Hour
	storageKeyPrefix     = "index/"
)

type BoltDBGetter interface {
	GetDB(name string, operation int) (*bbolt.DB, error)
}

type ShipperConfig struct {
	ActiveIndexDirectory string        `yaml:"active_index_directory"`
	SharedStoreType      string        `yaml:"shared_store"`
	CacheLocation        string        `yaml:"cache_location"`
	CacheTTL             time.Duration `yaml:"cache_ttl"`
	ResyncInterval       time.Duration `yaml:"resync_interval"`
	IngesterName         string        `yaml:"-"`
	Mode                 int           `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *ShipperConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.ActiveIndexDirectory, "boltdb.shipper.active-index-directory", "", "Directory where ingesters would write boltdb files which would then be uploaded by shipper to configured storage")
	f.StringVar(&cfg.SharedStoreType, "boltdb.shipper.shared-store", "", "Shared store for keeping boltdb files. Supported types: gcs, s3, azure, filesystem")
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

	uploader              string
	uploadedFilesMtime    map[string]time.Time
	uploadedFilesMtimeMtx sync.RWMutex

	done    chan struct{}
	wait    sync.WaitGroup
	metrics *boltDBShipperMetrics
}

// NewShipper creates a shipper for syncing local objects with a store
func NewShipper(cfg ShipperConfig, storageClient chunk.ObjectClient, boltDBGetter BoltDBGetter, registerer prometheus.Registerer) (*Shipper, error) {
	err := chunk_util.EnsureDirectory(cfg.CacheLocation)
	if err != nil {
		return nil, err
	}

	shipper := Shipper{
		cfg:                cfg,
		boltDBGetter:       boltDBGetter,
		downloadedPeriods:  map[string]*filesCollection{},
		storageClient:      util.NewPrefixedObjectClient(storageClient, storageKeyPrefix),
		done:               make(chan struct{}),
		uploadedFilesMtime: map[string]time.Time{},
		metrics:            newBoltDBShipperMetrics(registerer),
	}

	shipper.uploader, err = shipper.getUploaderName()
	if err != nil {
		return nil, err
	}

	level.Info(pkg_util.Logger).Log("msg", fmt.Sprintf("starting boltdb shipper in %d mode", cfg.Mode))

	shipper.wait.Add(1)
	go shipper.loop()

	return &shipper, nil
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

func (s *Shipper) loop() {
	defer s.wait.Done()

	resyncTicker := time.NewTicker(s.cfg.ResyncInterval)
	defer resyncTicker.Stop()

	uploadFilesTicker := time.NewTicker(ShipperFileUploadInterval)
	defer uploadFilesTicker.Stop()

	cacheCleanupTicker := time.NewTicker(cacheCleanupInterval)
	defer cacheCleanupTicker.Stop()

	for {
		select {
		case <-resyncTicker.C:
			err := s.syncLocalWithStorage(context.Background())
			if err != nil {
				level.Error(pkg_util.Logger).Log("msg", "error syncing local boltdb files with storage", "err", err)
			}
		case <-uploadFilesTicker.C:
			err := s.uploadFiles(context.Background())
			if err != nil {
				level.Error(pkg_util.Logger).Log("msg", "error pushing archivable files to store", "err", err)
			}
		case <-cacheCleanupTicker.C:
			err := s.cleanupCache()
			if err != nil {
				level.Error(pkg_util.Logger).Log("msg", "error cleaning up expired tables", "err", err)
			}
		case <-s.done:
			return
		}
	}
}

// Stop the shipper and push all the local files to the store
func (s *Shipper) Stop() {
	close(s.done)
	s.wait.Wait()

	// Push all boltdb files to storage before returning
	err := s.uploadFiles(context.Background())
	if err != nil {
		level.Error(pkg_util.Logger).Log("msg", "error pushing archivable files to store", "err", err)
	}

	s.downloadedPeriodsMtx.Lock()
	defer s.downloadedPeriodsMtx.Unlock()

	for _, fc := range s.downloadedPeriods {
		fc.Lock()
		for _, fl := range fc.files {
			_ = fl.boltdb.Close()
		}
		fc.Unlock()
	}
}

// cleanupCache removes all the files for a period which has not be queried for using the configured TTL
func (s *Shipper) cleanupCache() error {
	s.downloadedPeriodsMtx.Lock()
	defer s.downloadedPeriodsMtx.Unlock()

	for period, fc := range s.downloadedPeriods {
		if fc.lastUsedAt.Add(s.cfg.CacheTTL).Before(time.Now()) {
			for uploader := range fc.files {
				if err := s.deleteFileFromCache(period, uploader, fc); err != nil {
					return err
				}
			}

			delete(s.downloadedPeriods, period)
		}
	}

	return nil
}

// syncLocalWithStorage syncs all the periods that we have in the cache with the storage
// i.e download new and updated files and remove files which were delete from the storage.
func (s *Shipper) syncLocalWithStorage(ctx context.Context) (err error) {
	s.downloadedPeriodsMtx.RLock()
	defer s.downloadedPeriodsMtx.RUnlock()

	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
		}
		s.metrics.filesDownloadOperationTotal.WithLabelValues(status).Inc()
	}()

	for period := range s.downloadedPeriods {
		if err := s.syncFilesForPeriod(ctx, period, s.downloadedPeriods[period]); err != nil {
			return err
		}
	}

	return
}

// deleteFileFromCache removes a file from cache.
// It takes care of locking the filesCollection, closing the boltdb file and removing the file from cache
func (s *Shipper) deleteFileFromCache(period, uploader string, fc *filesCollection) error {
	fc.Lock()
	defer fc.Unlock()

	if err := fc.files[uploader].boltdb.Close(); err != nil {
		return err
	}

	delete(fc.files, uploader)

	return os.Remove(path.Join(s.cfg.CacheLocation, period, uploader))
}

func (s *Shipper) forEach(ctx context.Context, period string, callback func(db *bbolt.DB) error) error {
	s.downloadedPeriodsMtx.RLock()
	fc, ok := s.downloadedPeriods[period]
	s.downloadedPeriodsMtx.RUnlock()

	if !ok {
		s.downloadedPeriodsMtx.Lock()
		fc, ok = s.downloadedPeriods[period]
		if ok {
			s.downloadedPeriodsMtx.Unlock()
		} else {
			level.Info(pkg_util.Logger).Log("msg", fmt.Sprintf("downloading all files for period %s", period))

			fc = &filesCollection{files: map[string]downloadedFiles{}}
			s.downloadedPeriods[period] = fc
			s.downloadedPeriodsMtx.Unlock()

			if err := s.downloadFilesForPeriod(ctx, period, fc); err != nil {
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
