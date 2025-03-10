// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package oss

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	alioss "github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/clientutil"
	"github.com/thanos-io/objstore/exthttp"
)

// PartSize is a part size for multi part upload.
const PartSize = 1024 * 1024 * 128

// Config stores the configuration for oss bucket.
type Config struct {
	Endpoint        string `yaml:"endpoint"`
	Bucket          string `yaml:"bucket"`
	AccessKeyID     string `yaml:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret"`
}

// Bucket implements the store.Bucket interface.
type Bucket struct {
	name   string
	logger log.Logger
	client *alioss.Client
	config Config
	bucket *alioss.Bucket
}

func NewTestBucket(t testing.TB) (objstore.Bucket, func(), error) {
	c := Config{
		Endpoint:        os.Getenv("ALIYUNOSS_ENDPOINT"),
		Bucket:          os.Getenv("ALIYUNOSS_BUCKET"),
		AccessKeyID:     os.Getenv("ALIYUNOSS_ACCESS_KEY_ID"),
		AccessKeySecret: os.Getenv("ALIYUNOSS_ACCESS_KEY_SECRET"),
	}

	if c.Endpoint == "" || c.AccessKeyID == "" || c.AccessKeySecret == "" {
		return nil, nil, errors.New("aliyun oss endpoint or access_key_id or access_key_secret " +
			"is not present in config file")
	}
	if c.Bucket != "" && os.Getenv("THANOS_ALLOW_EXISTING_BUCKET_USE") == "true" {
		t.Log("ALIYUNOSS_BUCKET is defined. Normally this tests will create temporary bucket " +
			"and delete it after test. Unset ALIYUNOSS_BUCKET env variable to use default logic. If you really want to run " +
			"tests against provided (NOT USED!) bucket, set THANOS_ALLOW_EXISTING_BUCKET_USE=true.")
		return NewTestBucketFromConfig(t, c, true)
	}
	return NewTestBucketFromConfig(t, c, false)
}

func (b *Bucket) Provider() objstore.ObjProvider { return objstore.ALIYUNOSS }

// Upload the contents of the reader as an object into the bucket.
func (b *Bucket) Upload(_ context.Context, name string, r io.Reader) error {
	// TODO(https://github.com/thanos-io/thanos/issues/678): Remove guessing length when minio provider will support multipart upload without this.
	size, err := objstore.TryToGetSize(r)
	if err != nil {
		return errors.Wrapf(err, "failed to get size apriori to upload %s", name)
	}

	chunksnum, lastslice := int(math.Floor(float64(size)/PartSize)), size%PartSize

	ncloser := io.NopCloser(r)
	switch chunksnum {
	case 0:
		if err := b.bucket.PutObject(name, ncloser); err != nil {
			return errors.Wrap(err, "failed to upload oss object")
		}
	default:
		{
			init, err := b.bucket.InitiateMultipartUpload(name)
			if err != nil {
				return errors.Wrap(err, "failed to initiate multi-part upload")
			}
			chunk := 0
			uploadEveryPart := func(everypartsize int64, cnk int) (alioss.UploadPart, error) {
				prt, err := b.bucket.UploadPart(init, ncloser, everypartsize, cnk)
				if err != nil {
					if err := b.bucket.AbortMultipartUpload(init); err != nil {
						return prt, errors.Wrap(err, "failed to abort multi-part upload")
					}

					return prt, errors.Wrap(err, "failed to upload multi-part chunk")
				}
				return prt, nil
			}
			var parts []alioss.UploadPart
			for ; chunk < chunksnum; chunk++ {
				part, err := uploadEveryPart(PartSize, chunk+1)
				if err != nil {
					return errors.Wrap(err, "failed to upload every part")
				}
				parts = append(parts, part)
			}
			if lastslice != 0 {
				part, err := uploadEveryPart(lastslice, chunksnum+1)
				if err != nil {
					return errors.Wrap(err, "failed to upload the last chunk")
				}
				parts = append(parts, part)
			}
			if _, err := b.bucket.CompleteMultipartUpload(init, parts); err != nil {
				return errors.Wrap(err, "failed to set multi-part upload completive")
			}
		}
	}
	return nil
}

// Delete removes the object with the given name.
func (b *Bucket) Delete(ctx context.Context, name string) error {
	if err := b.bucket.DeleteObject(name); err != nil {
		return errors.Wrap(err, "delete oss object")
	}
	return nil
}

