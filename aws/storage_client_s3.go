package aws

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"strings"

	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/chunk"
	awscommon "github.com/weaveworks/common/aws"
	"github.com/weaveworks/common/instrument"
)

var (
	s3RequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "s3_request_duration_seconds",
		Help:      "Time spent doing S3 requests.",
		Buckets:   []float64{.025, .05, .1, .25, .5, 1, 2},
	}, []string{"operation", "status_code"})
)

func init() {
	prometheus.MustRegister(s3RequestDuration)
}

type s3storageClient struct {
	storageClient
	bucketName string
	S3         s3iface.S3API
}

// NewS3StorageClient makes a new AWS-backed StorageClient.
func NewS3StorageClient(cfg StorageConfig, schemaCfg chunk.SchemaConfig) (chunk.StorageClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}

	if cfg.S3.URL == nil {
		return nil, fmt.Errorf("no URL specified for S3")
	}
	s3Config, err := awscommon.ConfigFromURL(cfg.S3.URL)
	if err != nil {
		return nil, err
	}
	s3Config = s3Config.WithMaxRetries(0) // We do our own retries, so we can monitor them
	s3Client := s3.New(session.New(s3Config))
	bucketName := strings.TrimPrefix(cfg.S3.URL.Path, "/")

	client := s3storageClient{
		storageClient: storageClient{
			cfg:       cfg.DynamoDBConfig,
			schemaCfg: schemaCfg,
			DynamoDB:  dynamoDB,
		},
		S3:         s3Client,
		bucketName: bucketName,
	}
	client.queryRequestFn = client.queryRequest
	client.batchGetItemRequestFn = client.batchGetItemRequest
	client.batchWriteItemRequestFn = client.batchWriteItemRequest
	return client, nil
}

func (a s3storageClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	sp, ctx := ot.StartSpanFromContext(ctx, "GetChunks.S3")
	defer sp.Finish()
	sp.LogFields(otlog.Int("chunks requested", len(chunks)))

	incomingChunks := make(chan chunk.Chunk)
	incomingErrors := make(chan error)
	for _, c := range chunks {
		go func(c chunk.Chunk) {
			c, err := a.getS3Chunk(ctx, c)
			if err != nil {
				incomingErrors <- err
				return
			}
			incomingChunks <- c
		}(c)
	}

	result := []chunk.Chunk{}
	errors := []error{}
	for i := 0; i < len(chunks); i++ {
		select {
		case chunk := <-incomingChunks:
			result = append(result, chunk)
		case err := <-incomingErrors:
			errors = append(errors, err)
		}
	}

	sp.LogFields(otlog.Int("chunks fetched", len(result)))
	if len(errors) > 0 {
		sp.LogFields(otlog.String("error", errors[0].Error()))
		// Return any chunks we did receive: a partial result may be useful
		return result, errors[0]
	}
	return result, nil
}

func (a s3storageClient) getS3Chunk(ctx context.Context, c chunk.Chunk) (chunk.Chunk, error) {
	var resp *s3.GetObjectOutput
	err := instrument.TimeRequestHistogram(ctx, "S3.GetObject", s3RequestDuration, func(ctx context.Context) error {
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
	decodeContext := chunk.NewDecodeContext()
	if err := c.Decode(decodeContext, buf); err != nil {
		return chunk.Chunk{}, err
	}
	return c, nil
}

func (a s3storageClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	var (
		s3ChunkKeys []string
		s3ChunkBufs [][]byte
	)

	for i := range chunks {
		// Encode the chunk first - checksum is calculated as a side effect.
		buf, err := chunks[i].Encode()
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

func (a s3storageClient) putS3Chunk(ctx context.Context, key string, buf []byte) error {
	return instrument.TimeRequestHistogram(ctx, "S3.PutObject", s3RequestDuration, func(ctx context.Context) error {
		_, err := a.S3.PutObjectWithContext(ctx, &s3.PutObjectInput{
			Body:   bytes.NewReader(buf),
			Bucket: aws.String(a.bucketName),
			Key:    aws.String(key),
		})
		return err
	})
}
