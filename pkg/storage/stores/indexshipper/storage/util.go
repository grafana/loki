package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	gzip "github.com/klauspost/pgzip"
)

var (
	gzipReader = sync.Pool{}
)

// getGzipReader gets or creates a new CompressionReader and reset it to read from src
func getGzipReader(src io.Reader) (io.Reader, error) {
	if r := gzipReader.Get(); r != nil {
		reader := r.(*gzip.Reader)
		err := reader.Reset(src)
		if err != nil {
			return nil, err
		}
		return reader, nil
	}
	reader, err := gzip.NewReader(src)
	if err != nil {
		return nil, err
	}
	return reader, nil
}

// putGzipReader places back in the pool a CompressionReader
func putGzipReader(reader io.Reader) {
	gzipReader.Put(reader)
}

type GetFileFunc func() (io.ReadCloser, error)

// DownloadFileFromStorage downloads a file from storage to given location.
func DownloadFileFromStorage(destination string, decompressFile bool, sync bool, logger log.Logger, getFileFunc GetFileFunc) error {
	start := time.Now()
	readCloser, err := getFileFunc()
	if err != nil {
		return err
	}

	defer func() {
		if err := readCloser.Close(); err != nil {
			level.Error(logger).Log("msg", "failed to close read closer", "err", err)
		}
	}()

	f, err := os.Create(destination)
	if err != nil {
		return err
	}

	defer func() {
		if err := f.Close(); err != nil {
			level.Warn(logger).Log("msg", "failed to close file", "file", destination)
		}
	}()
	var objectReader io.Reader = readCloser
	if decompressFile {
		decompressedReader, err := getGzipReader(readCloser)
		if err != nil {
			return err
		}
		defer putGzipReader(decompressedReader)

		objectReader = decompressedReader
	}

	_, err = io.Copy(f, objectReader)
	if err != nil {
		return err
	}

	fStat, err := f.Stat()
	if err != nil {
		level.Error(logger).Log("msg", "failed to get stat for downloaded file", "err", err)
	}

	if err == nil {
		logger = log.With(logger, "size", humanize.Bytes(uint64(fStat.Size())))
	}
	level.Info(logger).Log("msg", "downloaded file", "total_time", time.Since(start))

	if sync {
		return f.Sync()
	}
	return nil
}

func IsCompressedFile(filename string) bool {
	return strings.HasSuffix(filename, ".gz")
}

func LoggerWithFilename(logger log.Logger, filename string) log.Logger {
	return log.With(logger, "file-name", filename)
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