// Attributes returns information about the specified object.
func (b *Bucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	m, err := b.bucket.GetObjectMeta(name)
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	size, err := clientutil.ParseContentLength(m)
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	// aliyun oss return Last-Modified header in RFC1123 format.
	// see api doc for details: https://www.alibabacloud.com/help/doc-detail/31985.htm
	mod, err := clientutil.ParseLastModified(m, time.RFC1123)
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	return objstore.ObjectAttributes{
		Size:         size,
		LastModified: mod,
	}, nil
}

// NewBucket returns a new Bucket using the provided oss config values.
func NewBucket(logger log.Logger, conf []byte, component string, wrapRoundtripper func(http.RoundTripper) http.RoundTripper) (*Bucket, error) {
	var config Config
	if err := yaml.Unmarshal(conf, &config); err != nil {
		return nil, errors.Wrap(err, "parse aliyun oss config file failed")
	}
	return NewBucketWithConfig(logger, config, component, wrapRoundtripper)
}

// NewBucketWithConfig returns a new Bucket using the provided oss config struct.
func NewBucketWithConfig(logger log.Logger, config Config, component string, wrapRoundtripper func(http.RoundTripper) http.RoundTripper) (*Bucket, error) {
	if err := validate(config); err != nil {
		return nil, err
	}
	var clientOptions []alioss.ClientOption
	if wrapRoundtripper != nil {
		rt, err := exthttp.DefaultTransport(exthttp.DefaultHTTPConfig)
		if err != nil {
			return nil, err
		}
		clientOptions = append(clientOptions, func(client *alioss.Client) {
			client.HTTPClient = &http.Client{
				Transport: wrapRoundtripper(rt),
			}
		})
	}
	client, err := alioss.New(config.Endpoint, config.AccessKeyID, config.AccessKeySecret, clientOptions...)
	if err != nil {
		return nil, errors.Wrap(err, "create aliyun oss client failed")
	}
	bk, err := client.Bucket(config.Bucket)
	if err != nil {
		return nil, errors.Wrapf(err, "use aliyun oss bucket %s failed", config.Bucket)
	}

	bkt := &Bucket{
		logger: logger,
		client: client,
		name:   config.Bucket,
		config: config,
		bucket: bk,
	}
	return bkt, nil
}

// validate checks to see the config options are set.
func validate(config Config) error {
	if config.Endpoint == "" || config.Bucket == "" {
		return errors.New("aliyun oss endpoint or bucket is not present in config file")
	}
	if config.AccessKeyID == "" || config.AccessKeySecret == "" {
		return errors.New("aliyun oss access_key_id or access_key_secret is not present in config file")
	}

	return nil
}

func (b *Bucket) SupportedIterOptions() []objstore.IterOptionType {
	return []objstore.IterOptionType{objstore.Recursive}
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (b *Bucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	if dir != "" {
		dir = strings.TrimSuffix(dir, objstore.DirDelim) + objstore.DirDelim
	}

	delimiter := alioss.Delimiter(objstore.DirDelim)
	if objstore.ApplyIterOptions(options...).Recursive {
		delimiter = nil
	}

	marker := alioss.Marker("")
	for {
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "context closed while iterating bucket")
		}
		objects, err := b.bucket.ListObjects(alioss.Prefix(dir), delimiter, marker)
		if err != nil {
			return errors.Wrap(err, "listing aliyun oss bucket failed")
		}
		marker = alioss.Marker(objects.NextMarker)

		for _, object := range objects.Objects {
			if err := f(object.Key); err != nil {
				return errors.Wrapf(err, "callback func invoke for object %s failed ", object.Key)
			}
		}

		for _, object := range objects.CommonPrefixes {
			if err := f(object); err != nil {
				return errors.Wrapf(err, "callback func invoke for directory %s failed", object)
			}
		}
		if !objects.IsTruncated {
			break
		}
	}

	return nil
}

