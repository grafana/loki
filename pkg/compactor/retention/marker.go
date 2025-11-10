package retention

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"go.etcd.io/bbolt"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
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

	count               int64
	currentFileCount    int64
	curFileName         string
	workDir             string
	markerStorageClient client.ObjectClient

	buf []byte
}

func NewMarkerWriter(markerStorageClient client.ObjectClient) (MarkerStorageWriter, error) {
	msw := &markerStorageWriter{
		markerStorageClient: markerStorageClient,
		currentFileCount:    0,
		buf:                 make([]byte, 8),
	}

	return msw, msw.createFile()
}

func (m *markerStorageWriter) createFile() error {
	fileName := filepath.Join(os.TempDir(), fmt.Sprint(time.Now().UnixNano()))
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

	defer func() {
		if err := os.Remove(m.curFileName); err != nil {
			level.Error(util_log.Logger).Log("msg", "error removing current marker file", "file", m.curFileName)
		}
	}()

	f, err := os.ReadFile(m.curFileName)
	if err != nil {
		return err
	}

	return m.markerStorageClient.PutObject(context.Background(), filepath.Base(m.curFileName), bytes.NewReader(f))
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
	markerStorageClient client.ObjectClient
	maxParallelism      int
	minAgeFile          time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	sweeperMetrics *sweeperMetrics
}

func newMarkerReader(markerStorageClient client.ObjectClient, maxParallelism int, minAgeFile time.Duration, sweeperMetrics *sweeperMetrics) (*markerProcessor, error) {
	ctx, cancel := context.WithCancel(context.Background())
	return &markerProcessor{
		markerStorageClient: markerStorageClient,
		ctx:                 ctx,
		cancel:              cancel,
		maxParallelism:      maxParallelism,
		minAgeFile:          minAgeFile,
		sweeperMetrics:      sweeperMetrics,
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
			names, times, err := r.availableFiles()
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to list marks files", "err", err)
				continue
			}
			if len(names) == 0 {
				level.Info(util_log.Logger).Log("msg", "no marks file found")
				r.sweeperMetrics.markerFileCurrentTime.Set(0)
				continue
			}
			for i, name := range names {
				level.Debug(util_log.Logger).Log("msg", "processing mark file", "name", name)
				if r.ctx.Err() != nil {
					return
				}
				r.sweeperMetrics.markerFileCurrentTime.Set(float64(times[i].UnixNano()) / 1e9)
				allChunksDeleted, err := r.processFile(name, deleteFunc)
				if err != nil {
					level.Warn(util_log.Logger).Log("msg", "failed to process marks", "name", name, "err", err)
					continue
				}
				if allChunksDeleted {
					// delete marks file if all chunks were deleted successfully.
					if err := r.deleteMarksFile(name); err != nil {
						level.Warn(util_log.Logger).Log("msg", "failed to delete marks", "name", name, "err", err)
					}
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
			names, _, err := r.availableFiles()
			if err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to list marks path", "err", err)
				continue
			}
			r.sweeperMetrics.markerFilesCurrent.Set(float64(len(names)))
		}
	}()
}

func (r *markerProcessor) processFile(name string, deleteFunc func(ctx context.Context, chunkId []byte) error) (bool, error) {
	var (
		wg               sync.WaitGroup
		queue            = make(chan *keyPair)
		allChunksDeleted = atomic.NewBool(true)
	)

	tempFile, err := os.CreateTemp("", name)
	if err != nil {
		return false, err
	}

	defer func() {
		if err := os.Remove(tempFile.Name()); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to temp file", "file", tempFile.Name(), "err", err)
		}
	}()

	reader, _, err := r.markerStorageClient.GetObject(r.ctx, name)
	if err != nil {
		return false, err
	}
	defer reader.Close()

	_, err = io.Copy(tempFile, reader)
	if err != nil {
		return false, err
	}

	dbView, err := shipper_util.SafeOpenBoltdbFile(tempFile.Name())
	if err != nil {
		return false, err
	}
	// we don't need to force sync to file, we just view the file.
	dbView.NoSync = true
	defer func() {
		if err := dbView.Close(); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to close db view", "err", err)
		}
	}()

	for i := 0; i < r.maxParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for key := range queue {
				if err := deleteFunc(r.ctx, key.value.Bytes()); err != nil {
					allChunksDeleted.Store(false)
					level.Warn(util_log.Logger).Log("msg", "failed to delete key", "key", key.key.String(), "value", key.value.String(), "err", err)
				}
				putKeyBuffer(key)
			}
		}()
	}

	err = dbView.View(func(tx *bbolt.Tx) error {
		defer func() {
			close(queue)
			wg.Wait()
		}()

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
	if err != nil {
		return false, err
	}

	return allChunksDeleted.Load(), nil
}

func (r *markerProcessor) deleteMarksFile(name string) error {
	r.sweeperMetrics.markerFilesDeletedTotal.Inc()
	return r.markerStorageClient.DeleteObject(r.ctx, name)
}

// availableFiles returns markers file names in chronological order, skipping files that are not old enough.
func (r *markerProcessor) availableFiles() ([]string, []time.Time, error) {
	var found []string
	var creationTime []time.Time
	objects, _, err := r.markerStorageClient.List(r.ctx, "", "/")
	if err != nil {
		return nil, nil, err
	}

	sort.Slice(objects, func(i, j int) bool {
		return objects[i].Key < objects[j].Key
	})

	for _, object := range objects {
		i, err := strconv.ParseInt(object.Key, 10, 64)
		if err != nil {
			level.Warn(util_log.Logger).Log("msg", "wrong file name", "path", object.Key, "err", err)
			continue
		}

		if time.Since(time.Unix(0, i)) > r.minAgeFile {
			found = append(found, object.Key)
			creationTime = append(creationTime, time.Unix(0, i))
		}
	}
	if len(found) == 0 {
		return nil, nil, nil
	}

	return found, creationTime, nil
}

func (r *markerProcessor) Stop() {
	r.cancel()
	r.wg.Wait()
}
