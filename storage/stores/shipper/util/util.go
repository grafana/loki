package util

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	var objectReader io.Reader = readCloser
	err = decompress(objectReader, destination)
	level.Info(util.Logger).Log("msg", fmt.Sprintf("downloaded file %s", objectKey))

	return err
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

func decompress(src io.Reader, dst string) error {
	// ungzip
	zr, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	// untar
	tr := tar.NewReader(zr)
	// uncompress each element
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return err
		}
		// check the type
		switch header.Typeflag {
		// if its a dir and it doesn't exist create it (with 0755 permission)
		case tar.TypeDir:
			target := header.Name[strings.LastIndex(header.Name, "/")+1:]
			target = filepath.Join(dst, target)
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}
		// if it's a file create it (with same permission)
		case tar.TypeReg:
			target := header.Name[strings.LastIndex(header.Name, "/")-1:]
			ss := strings.Split(header.Name, "/")
			target = strings.Join(ss[len(ss)-2:], "/")
			target = filepath.Join(dst, target)
			fileToWrite, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			// copy over contents
			if _, err := io.Copy(fileToWrite, tr); err != nil {
				return err
			}
			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			fileToWrite.Close()
		}
	}

	//
	return nil
}