func (b *Bucket) IterWithAttributes(ctx context.Context, dir string, f func(attrs objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	if err := objstore.ValidateIterOptions(b.SupportedIterOptions(), options...); err != nil {
		return err
	}

	return b.Iter(ctx, dir, func(name string) error {
		return f(objstore.IterObjectAttributes{Name: name})
	}, options...)
}

func (b *Bucket) Name() string {
	return b.name
}

func NewTestBucketFromConfig(t testing.TB, c Config, reuseBucket bool) (objstore.Bucket, func(), error) {
	if c.Bucket == "" {
		src := rand.NewSource(time.Now().UnixNano())

		bktToCreate := strings.ReplaceAll(fmt.Sprintf("test_%s_%x", strings.ToLower(t.Name()), src.Int63()), "_", "-")
		if len(bktToCreate) >= 63 {
			bktToCreate = bktToCreate[:63]
		}
		testclient, err := alioss.New(c.Endpoint, c.AccessKeyID, c.AccessKeySecret)
		if err != nil {
			return nil, nil, errors.Wrap(err, "create aliyun oss client failed")
		}

		if err := testclient.CreateBucket(bktToCreate); err != nil {
			return nil, nil, errors.Wrapf(err, "create aliyun oss bucket %s failed", bktToCreate)
		}
		c.Bucket = bktToCreate
	}

	bc, err := yaml.Marshal(c)
	if err != nil {
		return nil, nil, err
	}

	b, err := NewBucket(log.NewNopLogger(), bc, "thanos-aliyun-oss-test", nil)
	if err != nil {
		return nil, nil, err
	}

	if reuseBucket {
		if err := b.Iter(context.Background(), "", func(_ string) error {
			return errors.Errorf("bucket %s is not empty", c.Bucket)
		}); err != nil {
			return nil, nil, errors.Wrapf(err, "oss check bucket %s", c.Bucket)
		}

		t.Log("WARNING. Reusing", c.Bucket, "Aliyun OSS bucket for OSS tests. Manual cleanup afterwards is required")
		return b, func() {}, nil
	}

	return b, func() {
		objstore.EmptyBucket(t, context.Background(), b)
		if err := b.client.DeleteBucket(c.Bucket); err != nil {
			t.Logf("deleting bucket %s failed: %s", c.Bucket, err)
		}
	}, nil
}

func (b *Bucket) Close() error { return nil }

func (b *Bucket) setRange(start, end int64, name string) (alioss.Option, error) {
	var opt alioss.Option
	if 0 <= start && start <= end {
		header, err := b.bucket.GetObjectMeta(name)
		if err != nil {
			return nil, err
		}

		size, err := strconv.ParseInt(header["Content-Length"][0], 10, 64)
		if err != nil {
			return nil, err
		}

		if end > size {
			end = size - 1
		}

		opt = alioss.Range(start, end)
	} else {
		return nil, errors.Errorf("Invalid range specified: start=%d end=%d", start, end)
	}
	return opt, nil
}

func (b *Bucket) getRange(_ context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if name == "" {
		return nil, errors.New("given object name should not empty")
	}

	var opts []alioss.Option
	if length != -1 {
		opt, err := b.setRange(off, off+length-1, name)
		if err != nil {
			return nil, err
		}
		opts = append(opts, opt)
	}

	resp, err := b.bucket.DoGetObject(&oss.GetObjectRequest{ObjectKey: name}, opts)
	if err != nil {
		return nil, err
	}

	size, err := clientutil.ParseContentLength(resp.Response.Headers)
	if err == nil {
		return objstore.ObjectSizerReadCloser{
			ReadCloser: resp.Response,
			Size: func() (int64, error) {
				return size, nil
			},
		}, nil
	}

	return resp.Response, nil
}

// Get returns a reader for the given object name.
func (b *Bucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.getRange(ctx, name, 0, -1)
}

func (b *Bucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return b.getRange(ctx, name, off, length)
}

// Exists checks if the given object exists in the bucket.
func (b *Bucket) Exists(ctx context.Context, name string) (bool, error) {
	exists, err := b.bucket.IsObjectExist(name)
	if err != nil {
		if b.IsObjNotFoundErr(err) {
			return false, nil
		}
		return false, errors.Wrap(err, "cloud not check if object exists")
	}

	return exists, nil
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *Bucket) IsObjNotFoundErr(err error) bool {
	switch aliErr := errors.Cause(err).(type) {
	case alioss.ServiceError:
		if aliErr.StatusCode == http.StatusNotFound {
			return true
		}
	}
	return false
}

// IsAccessDeniedErr returns true if access to object is denied.
func (b *Bucket) IsAccessDeniedErr(err error) bool {
	switch aliErr := errors.Cause(err).(type) {
	case alioss.ServiceError:
		if aliErr.StatusCode == http.StatusForbidden {
			return true
		}
	}
	return false
}

func (b *Bucket) GetAndReplace(ctx context.Context, name string, f func(io.Reader) (io.Reader, error)) error {
	panic("unimplemented: OSS.GetAndReplace")
}
