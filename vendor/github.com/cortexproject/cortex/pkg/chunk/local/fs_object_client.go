package local

import (
	"bytes"
	"context"
	"encoding/base64"
	"flag"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/go-kit/kit/log/level"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/util"
	pkgUtil "github.com/cortexproject/cortex/pkg/util"
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
	cfg FSConfig
}

// NewFSObjectClient makes a chunk.ObjectClient which stores chunks as files in the local filesystem.
func NewFSObjectClient(cfg FSConfig) (*FSObjectClient, error) {
	if err := util.EnsureDirectory(cfg.Directory); err != nil {
		return nil, err
	}

	return &FSObjectClient{
		cfg: cfg,
	}, nil
}

// Stop implements ObjectClient
func (FSObjectClient) Stop() {}

// PutChunks implements ObjectClient
func (f *FSObjectClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return err
		}

		filename := base64.StdEncoding.EncodeToString([]byte(chunks[i].ExternalKey()))
		if err := f.PutObject(ctx, filename, bytes.NewReader(buf)); err != nil {
			return err
		}
	}
	return nil
}

// GetChunks implements ObjectClient
func (f *FSObjectClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	return util.GetParallelChunks(ctx, chunks, f.getChunk)
}

func (f *FSObjectClient) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, c chunk.Chunk) (chunk.Chunk, error) {
	filename := base64.StdEncoding.EncodeToString([]byte(c.ExternalKey()))

	readCloser, err := f.GetObject(ctx, filename)
	if err != nil {
		return chunk.Chunk{}, err
	}

	defer readCloser.Close()

	buf, err := ioutil.ReadAll(readCloser)
	if err != nil {
		return chunk.Chunk{}, err
	}

	if err := c.Decode(decodeContext, buf); err != nil {
		return c, err
	}

	return c, nil
}

// Get object from the store
func (f *FSObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	return os.Open(path.Join(f.cfg.Directory, objectKey))
}

// Put object into the store
func (f *FSObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	fullPath := path.Join(f.cfg.Directory, objectKey)
	err := util.EnsureDirectory(path.Dir(fullPath))
	if err != nil {
		return err
	}

	fl, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	defer fl.Close()

	_, err = io.Copy(fl, object)
	return err
}

// List only objects from the store non-recursively
func (f *FSObjectClient) List(ctx context.Context, prefix string) ([]chunk.StorageObject, error) {
	var storageObjects []chunk.StorageObject
	folderPath := filepath.Join(f.cfg.Directory, prefix)

	_, err := os.Stat(folderPath)
	if err != nil {
		if os.IsNotExist(err) {
			return storageObjects, nil
		}
		return nil, err
	}

	filesInfo, err := ioutil.ReadDir(folderPath)
	if err != nil {
		return nil, err
	}

	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			continue
		}
		storageObjects = append(storageObjects, chunk.StorageObject{
			Key:        filepath.Join(prefix, fileInfo.Name()),
			ModifiedAt: fileInfo.ModTime(),
		})
	}

	return storageObjects, nil
}

// DeleteChunksBefore implements BucketClient
func (f *FSObjectClient) DeleteChunksBefore(ctx context.Context, ts time.Time) error {
	return filepath.Walk(f.cfg.Directory, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() && info.ModTime().Before(ts) {
			level.Info(pkgUtil.Logger).Log("msg", "file has exceeded the retention period, removing it", "filepath", info.Name())
			if err := os.Remove(path); err != nil {
				return err
			}
		}
		return nil
	})
}
