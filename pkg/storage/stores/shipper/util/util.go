package util

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/go-kit/kit/log/level"
	gzip "github.com/klauspost/pgzip"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	delimiter = "/"
	sep       = "\xff"
)

var (
	gzipReader = sync.Pool{}
	gzipWriter = sync.Pool{}
)

// getGzipReader gets or creates a new CompressionReader and reset it to read from src
func getGzipReader(src io.Reader) io.Reader {
	if r := gzipReader.Get(); r != nil {
		reader := r.(*gzip.Reader)
		err := reader.Reset(src)
		if err != nil {
			panic(err)
		}
		return reader
	}
	reader, err := gzip.NewReader(src)
	if err != nil {
		panic(err)
	}
	return reader
}

// putGzipReader places back in the pool a CompressionReader
func putGzipReader(reader io.Reader) {
	gzipReader.Put(reader)
}

// getGzipWriter gets or creates a new CompressionWriter and reset it to write to dst
func getGzipWriter(dst io.Writer) io.WriteCloser {
	if w := gzipWriter.Get(); w != nil {
		writer := w.(*gzip.Writer)
		writer.Reset(dst)
		return writer
	}
	return gzip.NewWriter(dst)
}

// PutWriter places back in the pool a CompressionWriter
func putGzipWriter(writer io.WriteCloser) {
	gzipWriter.Put(writer)
}

type IndexStorageClient interface {
	GetFile(ctx context.Context, tableName, fileName string) (io.ReadCloser, error)
}

// GetFileFromStorage downloads a file from storage to given location.
func GetFileFromStorage(ctx context.Context, storageClient IndexStorageClient, tableName, fileName, destination string, sync bool) error {
	readCloser, err := storageClient.GetFile(ctx, tableName, fileName)
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

	defer func() {
		if err := f.Close(); err != nil {
			level.Warn(util_log.Logger).Log("msg", "failed to close file", "file", destination)
		}
	}()
	var objectReader io.Reader = readCloser
	if strings.HasSuffix(fileName, ".gz") {
		decompressedReader := getGzipReader(readCloser)
		defer putGzipReader(decompressedReader)

		objectReader = decompressedReader
	}

	_, err = io.Copy(f, objectReader)
	if err != nil {
		return err
	}

	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("downloaded file %s from table %s", fileName, tableName))
	if sync {
		return f.Sync()
	}
	return nil
}

func BuildIndexFileName(tableName, uploader, dbName string) string {
	// Files are stored with <uploader>-<db-name>
	objectKey := fmt.Sprintf("%s-%s", uploader, dbName)

	// if the file is a migrated one then don't add its name to the object key otherwise we would re-upload them again here with a different name.
	if tableName == dbName {
		objectKey = uploader
	}

	return objectKey
}

func CompressFile(src, dest string, sync bool) error {
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

	compressedWriter := getGzipWriter(compressedFile)
	defer putGzipWriter(compressedWriter)

	_, err = io.Copy(compressedWriter, uncompressedFile)
	if err != nil {
		return err
	}

	err = compressedWriter.Close()
	if err == nil {
		return err
	}
	if sync {
		return compressedFile.Sync()
	}
	return nil
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

func QueryKey(q chunk.IndexQuery) string {
	ret := q.TableName + sep + q.HashValue

	if len(q.RangeValuePrefix) != 0 {
		ret += sep + string(q.RangeValuePrefix)
	}

	if len(q.RangeValueStart) != 0 {
		ret += sep + string(q.RangeValueStart)
	}

	if len(q.ValueEqual) != 0 {
		ret += sep + string(q.ValueEqual)
	}

	return ret
}
