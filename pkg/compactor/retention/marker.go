package retention

import (
	"context"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"go.etcd.io/bbolt"

	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	shipper_util "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var (
	minListMarkDelay = time.Minute
	maxMarkPerFile   = int64(100000)
)

type MarkerStorageWriter interface {
	Put(chunkID []byte) error
	Count() int64
	Close() error
}

type markerStorageWriter struct {
	db     *bbolt.DB
	tx     *bbolt.Tx
	bucket *bbolt.Bucket

	count            int64
	currentFileCount int64
	curFileName      string
	workDir          string

	buf []byte
}

func NewMarkerStorageWriter(workingDir string) (MarkerStorageWriter, error) {
	dir := filepath.Join(workingDir, MarkersFolder)
	err := chunk_util.EnsureDirectory(dir)
	if err != nil {
		return nil, err
	}

	msw := &markerStorageWriter{
		workDir:          dir,
		currentFileCount: 0,
		buf:              make([]byte, 8),
	}

	return msw, msw.createFile()
}

func (m *markerStorageWriter) createFile() error {
	fileName := filepath.Join(m.workDir, fmt.Sprint(time.Now().UnixNano()))
	db, err := shipper_util.SafeOpenBoltdbFile(fileName)
	if err != nil {
		return err
	}
	tx, err := db.Begin(true)
	if err != nil {
		return err
	}
	bucket, err := tx.CreateBucketIfNotExists(chunkBucket)
	if err != nil {
		return err
	}
	level.Info(util_log.Logger).Log("msg", "mark file created", "file", fileName)
	bucket.FillPercent = 1
	m.db = db
	m.tx = tx
	m.bucket = bucket
	m.curFileName = fileName
	m.currentFileCount = 0
	return nil
}

func (m *markerStorageWriter) closeFile() error {
	err := m.tx.Commit()
	if err != nil {
		return err
	}
	if err := m.db.Close(); err != nil {
		return err
	}
	// The marker file is empty we can remove.
	if m.currentFileCount == 0 {
		return os.Remove(m.curFileName)
	}
	return nil
}

func (m *markerStorageWriter) Put(chunkID []byte) error {
	if m.currentFileCount > maxMarkPerFile { // roll files when max marks is reached.
		if err := m.closeFile(); err != nil {
			return err
		}
		if err := m.createFile(); err != nil {
			return err
		}

	}
	// insert in order and fillpercent = 1
	id, err := m.bucket.NextSequence()
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint64(m.buf, id) // insert in order using sequence id.
	// boltdb requires the value to be valid for the whole tx.
	// so we make a copy.
	value := make([]byte, len(chunkID))
	copy(value, chunkID)
	if err := m.bucket.Put(m.buf, value); err != nil {
		return err
	}
	m.count++
	m.currentFileCount++
	return nil
}

func (m *markerStorageWriter) Count() int64 {
	return m.count
}

func (m *markerStorageWriter) Close() error {
	return m.closeFile()
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
	folder := filepath.Join(workingDir, MarkersFolder)
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
	level.Info(util_log.Logger).Log("msg", "mark processor started", "workers", r.maxParallelism, "delay", r.minAgeFile)
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
			if len(paths) == 0 {
				level.Info(util_log.Logger).Log("msg", "no marks file found")
				r.sweeperMetrics.markerFileCurrentTime.Set(0)
				continue
			}
			for i, path := range paths {
				level.Debug(util_log.Logger).Log("msg", "processing mark file", "path", path)
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
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		tick := func() {
			select {
			case <-r.ctx.Done():
			case <-ticker.C:
			}
		}
		for ; true; tick() {
			if r.ctx.Err() != nil {
				return
			}
			paths, _, err := r.availablePath()
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to list marks path", "path", r.folder, "err", err)
				continue
			}
			r.sweeperMetrics.markerFilesCurrent.Set(float64(len(paths)))
		}
	}()
}

func (r *markerProcessor) processPath(path string, deleteFunc func(ctx context.Context, chunkId []byte) error) error {
	var (
		wg    sync.WaitGroup
		queue = make(chan *keyPair)
	)
	// we use a copy to view the file so that we can read and update at the same time.
	viewFile, err := os.CreateTemp("", "marker-view-")
	if err != nil {
		return err
	}
	if err := viewFile.Close(); err != nil {
		return fmt.Errorf("failed to close view file: %w", err)
	}
	defer func() {
		if err := os.Remove(viewFile.Name()); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to delete view file", "file", viewFile.Name(), "err", err)
		}
	}()
	if _, err := copyFile(path, viewFile.Name()); err != nil {
		return fmt.Errorf("failed to copy view file: %w", err)
	}
	dbView, err := shipper_util.SafeOpenBoltdbFile(viewFile.Name())
	if err != nil {
		return err
	}
	// we don't need to force sync to file, we just view the file.
	dbView.NoSync = true
	defer func() {
		if err := dbView.Close(); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to close db view", "err", err)
		}
	}()
	dbUpdate, err := shipper_util.SafeOpenBoltdbFile(path)
	if err != nil {
		return err
	}
	dbUpdate.MaxBatchDelay = 5 * time.Millisecond
	defer func() {
		close(queue)
		wg.Wait()
		if err := dbUpdate.Close(); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to close db", "err", err)
		}
	}()
	for i := 0; i < r.maxParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range queue {
				if err := processKey(r.ctx, key, dbUpdate, deleteFunc); err != nil {
					level.Warn(util_log.Logger).Log("msg", "failed to delete key", "key", key.key.String(), "value", key.value.String(), "err", err)
				}
				putKeyBuffer(key)
			}
		}()
	}
	return dbView.View(func(tx *bbolt.Tx) error {
		b := tx.Bucket(chunkBucket)
		if b == nil {
			return nil
		}

		c := b.Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			key, err := getKeyPairBuffer(k, v)
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
	})
}

func processKey(ctx context.Context, key *keyPair, db *bbolt.DB, deleteFunc func(ctx context.Context, chunkId []byte) error) error {
	chunkID := key.value.Bytes()
	if err := deleteFunc(ctx, chunkID); err != nil {
		return err
	}
	return db.Batch(func(tx *bbolt.Tx) error {
		b := tx.Bucket(chunkBucket)
		if b == nil {
			return nil
		}
		return b.Delete(key.key.Bytes())
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

		if d.IsDir() && d.Name() != MarkersFolder {
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
