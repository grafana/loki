package local

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/runutil"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/ruler/rulestore/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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

func (cfg *FSConfig) ToCortexLocalConfig() local.Config {
	return local.Config{
		Directory: cfg.Directory,
	}
}

// FSObjectClient holds config for filesystem as object store
type FSObjectClient struct {
	cfg           FSConfig
	pathSeparator string
}

var _ client.ObjectClient = (*FSObjectClient)(nil)

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

func (f *FSObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := f.GetAttributes(ctx, objectKey); err != nil {
		if f.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (f *FSObjectClient) GetAttributes(_ context.Context, objectKey string) (client.ObjectAttributes, error) {
	fullPath := filepath.Join(f.cfg.Directory, filepath.FromSlash(objectKey))
	fi, err := os.Lstat(fullPath)
	if err != nil {
		return client.ObjectAttributes{}, err
	}

	return client.ObjectAttributes{Size: fi.Size()}, nil
}

// GetObject from the store
func (f *FSObjectClient) GetObject(_ context.Context, objectKey string) (io.ReadCloser, int64, error) {
	fl, err := os.Open(filepath.Join(f.cfg.Directory, filepath.FromSlash(objectKey)))
	if err != nil {
		return nil, 0, err
	}
	stats, err := fl.Stat()
	if err != nil {
		return nil, 0, err
	}
	return fl, stats.Size(), nil
}

type SectionReadCloser struct {
	io.Reader
	closeFn func() error
}

func (l SectionReadCloser) Close() error {
	return l.closeFn()
}

// GetObject from the store
func (f *FSObjectClient) GetObjectRange(_ context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	fl, err := os.Open(filepath.Join(f.cfg.Directory, filepath.FromSlash(objectKey)))
	if err != nil {
		return nil, err
	}
	closer := SectionReadCloser{
		Reader:  io.NewSectionReader(fl, offset, length),
		closeFn: fl.Close,
	}
	return closer, nil
}

// PutObject into the store
func (f *FSObjectClient) PutObject(_ context.Context, objectKey string, object io.Reader) error {
	fullPath := filepath.Join(f.cfg.Directory, filepath.FromSlash(objectKey))
	err := util.EnsureDirectory(filepath.Dir(fullPath))
	if err != nil {
		return err
	}

	fl, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
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
func (f *FSObjectClient) List(_ context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
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
		return []client.StorageObject{{Key: info.Name(), ModifiedAt: info.ModTime()}}, nil, nil
	}

	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix

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
				commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(relPath+delimiter))
			}
			return filepath.SkipDir
		}

		storageObjects = append(storageObjects, client.StorageObject{Key: relPath, ModifiedAt: info.ModTime()})
		return nil
	})

	return storageObjects, commonPrefixes, err
}

func (f *FSObjectClient) DeleteObject(_ context.Context, objectKey string) error {
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
func (f *FSObjectClient) DeleteChunksBefore(_ context.Context, ts time.Time) error {
	return filepath.Walk(f.cfg.Directory, func(path string, info os.FileInfo, _ error) error {
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

// TODO(dannyk): implement for client
func (f *FSObjectClient) IsRetryableErr(error) bool { return false }

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
