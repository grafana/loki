package util

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
)

const delimiter = "/"

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
			level.Error(util_log.Logger)
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

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("downloaded file %s", objectKey))

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
	level.Info(util_log.Logger).Log("msg", "compressing the file", "src", src, "dest", dest)
	uncompressedFile, err := os.Open(src)
	if err != nil {
		return err
	}

	defer func() {
		if err := uncompressedFile.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close uncompressed file", "path", src, "err", err)
		}
	}()

	compressedFile, err := os.Create(dest)
	if err != nil {
		return err
	}

	defer func() {
		if err := compressedFile.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to close compressed file", "path", dest, "err", err)
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

type result struct {
	boltdb *bbolt.DB
	err    error
}

// SafeOpenBoltdbFile will recover from a panic opening a DB file, and return the panic message in the err return object.
func SafeOpenBoltdbFile(path string) (*bbolt.DB, error) {
	result := make(chan *result)
	// Open the file in a separate goroutine because we want to change
	// the behavior of a Fault for just this operation and not for the
	// calling goroutine
	go safeOpenBoltDbFile(path, result)
	res := <-result
	return res.boltdb, res.err
}

func safeOpenBoltDbFile(path string, ret chan *result) {
	// boltdb can throw faults which are not caught by recover unless we turn them into panics
	debug.SetPanicOnFault(true)
	res := &result{}

	defer func() {
		if r := recover(); r != nil {
			res.err = fmt.Errorf("recovered from panic opening boltdb file: %v", r)
		}

		// Return the result object on the channel to unblock the calling thread
		ret <- res
	}()

	b, err := local.OpenBoltdbFile(path)
	res.boltdb = b
	res.err = err
}

// RemoveDirectories will return a new slice with any StorageObjects identified as directories removed.
func RemoveDirectories(incoming []chunk.StorageObject) []chunk.StorageObject {
	outgoing := make([]chunk.StorageObject, 0, len(incoming))
	for _, o := range incoming {
		if IsDirectory(o.Key) {
			continue
		}
		outgoing = append(outgoing, o)
	}
	return outgoing
}

// IsDirectory will return true if the string ends in a forward slash
func IsDirectory(key string) bool {
	return strings.HasSuffix(key, "/")
}

func ValidateSharedStoreKeyPrefix(prefix string) error {
	if prefix == "" {
		return errors.New("shared store key prefix must be set")
	} else if strings.Contains(prefix, "\\") {
		// When using windows filesystem as object store the implementation of ObjectClient in Cortex takes care of conversion of separator.
		// We just need to always use `/` as a path separator.
		return fmt.Errorf("shared store key prefix should only have '%s' as a path separator", delimiter)
	} else if strings.HasPrefix(prefix, delimiter) {
		return errors.New("shared store key prefix should never start with a path separator i.e '/'")
	} else if !strings.HasSuffix(prefix, delimiter) {
		return errors.New("shared store key prefix should end with a path separator i.e '/'")
	}

	return nil
}
