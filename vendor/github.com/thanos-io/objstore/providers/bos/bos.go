// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package bos

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/services/bos"
	"github.com/baidubce/bce-sdk-go/services/bos/api"
	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/objstore"
)

// partSize 128MB.
const partSize = 1024 * 1024 * 128

// Bucket implements the store.Bucket interface against bos-compatible(Baidu Object Storage) APIs.
type Bucket struct {
	logger log.Logger
	client *bos.Client
	name   string
}

// Config encapsulates the necessary config values to instantiate an bos client.
type Config struct {
	Bucket    string `yaml:"bucket"`
	Endpoint  string `yaml:"endpoint"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
}

func (conf *Config) validate() error {
	if conf.Bucket == "" ||
		conf.Endpoint == "" ||
		conf.AccessKey == "" ||
		conf.SecretKey == "" {
		return errors.New("insufficient BOS configuration information")
	}

	return nil
}

// parseConfig unmarshal a buffer into a Config with default HTTPConfig values.
func parseConfig(conf []byte) (Config, error) {
	config := Config{}
	if err := yaml.Unmarshal(conf, &config); err != nil {
		return Config{}, err
	}

	return config, nil
}

// NewBucket new bos bucket.
func NewBucket(logger log.Logger, conf []byte, component string) (*Bucket, error) {
	// TODO(https://github.com/thanos-io/objstore/pull/150): Add support for roundtripper wrapper.
	if logger == nil {
		logger = log.NewNopLogger()
	}

	config, err := parseConfig(conf)
	if err != nil {
		return nil, errors.Wrap(err, "parsing BOS configuration")
	}

	return NewBucketWithConfig(logger, config, component)
}

// NewBucketWithConfig returns a new Bucket using the provided bos config struct.
func NewBucketWithConfig(logger log.Logger, config Config, component string) (*Bucket, error) {
	if err := config.validate(); err != nil {
		return nil, errors.Wrap(err, "validating BOS configuration")
	}

	client, err := bos.NewClient(config.AccessKey, config.SecretKey, config.Endpoint)
	if err != nil {
		return nil, errors.Wrap(err, "creating BOS client")
	}

	client.Config.UserAgent = fmt.Sprintf("thanos-%s", component)

	bkt := &Bucket{
		logger: logger,
		client: client,
		name:   config.Bucket,
	}
	return bkt, nil
}

func (b *Bucket) Provider() objstore.ObjProvider { return objstore.BOS }

// Name returns the bucket name for the provider.
func (b *Bucket) Name() string {
	return b.name
}

// Delete removes the object with the given name.
func (b *Bucket) Delete(_ context.Context, name string) error {
	return b.client.DeleteObject(b.name, name)
}

// Upload the contents of the reader as an object into the bucket.
func (b *Bucket) Upload(_ context.Context, name string, r io.Reader) error {
	size, err := objstore.TryToGetSize(r)
	if err != nil {
		return errors.Wrapf(err, "getting size of %s", name)
	}

	partNums, lastSlice := int(math.Floor(float64(size)/partSize)), size%partSize
	if partNums == 0 {
		body, err := bce.NewBodyFromSizedReader(r, lastSlice)
		if err != nil {
			return errors.Wrapf(err, "failed to create SizedReader for %s", name)
		}

		if _, err := b.client.PutObject(b.name, name, body, nil); err != nil {
			return errors.Wrapf(err, "failed to upload %s", name)
		}

		return nil
	}

	result, err := b.client.BasicInitiateMultipartUpload(b.name, name)
	if err != nil {
		return errors.Wrapf(err, "failed to initiate MultipartUpload for %s", name)
	}

	uploadEveryPart := func(partSize int64, part int, uploadId string) (string, error) {
		body, err := bce.NewBodyFromSizedReader(r, partSize)
		if err != nil {
			return "", err
		}

		etag, err := b.client.UploadPart(b.name, name, uploadId, part, body, nil)
		if err != nil {
			if err := b.client.AbortMultipartUpload(b.name, name, uploadId); err != nil {
				return etag, err
			}
			return etag, err
		}
		return etag, nil
	}

	var parts []api.UploadInfoType

	for part := 1; part <= partNums; part++ {
		etag, err := uploadEveryPart(partSize, part, result.UploadId)
		if err != nil {
			return errors.Wrapf(err, "failed to upload part %d for %s", part, name)
		}
		parts = append(parts, api.UploadInfoType{PartNumber: part, ETag: etag})
	}

	if lastSlice != 0 {
		etag, err := uploadEveryPart(lastSlice, partNums+1, result.UploadId)
		if err != nil {
			return errors.Wrapf(err, "failed to upload the last part for %s", name)
		}
		parts = append(parts, api.UploadInfoType{PartNumber: partNums + 1, ETag: etag})
	}

	if _, err := b.client.CompleteMultipartUploadFromStruct(b.name, name, result.UploadId, &api.CompleteMultipartUploadArgs{Parts: parts}); err != nil {
		return errors.Wrapf(err, "failed to set %s upload completed", name)
	}
	return nil
}

func (b *Bucket) SupportedIterOptions() []objstore.IterOptionType {
	return []objstore.IterOptionType{objstore.Recursive, objstore.UpdatedAt}
}

func (b *Bucket) IterWithAttributes(ctx context.Context, dir string, f func(attrs objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	if err := objstore.ValidateIterOptions(b.SupportedIterOptions(), options...); err != nil {
		return err
	}

	if dir != "" {
		dir = strings.TrimSuffix(dir, objstore.DirDelim) + objstore.DirDelim
	}

	delimiter := objstore.DirDelim

	params := objstore.ApplyIterOptions(options...)
	if params.Recursive {
		delimiter = ""
	}

	var marker string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		objects, err := b.client.ListObjects(b.name, &api.ListObjectsArgs{
			Delimiter: delimiter,
			Marker:    marker,
			MaxKeys:   1000,
			Prefix:    dir,
		})
		if err != nil {
			return err
		}

		marker = objects.NextMarker
		for _, object := range objects.Contents {
			attrs := objstore.IterObjectAttributes{
				Name: object.Key,
			}

			if params.LastModified && object.LastModified != "" {
				lastModified, err := time.Parse(time.RFC1123, object.LastModified)
				if err != nil {
					return fmt.Errorf("iter: get last modified: %w", err)
				}
				attrs.SetLastModified(lastModified)
			}

			if err := f(attrs); err != nil {
				return err
			}
		}

		for _, object := range objects.CommonPrefixes {
			if err := f(objstore.IterObjectAttributes{Name: object.Prefix}); err != nil {
				return err
			}
		}
		if !objects.IsTruncated {
			break
		}
	}
	return nil
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (b *Bucket) Iter(ctx context.Context, dir string, f func(string) error, opts ...objstore.IterOption) error {
	// Only include recursive option since attributes are not used in this method.
	var filteredOpts []objstore.IterOption
	for _, opt := range opts {
		if opt.Type == objstore.Recursive {
			filteredOpts = append(filteredOpts, opt)
			break
		}
	}

	return b.IterWithAttributes(ctx, dir, func(attrs objstore.IterObjectAttributes) error {
		return f(attrs.Name)
	}, filteredOpts...)
}

// Get returns a reader for the given object name.
func (b *Bucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.getRange(ctx, b.name, name, 0, -1)
}

// GetRange returns a new range reader for the given object name and range.
func (b *Bucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return b.getRange(ctx, b.name, name, off, length)
}

// Exists checks if the given object exists in the bucket.
func (b *Bucket) Exists(_ context.Context, name string) (bool, error) {
	_, err := b.client.GetObjectMeta(b.name, name)
	if err != nil {
		if b.IsObjNotFoundErr(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "getting object metadata of %s", name)
	}
	return true, nil
}

func (b *Bucket) Close() error {
	return nil
}

// ObjectSize returns the size of the specified object.
func (b *Bucket) ObjectSize(_ context.Context, name string) (uint64, error) {
	objMeta, err := b.client.GetObjectMeta(b.name, name)
	if err != nil {
		return 0, err
	}
	return uint64(objMeta.ContentLength), nil
}

// Attributes returns information about the specified object.
func (b *Bucket) Attributes(_ context.Context, name string) (objstore.ObjectAttributes, error) {
	objMeta, err := b.client.GetObjectMeta(b.name, name)
	if err != nil {
		return objstore.ObjectAttributes{}, errors.Wrapf(err, "gettting objectmeta of %s", name)
	}

	lastModified, err := time.Parse(time.RFC1123, objMeta.LastModified)
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	return objstore.ObjectAttributes{
		Size:         objMeta.ContentLength,
		LastModified: lastModified,
	}, nil
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *Bucket) IsObjNotFoundErr(err error) bool {
	switch bosErr := errors.Cause(err).(type) {
	case *bce.BceServiceError:
		if bosErr.StatusCode == http.StatusNotFound || bosErr.Code == "NoSuchKey" {
			return true
		}
	}
	return false
}

// IsAccessDeniedErr returns true if access to object is denied.
func (b *Bucket) IsAccessDeniedErr(_ error) bool {
	return false
}

func (b *Bucket) getRange(_ context.Context, bucketName, objectKey string, off, length int64) (io.ReadCloser, error) {
	if len(objectKey) == 0 {
		return nil, errors.Errorf("given object name should not empty")
	}

	ranges := []int64{off}
	if length != -1 {
		ranges = append(ranges, off+length-1)
	}

	obj, err := b.client.GetObject(bucketName, objectKey, map[string]string{}, ranges...)
	if err != nil {
		return nil, err
	}

	return objstore.ObjectSizerReadCloser{
		ReadCloser: obj.Body,
		Size: func() (int64, error) {
			return obj.ContentLength, nil
		},
	}, err
}

func configFromEnv() Config {
	c := Config{
		Bucket:    os.Getenv("BOS_BUCKET"),
		Endpoint:  os.Getenv("BOS_ENDPOINT"),
		AccessKey: os.Getenv("BOS_ACCESS_KEY"),
		SecretKey: os.Getenv("BOS_SECRET_KEY"),
	}
	return c
}

// NewTestBucket creates test bkt client that before returning creates temporary bucket.
// In a close function it empties and deletes the bucket.
func NewTestBucket(t testing.TB) (objstore.Bucket, func(), error) {
	c := configFromEnv()
	if err := validateForTest(c); err != nil {
		return nil, nil, err
	}

	if c.Bucket != "" {
		if os.Getenv("THANOS_ALLOW_EXISTING_BUCKET_USE") == "" {
			return nil, nil, errors.New("BOS_BUCKET is defined. Normally this tests will create temporary bucket " +
				"and delete it after test. Unset BOS_BUCKET env variable to use default logic. If you really want to run " +
				"tests against provided (NOT USED!) bucket, set THANOS_ALLOW_EXISTING_BUCKET_USE=true. WARNING: That bucket " +
				"needs to be manually cleared. This means that it is only useful to run one test in a time. This is due " +
				"to safety (accidentally pointing prod bucket for test) as well as BOS not being fully strong consistent.")
		}

		bc, err := yaml.Marshal(c)
		if err != nil {
			return nil, nil, err
		}

		b, err := NewBucket(log.NewNopLogger(), bc, "thanos-e2e-test")
		if err != nil {
			return nil, nil, err
		}

		if err := b.Iter(context.Background(), "", func(f string) error {
			return errors.Errorf("bucket %s is not empty", c.Bucket)
		}); err != nil {
			return nil, nil, errors.Wrapf(err, "checking bucket %s", c.Bucket)
		}

		t.Log("WARNING. Reusing", c.Bucket, "BOS bucket for BOS tests. Manual cleanup afterwards is required")
		return b, func() {}, nil
	}

	src := rand.NewSource(time.Now().UnixNano())
	tmpBucketName := strings.Replace(fmt.Sprintf("test_%x", src.Int63()), "_", "-", -1)

	if len(tmpBucketName) >= 31 {
		tmpBucketName = tmpBucketName[:31]
	}

	c.Bucket = tmpBucketName
	bc, err := yaml.Marshal(c)
	if err != nil {
		return nil, nil, err
	}

	b, err := NewBucket(log.NewNopLogger(), bc, "thanos-e2e-test")
	if err != nil {
		return nil, nil, err
	}

	if _, err := b.client.PutBucket(b.name); err != nil {
		return nil, nil, err
	}

	t.Log("created temporary BOS bucket for BOS tests with name", tmpBucketName)
	return b, func() {
		objstore.EmptyBucket(t, context.Background(), b)
		if err := b.client.DeleteBucket(b.name); err != nil {
			t.Logf("deleting bucket %s failed: %s", tmpBucketName, err)
		}
	}, nil
}

func validateForTest(conf Config) error {
	if conf.Endpoint == "" ||
		conf.AccessKey == "" ||
		conf.SecretKey == "" {
		return errors.New("insufficient BOS configuration information")
	}
	return nil
}

func (b *Bucket) GetAndReplace(ctx context.Context, name string, f func(io.Reader) (io.Reader, error)) error {
	panic("unimplemented: BOS.GetAndReplace")
}
