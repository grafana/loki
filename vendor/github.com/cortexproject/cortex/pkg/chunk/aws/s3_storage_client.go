package aws

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/util"
	awscommon "github.com/weaveworks/common/aws"
	"github.com/weaveworks/common/instrument"
)

var (
	s3RequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "s3_request_duration_seconds",
		Help:      "Time spent doing S3 requests.",
		Buckets:   []float64{.025, .05, .1, .25, .5, 1, 2},
	}, []string{"operation", "status_code"}))
)

func init() {
	s3RequestDuration.Register()
}

type s3ObjectClient struct {
	bucketName string
	S3         s3iface.S3API
}

// NewS3ObjectClient makes a new S3-backed ObjectClient.
func NewS3ObjectClient(cfg StorageConfig, schemaCfg chunk.SchemaConfig) (chunk.ObjectClient, error) {
	if cfg.S3.URL == nil {
		return nil, fmt.Errorf("no URL specified for S3")
	}
	s3Config, err := awscommon.ConfigFromURL(cfg.S3.URL)
	if err != nil {
		return nil, err
	}

	s3Config = s3Config.WithS3ForcePathStyle(cfg.S3ForcePathStyle) // support for Path Style S3 url if has the flag

	s3Config = s3Config.WithMaxRetries(0) // We do our own retries, so we can monitor them
	s3Client := s3.New(session.New(s3Config))
	bucketName := strings.TrimPrefix(cfg.S3.URL.Path, "/")
	client := s3ObjectClient{
		S3:         s3Client,
		bucketName: bucketName,
	}
	return client, nil
}

func (a s3ObjectClient) Stop() {
}

func (a s3ObjectClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	return util.GetParallelChunks(ctx, chunks, a.getChunk)
}

func (a s3ObjectClient) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, c chunk.Chunk) (chunk.Chunk, error) {
	var resp *s3.GetObjectOutput
	err := instrument.CollectedRequest(ctx, "S3.GetObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		resp, err = a.S3.GetObjectWithContext(ctx, &s3.GetObjectInput{
			Bucket: aws.String(a.bucketName),
			Key:    aws.String(c.ExternalKey()),
		})
		return err
	})
	if err != nil {
		return chunk.Chunk{}, err
	}
	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return chunk.Chunk{}, err
	}
	if err := c.Decode(decodeContext, buf); err != nil {
		return chunk.Chunk{}, err
	}
	return c, nil
}

func (a s3ObjectClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	var (
		s3ChunkKeys []string
		s3ChunkBufs [][]byte
	)

	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return err
		}
		key := chunks[i].ExternalKey()

		s3ChunkKeys = append(s3ChunkKeys, key)
		s3ChunkBufs = append(s3ChunkBufs, buf)
	}

	incomingErrors := make(chan error)
	for i := range s3ChunkBufs {
		go func(i int) {
			incomingErrors <- a.putS3Chunk(ctx, s3ChunkKeys[i], s3ChunkBufs[i])
		}(i)
	}

	var lastErr error
	for range s3ChunkKeys {
		err := <-incomingErrors
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (a s3ObjectClient) putS3Chunk(ctx context.Context, key string, buf []byte) error {
	return instrument.CollectedRequest(ctx, "S3.PutObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		_, err := a.S3.PutObjectWithContext(ctx, &s3.PutObjectInput{
			Body:   bytes.NewReader(buf),
			Bucket: aws.String(a.bucketName),
			Key:    aws.String(key),
		})
		return err
	})
}
