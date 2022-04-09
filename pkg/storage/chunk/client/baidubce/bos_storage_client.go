package baidubce

import (
	"context"
	"flag"

	"io"
	"time"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/bos"
	"github.com/baidubce/bce-sdk-go/services/bos/api"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"

	"github.com/grafana/loki/pkg/storage/chunk/client"
)

// ObjectNotFoundErr The specified key does not exist. ref: https://cloud.baidu.com/doc/BOS/s/Ajwvysfpl
const ObjectNotFoundErr = "ObjectNotFoundErr"

const DefaultEndpoint = "bj.bcebos.com"

var bosRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "loki",
	Name:      "bos_request_duration_seconds",
	Help:      "Time spent doing bos requests.",
	Buckets:   []float64{.025, .05, .1, .25, .5, 1, 2},
}, []string{"operation", "status_code"}))

func init() {
	bosRequestDuration.Register()
}

type BosStorageConfig struct {
	BucketName      string `yaml:"bucket_name"`
	Endpoint        string `yaml:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *BosStorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *BosStorageConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.BucketName, prefix+"baidubce.bucket-name", "", "Name of Bos bucket.")
	f.StringVar(&cfg.Endpoint, prefix+"baidubce.endpoint", DefaultEndpoint, "Bos endpoint to connect to.")
	f.StringVar(&cfg.AccessKeyID, prefix+"baidubce.access-key-id", "", "Baidu BCE Access Key ID")
	f.StringVar(&cfg.SecretAccessKey, prefix+"baidubce.secret-access-key", "", "Baidu BCE Secret Access Key")
}

type BosObjectStorage struct {
	cfg    *BosStorageConfig
	client *bos.Client
}

func (cfg *BosStorageConfig) Validate() error {
	return nil
}

func NewBosObjectStorage(cfg *BosStorageConfig) (*BosObjectStorage, error) {
	clientConfig := bos.BosClientConfiguration{
		Ak:               cfg.AccessKeyID,
		Sk:               cfg.SecretAccessKey,
		Endpoint:         cfg.Endpoint,
		RedirectDisabled: false,
	}
	bosClient, err := bos.NewClientWithConfig(&clientConfig)
	if err != nil {
		return nil, err
	}
	return &BosObjectStorage{
		cfg:    cfg,
		client: bosClient,
	}, nil
}

func (b *BosObjectStorage) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return instrument.CollectedRequest(ctx, "Bos.PutObject", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		body, err := bce.NewBodyFromSizedReader(object, -1)
		if err != nil {
			return err
		}
		_, err = b.client.BasicPutObject(b.cfg.BucketName, objectKey, body)
		return err
	})
}

func (b *BosObjectStorage) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var res *api.GetObjectResult
	err := instrument.CollectedRequest(ctx, "Bos.GetObject", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		var requestErr error
		res, requestErr = b.client.BasicGetObject(b.cfg.BucketName, objectKey)
		return requestErr
	})
	if err != nil {
		return nil, 0, errors.Wrap(err, "failed to get bos object")
	}
	var size int64
	if res.ContentLength != 0 {
		size = res.ContentLength
	}
	return res.Body, size, nil
}

func (b *BosObjectStorage) List(ctx context.Context, prefix string, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix

	err := instrument.CollectedRequest(ctx, "Bos.List", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
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
				LastModifiedTime, err := time.Parse(time.RFC3339, content.LastModified)
				if err != nil {
					return err
				}
				storageObjects = append(storageObjects, client.StorageObject{
					Key:        content.Key,
					ModifiedAt: LastModifiedTime,
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

func (b *BosObjectStorage) DeleteObject(ctx context.Context, objectKey string) error {
	return instrument.CollectedRequest(ctx, "Bos.DeleteObject", bosRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		err := b.client.DeleteObject(b.cfg.BucketName, objectKey)
		return err
	})
}

func (b *BosObjectStorage) IsObjectNotFoundErr(err error) bool {
	// ref: https://cloud.baidu.com/doc/BOS/s/rjwvyrwye
	switch realErr := err.(type) {
	case *bce.BceClientError:
		return false
	case *bce.BceServiceError:
		if realErr.Code == ObjectNotFoundErr {
			return true
		}
	default:
		return false
	}
	return false
}

func (b *BosObjectStorage) Stop() {}
