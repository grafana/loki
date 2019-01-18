package gcp

import (
	"context"
	"flag"
	"io/ioutil"

	"cloud.google.com/go/storage"
	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/util"
)

type gcsObjectClient struct {
	cfg       GCSConfig
	schemaCfg chunk.SchemaConfig
	client    *storage.Client
	bucket    *storage.BucketHandle
}

// GCSConfig is config for the GCS Chunk Client.
type GCSConfig struct {
	BucketName string `yaml:"bucket_name"`
}

// RegisterFlags registers flags.
func (cfg *GCSConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.BucketName, "gcs.bucketname", "", "Name of GCS bucket to put chunks in.")
}

// NewGCSObjectClient makes a new chunk.ObjectClient that writes chunks to GCS.
func NewGCSObjectClient(ctx context.Context, cfg GCSConfig, schemaCfg chunk.SchemaConfig) (chunk.ObjectClient, error) {
	client, err := storage.NewClient(ctx, instrumentation()...)
	if err != nil {
		return nil, err
	}
	return newGCSObjectClient(cfg, schemaCfg, client), nil
}

func newGCSObjectClient(cfg GCSConfig, schemaCfg chunk.SchemaConfig, client *storage.Client) chunk.ObjectClient {
	bucket := client.Bucket(cfg.BucketName)
	return &gcsObjectClient{
		cfg:       cfg,
		schemaCfg: schemaCfg,
		client:    client,
		bucket:    bucket,
	}
}

func (s *gcsObjectClient) Stop() {
	s.client.Close()
}

func (s *gcsObjectClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	for _, chunk := range chunks {
		buf, err := chunk.Encoded()
		if err != nil {
			return err
		}
		writer := s.bucket.Object(chunk.ExternalKey()).NewWriter(ctx)
		if _, err := writer.Write(buf); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
	}
	return nil
}

func (s *gcsObjectClient) GetChunks(ctx context.Context, input []chunk.Chunk) ([]chunk.Chunk, error) {
	return util.GetParallelChunks(ctx, input, s.getChunk)
}

func (s *gcsObjectClient) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, input chunk.Chunk) (chunk.Chunk, error) {
	reader, err := s.bucket.Object(input.ExternalKey()).NewReader(ctx)
	if err != nil {
		return chunk.Chunk{}, errors.WithStack(err)
	}
	defer reader.Close()

	buf, err := ioutil.ReadAll(reader)
	if err != nil {
		return chunk.Chunk{}, errors.WithStack(err)
	}

	if err := input.Decode(decodeContext, buf); err != nil {
		return chunk.Chunk{}, err
	}

	return input, nil
}
