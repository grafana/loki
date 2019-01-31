package local

import (
	"context"
	"encoding/base64"
	"flag"
	"io/ioutil"
	"path"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/util"
)

// FSConfig is the config for a fsObjectClient.
type FSConfig struct {
	Directory string `yaml:"directory"`
}

// RegisterFlags registers flags.
func (cfg *FSConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Directory, "local.chunk-directory", "", "Directory to store chunks in.")
}

type fsObjectClient struct {
	cfg FSConfig
}

// NewFSObjectClient makes a chunk.ObjectClient which stores chunks as files in the local filesystem.
func NewFSObjectClient(cfg FSConfig) (chunk.ObjectClient, error) {
	if err := ensureDirectory(cfg.Directory); err != nil {
		return nil, err
	}

	return &fsObjectClient{
		cfg: cfg,
	}, nil
}

func (fsObjectClient) Stop() {}

func (f *fsObjectClient) PutChunks(_ context.Context, chunks []chunk.Chunk) error {
	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return err
		}

		filename := base64.StdEncoding.EncodeToString([]byte(chunks[i].ExternalKey()))
		if err := ioutil.WriteFile(path.Join(f.cfg.Directory, filename), buf, 0644); err != nil {
			return err
		}
	}
	return nil
}

func (f *fsObjectClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	return util.GetParallelChunks(ctx, chunks, f.getChunk)
}

func (f *fsObjectClient) getChunk(_ context.Context, decodeContext *chunk.DecodeContext, c chunk.Chunk) (chunk.Chunk, error) {
	filename := base64.StdEncoding.EncodeToString([]byte(c.ExternalKey()))
	buf, err := ioutil.ReadFile(path.Join(f.cfg.Directory, filename))
	if err != nil {
		return c, err
	}

	if err := c.Decode(decodeContext, buf); err != nil {
		return c, err
	}

	return c, nil
}
