package huaweicloud

import (
	"context"
	"flag"
	"io"
	"net/http"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/instrument"
	"github.com/huaweicloud/huaweicloud-sdk-go-obs/obs"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	NoSuchKeyErr = "NoSuchKey"
	NotFoundErr  = "404"
)

var obsRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: constants.Loki,
	Name:      "obs_request_duration_seconds",
	Help:      "Time spent doing OBS requests.",
	Buckets:   prometheus.ExponentialBuckets(0.005, 4, 7),
}, []string{"operation", "status_code"}))

func init() {
	obsRequestDuration.Register()
}

type ObsObjectClient struct {
	client *obs.ObsClient
	bucket string
}

// ObsConfig is config for the OBS Chunk Client.
type ObsConfig struct {
	Bucket               string         `yaml:"bucket"`
	Endpoint             string         `yaml:"endpoint"`
	AccessKeyID          string         `yaml:"access_key_id"`
	SecretAccessKey      flagext.Secret `yaml:"secret_access_key"`
	ConnectionTimeoutSec int            `yaml:"conn_timeout_sec"`
	ReadWriteTimeoutSec  int            `yaml:"read_write_timeout_sec"`
}

// RegisterFlags registers flags.
func (cfg *ObsConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *ObsConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Bucket, prefix+"obs.bucketname", "", "Name of OBS bucket.")
	f.StringVar(&cfg.Endpoint, prefix+"obs.endpoint", "", "OBS endpoint to connect to.")
	f.StringVar(&cfg.AccessKeyID, prefix+"obs.access-key-id", "", "Huawei Cloud Access Key ID")
	f.Var(&cfg.SecretAccessKey, prefix+"obs.secret-access-key", "Huawei Cloud Secret Access Key")
	f.IntVar(&cfg.ConnectionTimeoutSec, prefix+"obs.conn-timeout-sec", 30, "Connection timeout in seconds")
	f.IntVar(&cfg.ReadWriteTimeoutSec, prefix+"obs.read-write-timeout-sec", 60, "Read/Write timeout in seconds")
}

func (cfg *ObsConfig) Validate() error {
	if cfg.ReadWriteTimeoutSec <= 0 {
		return errors.New("read write timeout must be greater than 0")
	}
	if cfg.ConnectionTimeoutSec <= 0 {
		return errors.New("connection timeout must be greater than 0")
	}
	return nil
}

// NewObsObjectClient makes a new chunk.Client that writes chunks to OBS.
func NewObsObjectClient(_ context.Context, cfg ObsConfig) (client.ObjectClient, error) {
	obsClient, err := obs.New(cfg.AccessKeyID, cfg.SecretAccessKey.String(), cfg.Endpoint,
		obs.WithConnectTimeout(cfg.ConnectionTimeoutSec),
		obs.WithSocketTimeout(cfg.ReadWriteTimeoutSec),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create OBS client")
	}

	return &ObsObjectClient{
		client: obsClient,
		bucket: cfg.Bucket,
	}, nil
}

func (s *ObsObjectClient) Stop() {
	if s.client != nil {
		s.client.Close()
	}
}

func (s *ObsObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := s.objectAttributes(ctx, objectKey, "OBS.ObjectExists"); err != nil {
		if s.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (s *ObsObjectClient) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	return s.objectAttributes(ctx, objectKey, "OBS.GetAttributes")
}

func (s *ObsObjectClient) objectAttributes(ctx context.Context, objectKey, operation string) (client.ObjectAttributes, error) {
	var objectSize int64
	err := instrument.CollectedRequest(ctx, operation, obsRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		input := &obs.GetObjectMetadataInput{
			Bucket: s.bucket,
			Key:    objectKey,
		}

		output, requestErr := s.client.GetObjectMetadata(input)
		if requestErr != nil {
			return requestErr
		}

		if output != nil {
			objectSize = output.ContentLength
		}
		return nil
	})
	if err != nil {
		return client.ObjectAttributes{}, err
	}

	return client.ObjectAttributes{Size: objectSize}, nil
}

// GetObject returns a reader and the size for the specified object key from the configured OBS bucket.
func (s *ObsObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var output *obs.GetObjectOutput
	err := instrument.CollectedRequest(ctx, "OBS.GetObject", obsRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		input := &obs.GetObjectInput{}
		input.Bucket = s.bucket
		input.Key = objectKey

		var requestErr error
		output, requestErr = s.client.GetObject(input)
		return requestErr
	})
	if err != nil {
		return nil, 0, err
	}

	size := output.ContentLength
	return output.Body, size, nil
}

// GetObjectRange returns a reader for the specified object key range from the configured OBS bucket.
func (s *ObsObjectClient) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	var output *obs.GetObjectOutput
	err := instrument.CollectedRequest(ctx, "OBS.GetObjectRange", obsRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		input := &obs.GetObjectInput{}
		input.Bucket = s.bucket
		input.Key = objectKey
		input.RangeStart = offset
		input.RangeEnd = offset + length - 1

		var requestErr error
		output, requestErr = s.client.GetObject(input)
		return requestErr
	})
	if err != nil {
		return nil, err
	}
	return output.Body, nil
}

// PutObject puts the specified bytes into the configured OBS bucket at the provided key
func (s *ObsObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return instrument.CollectedRequest(ctx, "OBS.PutObject", obsRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		input := &obs.PutObjectInput{}
		input.Bucket = s.bucket
		input.Key = objectKey
		input.Body = object

		_, err := s.client.PutObject(input)
		if err != nil {
			return errors.Wrap(err, "failed to put obs object")
		}
		return nil
	})
}

// List implements chunk.ObjectClient.
func (s *ObsObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix

	err := instrument.CollectedRequest(ctx, "OBS.List", obsRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		input := &obs.ListObjectsInput{}
		input.Bucket = s.bucket
		input.Prefix = prefix
		input.Delimiter = delimiter
		input.MaxKeys = 1000

		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			output, err := s.client.ListObjects(input)
			if err != nil {
				return errors.Wrap(err, "list huaweicloud obs bucket failed")
			}

			for _, content := range output.Contents {
				storageObjects = append(storageObjects, client.StorageObject{
					Key:        content.Key,
					ModifiedAt: content.LastModified,
				})
			}

			for _, commonPrefix := range output.CommonPrefixes {
				if commonPrefix != "" {
					commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(commonPrefix))
				}
			}

			if !output.IsTruncated {
				break
			}

			input.Marker = output.NextMarker
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	return storageObjects, commonPrefixes, nil
}

// DeleteObject deletes the specified object key from the configured OBS bucket.
func (s *ObsObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return instrument.CollectedRequest(ctx, "OBS.DeleteObject", obsRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		input := &obs.DeleteObjectInput{}
		input.Bucket = s.bucket
		input.Key = objectKey

		_, err := s.client.DeleteObject(input)
		if err != nil {
			return err
		}
		return nil
	})
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *ObsObjectClient) IsObjectNotFoundErr(err error) bool {
	if obsErr, ok := err.(obs.ObsError); ok {
		// OBS returns 404 status code for missing objects
		if obsErr.StatusCode == http.StatusNotFound {
			return true
		}
		// OBS returns "NoSuchKey" error code for missing objects
		if obsErr.Code == NoSuchKeyErr {
			return true
		}
		return false
	}
	return false
}

// TODO(dannyk): implement for client
func (s *ObsObjectClient) IsRetryableErr(error) bool { return false }
