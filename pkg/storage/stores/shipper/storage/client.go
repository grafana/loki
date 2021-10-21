package storage

import (
	"context"
	"io"
	"path"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/storage/chunk"
)

const delimiter = "/"

// Client is used to manage boltdb index files in object storage, when using boltdb-shipper.
type Client interface {
	ListTables(ctx context.Context) ([]string, error)
	ListFiles(ctx context.Context, tableName string) ([]IndexFile, error)
	GetFile(ctx context.Context, tableName, fileName string) (io.ReadCloser, error)
	PutFile(ctx context.Context, tableName, fileName string, file io.ReadSeeker) error
	DeleteFile(ctx context.Context, tableName, fileName string) error
	IsFileNotFoundErr(err error) bool
	Stop()
}

type indexStorageClient struct {
	objectClient  chunk.ObjectClient
	storagePrefix string
}

type IndexFile struct {
	Name       string
	ModifiedAt time.Time
}

func NewIndexStorageClient(objectClient chunk.ObjectClient, storagePrefix string) Client {
	return &indexStorageClient{objectClient: objectClient, storagePrefix: storagePrefix}
}

func (s *indexStorageClient) ListTables(ctx context.Context) ([]string, error) {
	_, tables, err := s.objectClient.List(ctx, s.storagePrefix, delimiter)
	if err != nil {
		return nil, err
	}

	tableNames := make([]string, 0, len(tables))
	for _, table := range tables {
		tableNames = append(tableNames, path.Base(string(table)))
	}

	return tableNames, nil
}

func (s *indexStorageClient) ListFiles(ctx context.Context, tableName string) ([]IndexFile, error) {
	// The forward slash here needs to stay because we are trying to list contents of a directory without which
	// we will get the name of the same directory back with hosted object stores.
	// This is due to the object stores not having a concept of directories.
	objects, _, err := s.objectClient.List(ctx, s.storagePrefix+tableName+delimiter, delimiter)
	if err != nil {
		return nil, err
	}

	files := make([]IndexFile, 0, len(objects))
	for _, object := range objects {
		// The s3 client can also return the directory itself in the ListObjects.
		if strings.HasSuffix(object.Key, delimiter) {
			continue
		}
		files = append(files, IndexFile{
			Name:       path.Base(object.Key),
			ModifiedAt: object.ModifiedAt,
		})
	}

	return files, nil
}

func (s *indexStorageClient) GetFile(ctx context.Context, tableName, fileName string) (io.ReadCloser, error) {
	return s.objectClient.GetObject(ctx, s.storagePrefix+path.Join(tableName, fileName))
}

func (s *indexStorageClient) PutFile(ctx context.Context, tableName, fileName string, file io.ReadSeeker) error {
	return s.objectClient.PutObject(ctx, s.storagePrefix+path.Join(tableName, fileName), file)
}

func (s *indexStorageClient) DeleteFile(ctx context.Context, tableName, fileName string) error {
	return s.objectClient.DeleteObject(ctx, s.storagePrefix+path.Join(tableName, fileName))
}

func (s *indexStorageClient) IsFileNotFoundErr(err error) bool {
	return s.objectClient.IsObjectNotFoundErr(err)
}

func (s *indexStorageClient) Stop() {
	s.objectClient.Stop()
}
