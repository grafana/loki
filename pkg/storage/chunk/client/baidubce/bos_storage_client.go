package baidubce

import (
	"context"
	"flag"
	"io"
	"time"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/bos"
	"github.com/baidubce/bce-sdk-go/services/bos/api"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/instrument"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// NoSuchKeyErr The resource you requested does not exist.
// refer to: https://cloud.baidu.com/doc/BOS/s/Ajwvysfpl
//
//	https://intl.cloud.baidu.com/doc/BOS/s/Ajwvysfpl-en
const NoSuchKeyErr = "NoSuchKey"

const DefaultEndpoint = bos.DEFAULT_SERVICE_DOMAIN

var bosRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: constants.Loki,
	Name:      "bos_request_duration_seconds",
	Help:      "Time spent doing BOS requests.",
	Buckets:   prometheus.ExponentialBuckets(0.005, 4, 6),
}, []string{"operation", "status_code"}))

func init() {
	bosRequestDuration.Register()
}

type BOSStorageConfig struct {
	BucketName      string         `yaml:"bucket_name"`
	Endpoint        string         `yaml:"endpoint"`
	AccessKeyID     string         `yaml:"access_key_id"`
	SecretAccessKey flagext.Secret `yaml:"secret_access_key"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *BOSStorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *BOSStorageConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.BucketName, prefix+"bos.bucket-name", "", "Name of BOS bucket.")
	f.StringVar(&cfg.Endpoint, prefix+"bos.endpoint", DefaultEndpoint, "BOS endpoint to connect to.")
	f.StringVar(&cfg.AccessKeyID, prefix+"bos.access-key-id", "", "Baidu Cloud Engine (BCE) Access Key ID.")
	f.Var(&cfg.SecretAccessKey, prefix+"bos.secret-access-key", "Baidu Cloud Engine (BCE) Secret Access Key.")
}

type BOSObjectStorage struct {
	cfg    *BOSStorageConfig
	client *bos.Client
}

func NewBOSObjectStorage(cfg *BOSStorageConfig) (*BOSObjectStorage, error) {
	clientConfig := bos.BosClientConfiguration{
		Ak:               cfg.AccessKeyID,
		Sk:               cfg.SecretAccessKey.String(),
		Endpoint:         cfg.Endpoint,
		RedirectDisabled: false,
	}
	bosClient, err := bos.NewClientWithConfig(&clientConfig)
	if err != nil {
		return nil, err
	}
	return &BOSObjectStorage{
		cfg:    cfg,
		client: bosClient,
	}, nil
}

func (b *BOSObjectStorage) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return instrument.CollectedRequest(ctx, "BOS.PutObject", bosRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		body, err := bce.NewBodyFromSizedReader(object, -1)
		if err != nil {
			return err
		}
		_, err = b.client.BasicPutObject(b.cfg.BucketName, objectKey, body)
		return err
	})
}

func (b *BOSObjectStorage) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := b.objectAttributes(ctx, objectKey, "BOS.ObjectExists"); err != nil {
		if b.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (b *BOSObjectStorage) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	return b.objectAttributes(ctx, objectKey, "BOS.GetAttributes")
}

func (b *BOSObjectStorage) objectAttributes(ctx context.Context, objectKey, source string) (client.ObjectAttributes, error) {
	var objectSize int64
	err := instrument.CollectedRequest(ctx, source, bosRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		metaResult, requestErr := b.client.GetObjectMeta(b.cfg.BucketName, objectKey)
		if requestErr != nil {
			return requestErr
		}
		if metaResult != nil {
			objectSize = metaResult.ContentLength
		}
		return nil
	})
	if err != nil {
		return client.ObjectAttributes{}, err
	}

	return client.ObjectAttributes{Size: objectSize}, nil
}

func (b *BOSObjectStorage) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var res *api.GetObjectResult
	err := instrument.CollectedRequest(ctx, "BOS.GetObject", bosRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		var requestErr error
		res, requestErr = b.client.BasicGetObject(b.cfg.BucketName, objectKey)
		return requestErr
	})
	if err != nil {
		return nil, 0, errors.Wrapf(err, "failed to get BOS object [ %s ]", objectKey)
	}
	size := res.ContentLength
	return res.Body, size, nil
}

func (b *BOSObjectStorage) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	var res *api.GetObjectResult
	err := instrument.CollectedRequest(ctx, "BOS.GetObject", bosRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		var requestErr error
		res, requestErr = b.client.GetObject(b.cfg.BucketName, objectKey, nil, offset, offset+length-1)
		return requestErr
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get BOS object [ %s ]", objectKey)
	}
	return res.Body, nil
}

func (b *BOSObjectStorage) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix

	err := instrument.CollectedRequest(ctx, "BOS.List", bosRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		args := new(api.ListObjectsArgs)
		args.Prefix = prefix
		args.Delimiter = delimiter
		for {
			listObjectResult, err := b.client.ListObjects(b.cfg.BucketName, args)
			if err != nil {
				return err
			}
			for _, content := range listObjectResult.Contents {
				// LastModified format 2021-10-28T06:55:01Z
				lastModifiedTime, err := time.Parse(time.RFC3339, content.LastModified)
				if err != nil {
					return err
				}
				storageObjects = append(storageObjects, client.StorageObject{
					Key:        content.Key,
					ModifiedAt: lastModifiedTime,
				})
			}
			for _, commonPrefix := range listObjectResult.CommonPrefixes {
				commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(commonPrefix.Prefix))
			}
			if !listObjectResult.IsTruncated {
				break
			}
			args.Prefix = listObjectResult.Prefix
			args.Marker = listObjectResult.NextMarker
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return storageObjects, commonPrefixes, nil
}

func (b *BOSObjectStorage) DeleteObject(ctx context.Context, objectKey string) error {
	return instrument.CollectedRequest(ctx, "BOS.DeleteObject", bosRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
		err := b.client.DeleteObject(b.cfg.BucketName, objectKey)
		return err
	})
}

func (b *BOSObjectStorage) IsObjectNotFoundErr(err error) bool {
	switch realErr := errors.Cause(err).(type) {
	// Client exception indicates an exception encountered when the client attempts to send a request to the BOS and transmits data.
	case *bce.BceClientError:
		return false
	// When an exception occurs on the BOS server, the BOS server returns the corresponding error message to the user to locate the problem.
	// BceServiceError will return an error message string to contain the error code :
	// https://github.com/baidubce/bce-sdk-go/blob/1e5bfbecf07c6ed5d97a0090a9faee7d89466239/bce/error.go#L47-L53
	case *bce.BceServiceError:
		if realErr.Code == NoSuchKeyErr {
			return true
		}
	default:
		return false
	}
	return false
}

func (b *BOSObjectStorage) Stop() {}

// TODO(dannyk): implement for client
func (b *BOSObjectStorage) IsRetryableErr(error) bool { return false }
