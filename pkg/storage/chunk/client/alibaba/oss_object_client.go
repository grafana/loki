package alibaba

import (
	"context"
	"flag"
	"io"
	"net/http"
	"strconv"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/grafana/dskit/instrument"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const NoSuchKeyErr = "NoSuchKey"

var ossRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: constants.Loki,
	Name:      "oss_request_duration_seconds",
	Help:      "Time spent doing OSS requests.",
	Buckets:   prometheus.ExponentialBuckets(0.005, 4, 7),
}, []string{"operation", "status_code"}))

func init() {
	ossRequestDuration.Register()
}

type OssObjectClient struct {
	defaultBucket *oss.Bucket
}

// OssConfig is config for the OSS Chunk Client.
type OssConfig struct {
	Bucket               string `yaml:"bucket"`
	Endpoint             string `yaml:"endpoint"`
	AccessKeyID          string `yaml:"access_key_id"`
	SecretAccessKey      string `yaml:"secret_access_key"`
	ConnectionTimeoutSec int64  `yaml:"conn_timeout_sec"`
	ReadWriteTimeoutSec  int64  `yaml:"read_write_timeout_sec"`
}

// RegisterFlags registers flags.
func (cfg *OssConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *OssConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Bucket, prefix+"oss.bucketname", "", "Name of OSS bucket.")
	f.StringVar(&cfg.Endpoint, prefix+"oss.endpoint", "", "oss Endpoint to connect to.")
	f.StringVar(&cfg.AccessKeyID, prefix+"oss.access-key-id", "", "alibabacloud Access Key ID")
	f.StringVar(&cfg.SecretAccessKey, prefix+"oss.secret-access-key", "", "alibabacloud Secret Access Key")
	f.Int64Var(&cfg.ConnectionTimeoutSec, prefix+"oss.conn-timeout-sec", 30, "Connection timeout in seconds")
	f.Int64Var(&cfg.ReadWriteTimeoutSec, prefix+"oss.read-write-timeout-sec", 60, "Read/Write timeout in seconds")
}

func (cfg *OssConfig) Validate() error {
	if cfg.ReadWriteTimeoutSec <= 0 {
		return errors.New("read write timeout must be greater than 0")
	}
	return nil
}

// NewOssObjectClient makes a new chunk.Client that writes chunks to OSS.
func NewOssObjectClient(_ context.Context, cfg OssConfig) (client.ObjectClient, error) {
	client, err := oss.New(cfg.Endpoint, cfg.AccessKeyID, cfg.SecretAccessKey, oss.Timeout(cfg.ConnectionTimeoutSec, cfg.ReadWriteTimeoutSec))
	if err != nil {
		return nil, err
	}
	bucket, err := client.Bucket(cfg.Bucket)
	if err != nil {
		return nil, err
	}
	return &OssObjectClient{
		defaultBucket: bucket,
	}, nil
}

func (s *OssObjectClient) Stop() {
}

func (s *OssObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := s.objectAttributes(ctx, objectKey, "OSS.ObjectExists"); err != nil {
		if s.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (s *OssObjectClient) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	return s.objectAttributes(ctx, objectKey, "OSS.GetAttributes")
}

func (s *OssObjectClient) objectAttributes(ctx context.Context, objectKey, operation string) (client.ObjectAttributes, error) {
	var options []oss.Option
	var objectSize int64
	err := instrument.CollectedRequest(ctx, operation, ossRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		headers, requestErr := s.defaultBucket.GetObjectMeta(objectKey, options...)
		if requestErr != nil {
			return requestErr
		}

		objectSize, _ = strconv.ParseInt(headers.Get(oss.HTTPHeaderContentLength), 10, 64)
		return nil
	})
	if err != nil {
		return client.ObjectAttributes{}, err
	}

	return client.ObjectAttributes{Size: objectSize}, nil
}

// GetObject returns a reader and the size for the specified object key from the configured OSS bucket.
func (s *OssObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var resp *oss.GetObjectResult
	var options []oss.Option
	err := instrument.CollectedRequest(ctx, "OSS.GetObject", ossRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		var requestErr error
		resp, requestErr = s.defaultBucket.DoGetObject(&oss.GetObjectRequest{ObjectKey: objectKey}, options)
		if requestErr != nil {
			return requestErr
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	length := resp.Response.Headers.Get("Content-Length")
	size, err := strconv.Atoi(length)
	if err != nil {
		return nil, 0, err
	}
	return resp.Response.Body, int64(size), err
}

// GetObject returns a reader and the size for the specified object key from the configured OSS bucket.
func (s *OssObjectClient) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	var resp *oss.GetObjectResult
	options := []oss.Option{
		oss.Range(offset, offset+length-1),
	}
	err := instrument.CollectedRequest(ctx, "OSS.GetObject", ossRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		var requestErr error
		resp, requestErr = s.defaultBucket.DoGetObject(&oss.GetObjectRequest{ObjectKey: objectKey}, options)
		if requestErr != nil {
			return requestErr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp.Response.Body, nil
}

// PutObject puts the specified bytes into the configured OSS bucket at the provided key
func (s *OssObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return instrument.CollectedRequest(ctx, "OSS.PutObject", ossRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		if err := s.defaultBucket.PutObject(objectKey, object); err != nil {
			return errors.Wrap(err, "failed to put oss object")
		}
		return nil
	})
}

// List implements chunk.ObjectClient.
func (s *OssObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	marker := oss.Marker("")
	for {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		objects, err := s.defaultBucket.ListObjects(oss.Prefix(prefix), oss.Delimiter(delimiter), marker)
		if err != nil {
			return nil, nil, errors.Wrap(err, "list alibaba oss bucket failed")
		}
		marker = oss.Marker(objects.NextMarker)
		for _, object := range objects.Objects {
			storageObjects = append(storageObjects, client.StorageObject{
				Key:        object.Key,
				ModifiedAt: object.LastModified,
			})
		}
		for _, object := range objects.CommonPrefixes {
			if object != "" {
				commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(object))
			}
		}
		if !objects.IsTruncated {
			break
		}
	}
	return storageObjects, commonPrefixes, nil
}

// DeleteObject deletes the specified object key from the configured OSS bucket.
func (s *OssObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return instrument.CollectedRequest(ctx, "OSS.DeleteObject", ossRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		err := s.defaultBucket.DeleteObject(objectKey)
		if err != nil {
			return err
		}
		return nil
	})
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *OssObjectClient) IsObjectNotFoundErr(err error) bool {
	switch caseErr := err.(type) {
	case oss.ServiceError:
		if caseErr.Code == NoSuchKeyErr && caseErr.StatusCode == http.StatusNotFound {
			return true
		}
		return false
	default:
		return false
	}
}

// TODO(dannyk): implement for client
func (s *OssObjectClient) IsRetryableErr(error) bool { return false }
