package retention

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

var minListMarkDelay = time.Minute

type MarkerStorageWriter interface {
	Put(chunkID []byte) error
	Count() int64
	Close() error
}

type markerStorageWriter struct {
	db     *bbolt.DB
	tx     *bbolt.Tx
	bucket *bbolt.Bucket

	count    int64
	fileName string
}

func NewMarkerStorageWriter(workingDir string) (MarkerStorageWriter, error) {
	err := chunk_util.EnsureDirectory(filepath.Join(workingDir, markersFolder))
	if err != nil {
		return nil, err
	}
	fileName := filepath.Join(workingDir, markersFolder, fmt.Sprint(time.Now().UnixNano()))
	db, err := shipper_util.SafeOpenBoltdbFile(fileName)
	if err != nil {
		return nil, err
	}
	tx, err := db.Begin(true)
	if err != nil {
		return nil, err
	}
	bucket, err := tx.CreateBucketIfNotExists(chunkBucket)
	if err != nil {
		return nil, err
	}
	return &markerStorageWriter{
		db:       db,
		tx:       tx,
		bucket:   bucket,
		count:    0,
		fileName: fileName,
	}, err
}

func (m *markerStorageWriter) Put(chunkID []byte) error {
	if err := m.bucket.Put(chunkID, empty); err != nil {
		return err
	}
	m.count++
	return nil
}

func (m *markerStorageWriter) Count() int64 {
	return m.count
}

func (m *markerStorageWriter) Close() error {
	if err := m.tx.Commit(); err != nil {
		return err
	}
	if err := m.db.Close(); err != nil {
		return err
	}
	// The marker file is empty we can remove.
	if m.count == 0 {
		return os.Remove(m.fileName)
	}
	return nil
}

type MarkerProcessor interface {
	// Start starts parsing marks and calling deleteFunc for each.
	// If deleteFunc returns no error the mark is deleted from the storage.
	// Otherwise the mark will reappears in future iteration.
	Start(deleteFunc func(ctx context.Context, chunkId []byte) error)
	// Stop stops processing marks.
	Stop()
}

type markerProcessor struct {
	folder         string // folder where to find markers file.
	maxParallelism int
	minAgeFile     time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	sweeperMetrics *sweeperMetrics
}

func newMarkerStorageReader(workingDir string, maxParallelism int, minAgeFile time.Duration, sweeperMetrics *sweeperMetrics) (*markerProcessor, error) {
	folder := filepath.Join(workingDir, markersFolder)
	err := chunk_util.EnsureDirectory(folder)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &markerProcessor{
		folder:         folder,
		ctx:            ctx,
		cancel:         cancel,
		maxParallelism: maxParallelism,
		minAgeFile:     minAgeFile,
		sweeperMetrics: sweeperMetrics,
	}, nil
}

func (r *markerProcessor) Start(deleteFunc func(ctx context.Context, chunkId []byte) error) {
	r.wg.Wait() // only one start at a time.
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(minListMarkDelay)
		defer ticker.Stop()
		tick := func() {
			select {
			case <-r.ctx.Done():
			case <-ticker.C:
			}
		}
		// instant first tick
		for ; true; tick() {
			if r.ctx.Err() != nil {
				// cancelled
				return
			}
			paths, times, err := r.availablePath()
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to list marks path", "path", r.folder, "err", err)
				continue
			}
			r.sweeperMetrics.markerFilesCurrent.Set(float64(len(paths)))
			if len(paths) == 0 {
				level.Info(util_log.Logger).Log("msg", "No marks file found")
			}
			for i, path := range paths {
				level.Debug(util_log.Logger).Log("msg", "processing mark file:")
				if r.ctx.Err() != nil {
					return
				}
				r.sweeperMetrics.markerFileCurrentTime.Set(float64(times[i].UnixNano()) / 1e9)
				if err := r.processPath(path, deleteFunc); err != nil {
					level.Warn(util_log.Logger).Log("msg", "failed to process marks", "path", path, "err", err)
					continue
				}
				// delete if empty.
				if err := r.deleteEmptyMarks(path); err != nil {
					level.Warn(util_log.Logger).Log("msg", "failed to delete marks", "path", path, "err", err)
				}
			}

		}
	}()
}

func (r *markerProcessor) processPath(path string, deleteFunc func(ctx context.Context, chunkId []byte) error) error {
	var (
		wg    sync.WaitGroup
		queue = make(chan *bytes.Buffer)
	)
	db, err := shipper_util.SafeOpenBoltdbFile(path)
	if err != nil {
		return err
	}
	defer func() {
		close(queue)
		wg.Wait()
		db.Close()
	}()
	for i := 0; i < r.maxParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range queue {
				if err := processKey(r.ctx, key, db, deleteFunc); err != nil {
					level.Warn(util_log.Logger).Log("msg", "failed to delete key", "key", key.String(), "err", err)
				}
				putKeyBuffer(key)
			}
		}()
	}
	if err := db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(chunkBucket)
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, _ := c.First(); k != nil; k, _ = c.Next() {
			key, err := getKeyBuffer(k)
			if err != nil {
				return err
			}
			select {
			case queue <- key:
			case <-r.ctx.Done():
				return r.ctx.Err()
			}

		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func processKey(ctx context.Context, key *bytes.Buffer, db *bbolt.DB, deleteFunc func(ctx context.Context, chunkId []byte) error) error {
	keyData := key.Bytes()
	if err := deleteFunc(ctx, keyData); err != nil {
		return err
	}
	// we don't use a batch because it would force us to copy the key.
	// but we most likely want to do batch in the future.
	return db.Update(func(tx *bbolt.Tx) error {
		b := tx.Bucket(chunkBucket)
		if b == nil {
			return nil
		}
		return b.Delete(keyData)
	})
}

func (r *markerProcessor) deleteEmptyMarks(path string) error {
	db, err := shipper_util.SafeOpenBoltdbFile(path)
	if err != nil {
		return err
	}
	var empty bool
	err = db.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(chunkBucket)
		if b == nil {
			empty = true
			return nil
		}
		if k, _ := b.Cursor().First(); k == nil {
			empty = true
			return nil
		}

		return nil
	})
	db.Close()
	if err != nil {
		return err
	}
	if empty {
		r.sweeperMetrics.markerFilesDeletedTotal.Inc()
		return os.Remove(path)
	}
	return nil
}

// availablePath returns markers path in chronological order, skipping file that are not old enough.
func (r *markerProcessor) availablePath() ([]string, []time.Time, error) {
	found := []int64{}
	if err := filepath.WalkDir(r.folder, func(path string, d fs.DirEntry, err error) error {
		if d == nil || err != nil {
			return err
		}

		if d.IsDir() && d.Name() != markersFolder {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		i, err := strconv.ParseInt(base, 10, 64)
		if err != nil {
			level.Warn(util_log.Logger).Log("msg", "wrong file name", "path", path, "base", base, "err", err)
			return nil
		}

		if time.Since(time.Unix(0, i)) > r.minAgeFile {
			found = append(found, i)
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	if len(found) == 0 {
		return nil, nil, nil
	}
	sort.Slice(found, func(i, j int) bool { return found[i] < found[j] })
	res := make([]string, len(found))
	resTime := make([]time.Time, len(found))
	for i, f := range found {
		res[i] = filepath.Join(r.folder, fmt.Sprintf("%d", f))
		resTime[i] = time.Unix(0, f)
	}
	return res, resTime, nil
}

func (r *markerProcessor) Stop() {
	r.cancel()
	r.wg.Wait()
}
