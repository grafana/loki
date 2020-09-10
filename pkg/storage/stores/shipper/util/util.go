package util

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/grafana/loki/pkg/chunkenc"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
)

type StorageClient interface {
	GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
}

// GetFileFromStorage downloads a file from storage to given location.
func GetFileFromStorage(ctx context.Context, storageClient StorageClient, objectKey, destination string) error {
	readCloser, err := storageClient.GetObject(ctx, objectKey)
	if err != nil {
		return err
	}

	defer func() {
		if err := readCloser.Close(); err != nil {
			level.Error(util.Logger)
		}
	}()

	f, err := os.Create(destination)
	if err != nil {
		return err
	}

	var objectReader io.Reader = readCloser
	if strings.HasSuffix(objectKey, ".gz") {
		decompressedReader := chunkenc.Gzip.GetReader(readCloser)
		defer chunkenc.Gzip.PutReader(decompressedReader)

		objectReader = decompressedReader
	}

	_, err = io.Copy(f, objectReader)
	if err != nil {
		return err
	}

	level.Info(util.Logger).Log("msg", fmt.Sprintf("downloaded file %s", objectKey))

	return f.Sync()
}

func GetDBNameFromObjectKey(objectKey string) (string, error) {
	ss := strings.Split(objectKey, "/")

	if len(ss) != 2 {
		return "", fmt.Errorf("invalid object key: %v", objectKey)
	}
	if ss[1] == "" {
		return "", fmt.Errorf("empty db name, object key: %v", objectKey)
	}
	return ss[1], nil
}

func BuildObjectKey(tableName, uploader, dbName string) string {
	// Files are stored with <table-name>/<uploader>-<db-name>
	objectKey := fmt.Sprintf("%s/%s-%s", tableName, uploader, dbName)

	// if the file is a migrated one then don't add its name to the object key otherwise we would re-upload them again here with a different name.
	if tableName == dbName {
		objectKey = fmt.Sprintf("%s/%s", tableName, uploader)
	}

	return objectKey
}

func CompressFile(src, dest string) error {
	level.Info(util.Logger).Log("msg", "compressing the file", "src", src, "dest", dest)
	uncompressedFile, err := os.Open(src)
	if err != nil {
		return err
	}

	defer func() {
		if err := uncompressedFile.Close(); err != nil {
			level.Error(util.Logger).Log("msg", "failed to close uncompressed file", "path", src, "err", err)
		}
	}()

	compressedFile, err := os.Create(dest)
	if err != nil {
		return err
	}

	defer func() {
		if err := compressedFile.Close(); err != nil {
			level.Error(util.Logger).Log("msg", "failed to close compressed file", "path", dest, "err", err)
		}
	}()

	compressedWriter := chunkenc.Gzip.GetWriter(compressedFile)
	defer chunkenc.Gzip.PutWriter(compressedWriter)

	_, err = io.Copy(compressedWriter, uncompressedFile)
	if err != nil {
		return err
	}

	err = compressedWriter.Close()
	if err == nil {
		return err
	}

	return compressedFile.Sync()
}
