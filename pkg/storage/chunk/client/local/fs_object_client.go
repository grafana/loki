package local

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/runutil"

	"github.com/grafana/loki/pkg/ruler/rulestore/local"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// FSConfig is the config for a FSObjectClient.
type FSConfig struct {
	Directory                    string `yaml:"directory"`
	SizeBasedRetentionPercentage int    `yaml:"size_based_retention_percentage"`
}

// RegisterFlags registers flags.
func (cfg *FSConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlags registers flags with prefix.
func (cfg *FSConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, prefix+"local.chunk-directory", "", "Directory to store chunks in.")
	f.IntVar(&cfg.SizeBasedRetentionPercentage, prefix+"local.size-based-retention-percentage", 80, "Size based retention percentage")
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

// PutObject into the store
func (f *FSObjectClient) PutObject(_ context.Context, objectKey string, object io.ReadSeeker) error {
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

	// Should we perform a size based check here before copying the object to filesystem???
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
func (f *FSObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
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

//  ...
func (f *FSObjectClient) DeleteChunksBasedOnBlockSize(ctx context.Context) error {
	// Implement the function ;-)
	return nil
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

// Creating structure for DiskStatus
type DiskStatus struct {
	All         uint64  `json:"All"`
	Used        uint64  `json:"Used"`
	Free        uint64  `json:"Free"`
	UsedPercent float64 `json:UsedPercent`
}

// Function to get
// disk usage of path/disk
func DiskUsage(path string) (disk DiskStatus, err error) {
	fs := syscall.Statfs_t{}
	error := syscall.Statfs(path, &fs)
	if error != nil {
		return disk, error
	}
	disk.All = fs.Blocks * uint64(fs.Bsize)
	disk.Free = fs.Bfree * uint64(fs.Bsize)
	disk.Used = disk.All - disk.Free
	disk.UsedPercent = float64(disk.Used) / float64(disk.All) * float64(100)
	return disk, nil
}
