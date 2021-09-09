package local

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/runutil"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// FSConfig is the config for a FSObjectClient.
type FSConfig struct {
	Directory string `yaml:"directory"`
}

// RegisterFlags registers flags.
func (cfg *FSConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlags registers flags with prefix.
func (cfg *FSConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, prefix+"local.chunk-directory", "", "Directory to store chunks in.")
}

// FSObjectClient holds config for filesystem as object store
type FSObjectClient struct {
	cfg           FSConfig
	pathSeparator string
}

// NewFSObjectClient makes a chunk.Client which stores chunks as files in the local filesystem.
func NewFSObjectClient(cfg FSConfig) (*FSObjectClient, error) {
	// filepath.Clean cleans up the path by removing unwanted duplicate slashes, dots etc.
	// This is needed because DeleteObject works on paths which are already cleaned up and it
	// checks whether it is about to delete the configured directory when it becomes empty
	cfg.Directory = filepath.Clean(cfg.Directory)
	if err := util.EnsureDirectory(cfg.Directory); err != nil {
		return nil, err
	}

	return &FSObjectClient{
		cfg:           cfg,
		pathSeparator: string(os.PathSeparator),
	}, nil
}

// Stop implements ObjectClient
func (FSObjectClient) Stop() {}

// GetObject from the store
func (f *FSObjectClient) GetObject(_ context.Context, objectKey string) (io.ReadCloser, error) {
	fl, err := os.Open(filepath.Join(f.cfg.Directory, filepath.FromSlash(objectKey)))
	if err != nil {
		return nil, err
	}

	return fl, nil
}

// PutObject into the store
func (f *FSObjectClient) PutObject(_ context.Context, objectKey string, object io.ReadSeeker) error {
	fullPath := filepath.Join(f.cfg.Directory, filepath.FromSlash(objectKey))
	err := util.EnsureDirectory(filepath.Dir(fullPath))
	if err != nil {
		return err
	}

	fl, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	defer runutil.CloseWithLogOnErr(util_log.Logger, fl, "fullPath: %s", fullPath)

	_, err = io.Copy(fl, object)
	if err != nil {
		return err
	}

	err = fl.Sync()
	if err != nil {
		return err
	}

	return fl.Close()
}

// List implements chunk.ObjectClient.
// FSObjectClient assumes that prefix is a directory, and only supports "" and "/" delimiters.
func (f *FSObjectClient) List(ctx context.Context, prefix, delimiter string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	if delimiter != "" && delimiter != "/" {
		return nil, nil, fmt.Errorf("unsupported delimiter: %q", delimiter)
	}

	folderPath := filepath.Join(f.cfg.Directory, filepath.FromSlash(prefix))

	info, err := os.Stat(folderPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if !info.IsDir() {
		// When listing single file, return this file only.
		return []chunk.StorageObject{{Key: info.Name(), ModifiedAt: info.ModTime()}}, nil, nil
	}

	var storageObjects []chunk.StorageObject
	var commonPrefixes []chunk.StorageCommonPrefix

	err = filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Ignore starting folder itself.
		if path == folderPath {
			return nil
		}

		relPath, err := filepath.Rel(f.cfg.Directory, path)
		if err != nil {
			return err
		}

		relPath = filepath.ToSlash(relPath)

		if info.IsDir() {
			if delimiter == "" {
				// Go into directory
				return nil
			}

			empty, err := isDirEmpty(path)
			if err != nil {
				return err
			}

			if !empty {
				commonPrefixes = append(commonPrefixes, chunk.StorageCommonPrefix(relPath+delimiter))
			}
			return filepath.SkipDir
		}

		storageObjects = append(storageObjects, chunk.StorageObject{Key: relPath, ModifiedAt: info.ModTime()})
		return nil
	})

	return storageObjects, commonPrefixes, err
}

func (f *FSObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	// inspired from https://github.com/thanos-io/thanos/blob/55cb8ca38b3539381dc6a781e637df15c694e50a/pkg/objstore/filesystem/filesystem.go#L195
	file := filepath.Join(f.cfg.Directory, filepath.FromSlash(objectKey))

	for file != f.cfg.Directory {
		if err := os.Remove(file); err != nil {
			return err
		}

		file = filepath.Dir(file)
		empty, err := isDirEmpty(file)
		if err != nil {
			return err
		}

		if !empty {
			break
		}
	}

	return nil
}

// DeleteChunksBefore implements BucketClient
func (f *FSObjectClient) DeleteChunksBefore(ctx context.Context, ts time.Time) error {
	return filepath.Walk(f.cfg.Directory, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && info.ModTime().Before(ts) {
			level.Info(util_log.Logger).Log("msg", "file has exceeded the retention period, removing it", "filepath", info.Name())
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		return nil
	})
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (f *FSObjectClient) IsObjectNotFoundErr(err error) bool {
	return os.IsNotExist(errors.Cause(err))
}

// copied from https://github.com/thanos-io/thanos/blob/55cb8ca38b3539381dc6a781e637df15c694e50a/pkg/objstore/filesystem/filesystem.go#L181
func isDirEmpty(name string) (ok bool, err error) {
	f, err := os.Open(name)
	if err != nil {
		return false, err
	}
	defer runutil.CloseWithErrCapture(&err, f, "dir open")

	if _, err = f.Readdir(1); err == io.EOF {
		return true, nil
	}
	return false, err
}
