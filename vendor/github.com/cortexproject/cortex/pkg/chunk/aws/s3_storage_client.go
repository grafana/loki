package aws

import (
	"context"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/prometheus/client_golang/prometheus"
	awscommon "github.com/weaveworks/common/aws"
	"github.com/weaveworks/common/instrument"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/flagext"
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

// S3Config specifies config for storing chunks on AWS S3.
type S3Config struct {
	S3               flagext.URLValue
	BucketNames      string
	S3ForcePathStyle bool
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *S3Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet with a specified prefix
func (cfg *S3Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.Var(&cfg.S3, prefix+"s3.url", "S3 endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<bucket-name> to use a mock in-memory implementation.")
	f.BoolVar(&cfg.S3ForcePathStyle, prefix+"s3.force-path-style", false, "Set this to `true` to force the request to use path-style addressing.")
	f.StringVar(&cfg.BucketNames, prefix+"s3.buckets", "", "Comma separated list of bucket names to evenly distribute chunks over. Overrides any buckets specified in s3.url flag")
}

type S3ObjectClient struct {
	bucketNames []string
	S3          s3iface.S3API
	delimiter   string
}

// NewS3ObjectClient makes a new S3-backed ObjectClient.
func NewS3ObjectClient(cfg S3Config, delimiter string) (*S3ObjectClient, error) {
	if cfg.S3.URL == nil {
		return nil, fmt.Errorf("no URL specified for S3")
	}
	s3Config, err := awscommon.ConfigFromURL(cfg.S3.URL)
	if err != nil {
		return nil, err
	}

	s3Config = s3Config.WithS3ForcePathStyle(cfg.S3ForcePathStyle) // support for Path Style S3 url if has the flag

	s3Config = s3Config.WithMaxRetries(0) // We do our own retries, so we can monitor them
	s3Config = s3Config.WithHTTPClient(&http.Client{Transport: defaultTransport})
	sess, err := session.NewSession(s3Config)
	if err != nil {
		return nil, err
	}
	s3Client := s3.New(sess)
	bucketNames := []string{strings.TrimPrefix(cfg.S3.URL.Path, "/")}
	if cfg.BucketNames != "" {
		bucketNames = strings.Split(cfg.BucketNames, ",") // comma separated list of bucket names
	}
	client := S3ObjectClient{
		S3:          s3Client,
		bucketNames: bucketNames,
		delimiter:   delimiter,
	}
	return &client, nil
}

// Stop fulfills the chunk.ObjectClient interface
func (a *S3ObjectClient) Stop() {}

// DeleteObject deletes the specified objectKey from the appropriate S3 bucket
func (a *S3ObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	_, err := a.S3.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(a.bucketFromKey(objectKey)),
		Key:    aws.String(objectKey),
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey {
				return chunk.ErrStorageObjectNotFound
			}
		}
		return err
	}

	return nil
}

// bucketFromKey maps a key to a bucket name
func (a *S3ObjectClient) bucketFromKey(key string) string {
	if len(a.bucketNames) == 0 {
		return ""
	}

	hasher := fnv.New32a()
	hasher.Write([]byte(key)) //nolint: errcheck
	hash := hasher.Sum32()

	return a.bucketNames[hash%uint32(len(a.bucketNames))]
}

// GetObject returns a reader for the specified object key from the configured S3 bucket. If the
// key does not exist a generic chunk.ErrStorageObjectNotFound error is returned.
func (a *S3ObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	var resp *s3.GetObjectOutput

	// Map the key into a bucket
	bucket := a.bucketFromKey(objectKey)

	err := instrument.CollectedRequest(ctx, "S3.GetObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		resp, err = a.S3.GetObjectWithContext(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(objectKey),
		})
		return err
	})

	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == s3.ErrCodeNoSuchKey {
				return nil, chunk.ErrStorageObjectNotFound
			}
		}
		return nil, err
	}

	return resp.Body, nil
}

// Put object into the store
func (a *S3ObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return instrument.CollectedRequest(ctx, "S3.PutObject", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		_, err := a.S3.PutObjectWithContext(ctx, &s3.PutObjectInput{
			Body:   object,
			Bucket: aws.String(a.bucketFromKey(objectKey)),
			Key:    aws.String(objectKey),
		})
		return err
	})
}

// List only objects from the store non-recursively
func (a *S3ObjectClient) List(ctx context.Context, prefix string) ([]chunk.StorageObject, error) {
	var storageObjects []chunk.StorageObject

	for i := range a.bucketNames {
		err := instrument.CollectedRequest(ctx, "S3.List", s3RequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			input := s3.ListObjectsV2Input{
				Bucket:    aws.String(a.bucketNames[i]),
				Prefix:    aws.String(prefix),
				Delimiter: aws.String(a.delimiter),
			}

			for {
				output, err := a.S3.ListObjectsV2WithContext(ctx, &input)
				if err != nil {
					return err
				}

				for _, content := range output.Contents {
					storageObjects = append(storageObjects, chunk.StorageObject{
						Key:        *content.Key,
						ModifiedAt: *content.LastModified,
					})
				}

				if !*output.IsTruncated {
					// No more results to fetch
					break
				}

				input.SetContinuationToken(*output.NextContinuationToken)
			}

			return nil
		})

		if err != nil {
			return nil, err
		}
	}

	return storageObjects, nil
}
