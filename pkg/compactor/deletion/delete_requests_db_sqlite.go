package deletion

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

var errConnAlreadyClosed = fmt.Errorf("sqlite: close: already closed")

const (
	deleteRequestsDBSQLiteFileName   = DeleteRequestsTableName + ".sqlite"
	deleteRequestsDBSQLiteFileNameGZ = deleteRequestsDBSQLiteFileName + ".gz"
)

type sqlQuery struct {
	query    string
	execOpts *sqlitex.ExecOptions
}

type sqliteDB struct {
	indexStorageClient storage.Client
	path               string
	connPool           *sqlitex.Pool
	updatedAt          time.Time
	uploadedAt         time.Time

	done chan struct{}
	wg   sync.WaitGroup
}

func newSQLiteDB(workingDirectory string, indexStorageClient storage.Client) (*sqliteDB, error) {
	dbPath := filepath.Join(workingDirectory, fmt.Sprintf("%s.sqlite", DeleteRequestsTableName))
	if err := util.EnsureDirectory(filepath.Dir(dbPath)); err != nil {
		return nil, err
	}

	db := sqliteDB{
		indexStorageClient: indexStorageClient,
		path:               dbPath,
		done:               make(chan struct{}),
	}

	err := db.init()
	if err != nil {
		return nil, err
	}

	db.wg.Add(1)
	go db.loop()
	return &db, err
}

func (s *sqliteDB) Stop() {
	close(s.done)
	s.wg.Wait()

	if err := s.uploadFile(); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to upload delete requests file during shutdown", "err", err)
	}

	if err := s.connPool.Close(); err != nil {
		level.Error(util_log.Logger).Log("msg", "failed to close sqlite connection pool", "err", err)
	}
}

func (s *sqliteDB) init() error {
	tempFilePath := fmt.Sprintf("%s%s", s.path, tempFileSuffix)

	if err := os.Remove(tempFilePath); err != nil && !os.IsNotExist(err) {
		level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove temp file %s", tempFilePath), "err", err)
	}

	_, err := os.Stat(s.path)
	if err != nil {
		err = storage.DownloadFileFromStorage(s.path, true,
			true, storage.LoggerWithFilename(util_log.Logger, deleteRequestsDBSQLiteFileNameGZ), func() (io.ReadCloser, error) {
				return s.indexStorageClient.GetFile(context.Background(), DeleteRequestsTableName, deleteRequestsDBSQLiteFileNameGZ)
			})
		if err != nil && !s.indexStorageClient.IsFileNotFoundErr(err) {
			return err
		}
	}

	s.connPool, err = sqlitex.NewPool(s.path, sqlitex.PoolOptions{})
	return err
}

func (s *sqliteDB) loop() {
	uploadTicker := time.NewTicker(5 * time.Minute)
	defer uploadTicker.Stop()

	defer s.wg.Done()

	for {
		select {
		case <-uploadTicker.C:
			if err := s.uploadFile(); err != nil {
				level.Error(util_log.Logger).Log("msg", "failed to upload delete requests file", "err", err)
			}
		case <-s.done:
			return
		}
	}
}

func (s *sqliteDB) Exec(ctx context.Context, updatesData bool, queries ...sqlQuery) (err error) {
	if len(queries) == 0 {
		return nil
	}

	conn, err := s.connPool.Take(ctx)
	if err != nil {
		return err
	}
	defer s.connPool.Put(conn)

	// if there are more than 1 queries with updates, run it as a transaction
	if updatesData && len(queries) >= 1 {
		endFn := sqlitex.Transaction(conn)
		defer endFn(&err)
	}

	for _, query := range queries {
		if err := sqlitex.Execute(conn, query.query, query.execOpts); err != nil {
			return err
		}
	}

	if updatesData {
		s.updatedAt = time.Now()
	}
	return nil
}

func (s *sqliteDB) uploadFile() error {
	if s.uploadedAt.After(s.updatedAt) {
		level.Debug(util_log.Logger).Log("msg", "skipping uploading delete requests db since there have been no updates to the table since last upload")
		return nil
	}
	// we might get updates while we upload the file so store the current time and update the upload time after we are done uploading
	uploadTime := time.Now()
	level.Debug(util_log.Logger).Log("msg", "uploading delete requests db")

	conn, err := s.connPool.Take(context.Background())
	if err != nil {
		return err
	}
	defer s.connPool.Put(conn)

	// create a new temp database to copy in the current database
	tempDBPath := fmt.Sprintf("%s%s", s.path, tempFileSuffix)
	tempDBConn, err := sqlite.OpenConn(tempDBPath, sqlite.OpenReadWrite|sqlite.OpenCreate)
	if err != nil {
		return err
	}

	defer func() {
		if err := tempDBConn.Close(); err != nil && !errors.Is(err, errConnAlreadyClosed) {
			level.Error(util_log.Logger).Log("msg", "failed to close temp DB conn", "path", tempDBPath, "err", err)
		}

		if err := os.Remove(tempDBPath); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove temp DB", "path", tempDBPath, "err", err)
		}
	}()

	// use sqlite backup functionality to copy the database
	backup, err := sqlite.NewBackup(tempDBConn, "", conn, "")
	if err != nil {
		return err
	}
	_, err = backup.Step(-1)
	if err != nil {
		return err
	}

	if err := backup.Close(); err != nil {
		return err
	}
	if err := tempDBConn.Close(); err != nil {
		return err
	}

	// open the temp DB to read and compress it
	tempDB, err := os.Open(tempDBPath)
	if err != nil {
		return err
	}

	// create a new file to store the compressed DB
	compressedFilePath := fmt.Sprintf("%s%s", s.path, tempGZFileSuffix)
	compressedFile, err := os.Create(compressedFilePath)
	if err != nil {
		return err
	}

	defer func() {
		if err := compressedFile.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close compressed file", "path", compressedFilePath, "err", err)
		}

		if err := os.Remove(compressedFilePath); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to remove compressed file", "path", compressedFilePath, "err", err)
		}
	}()

	gzipPool := compression.GetWriterPool(compression.GZIP)
	compressedWriter := gzipPool.GetWriter(compressedFile)
	defer gzipPool.PutWriter(compressedWriter)

	// compress the DB
	_, err = io.Copy(compressedWriter, tempDB)
	if err != nil {
		return err
	}

	err = compressedWriter.Close()
	if err != nil {
		return err
	}

	// flush the file to disk and seek the file to the beginning.
	if err := compressedFile.Sync(); err != nil {
		return err
	}

	if _, err := compressedFile.Seek(0, 0); err != nil {
		return err
	}

	if err := s.indexStorageClient.PutFile(context.Background(), DeleteRequestsTableName, deleteRequestsDBSQLiteFileNameGZ, compressedFile); err != nil {
		return err
	}

	s.uploadedAt = uploadTime
	return nil
}

// cleanupSQLiteDB removes the SQLite DB from local disk as well as object storage
func cleanupSQLiteDB(workingDirectory string, indexStorageClient storage.Client) error {
	if err := filepath.WalkDir(workingDirectory, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), deleteRequestsDBSQLiteFileName) {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	err := indexStorageClient.DeleteFile(context.Background(), DeleteRequestsTableName, deleteRequestsDBSQLiteFileNameGZ)
	if err != nil && !indexStorageClient.IsFileNotFoundErr(err) {
		return err
	}

	return nil
}
