// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

// Package s3 implements common object storage abstractions against s3-compatible APIs.
package s3

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/encrypt"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/version"
	"github.com/thanos-io/thanos/pkg/objstore"
	"github.com/thanos-io/thanos/pkg/runutil"
	"gopkg.in/yaml.v2"
)

// DirDelim is the delimiter used to model a directory structure in an object store bucket.
const DirDelim = "/"

var DefaultConfig = Config{
	PutUserMetadata: map[string]string{},
	HTTPConfig: HTTPConfig{
		IdleConnTimeout:       model.Duration(90 * time.Second),
		ResponseHeaderTimeout: model.Duration(2 * time.Minute),
	},
	// Minimum file size after which an HTTP multipart request should be used to upload objects to storage.
	// Set to 128 MiB as in the minio client.
	PartSize: 1024 * 1024 * 128,
}

// Config stores the configuration for s3 bucket.
type Config struct {
	Bucket          string            `yaml:"bucket"`
	Endpoint        string            `yaml:"endpoint"`
	Region          string            `yaml:"region"`
	AccessKey       string            `yaml:"access_key"`
	Insecure        bool              `yaml:"insecure"`
	SignatureV2     bool              `yaml:"signature_version2"`
	SSEEncryption   bool              `yaml:"encrypt_sse"`
	SecretKey       string            `yaml:"secret_key"`
	PutUserMetadata map[string]string `yaml:"put_user_metadata"`
	HTTPConfig      HTTPConfig        `yaml:"http_config"`
	TraceConfig     TraceConfig       `yaml:"trace"`
	// PartSize used for multipart upload. Only used if uploaded object size is known and larger than configured PartSize.
	PartSize uint64 `yaml:"part_size"`
}

type TraceConfig struct {
	Enable bool `yaml:"enable"`
}

// HTTPConfig stores the http.Transport configuration for the s3 minio client.
type HTTPConfig struct {
	IdleConnTimeout       model.Duration `yaml:"idle_conn_timeout"`
	ResponseHeaderTimeout model.Duration `yaml:"response_header_timeout"`
	InsecureSkipVerify    bool           `yaml:"insecure_skip_verify"`
}

// Bucket implements the store.Bucket interface against s3-compatible APIs.
type Bucket struct {
	logger          log.Logger
	name            string
	client          *minio.Client
	sse             encrypt.ServerSide
	putUserMetadata map[string]string
	partSize        uint64
}

// parseConfig unmarshals a buffer into a Config with default HTTPConfig values.
func parseConfig(conf []byte) (Config, error) {
	config := DefaultConfig
	if err := yaml.Unmarshal(conf, &config); err != nil {
		return Config{}, err
	}

	return config, nil
}

// NewBucket returns a new Bucket using the provided s3 config values.
func NewBucket(logger log.Logger, conf []byte, component string) (*Bucket, error) {
	config, err := parseConfig(conf)
	if err != nil {
		return nil, err
	}

	return NewBucketWithConfig(logger, config, component)
}

// NewBucketWithConfig returns a new Bucket using the provided s3 config values.
func NewBucketWithConfig(logger log.Logger, config Config, component string) (*Bucket, error) {
	var chain []credentials.Provider

	if err := validate(config); err != nil {
		return nil, err
	}
	if config.AccessKey != "" {
		signature := credentials.SignatureV4
		// TODO(bwplotka): Don't do flags, use actual v2, v4 params.
		if config.SignatureV2 {
			signature = credentials.SignatureV2
		}

		chain = []credentials.Provider{&credentials.Static{
			Value: credentials.Value{
				AccessKeyID:     config.AccessKey,
				SecretAccessKey: config.SecretKey,
				SignerType:      signature,
			},
		}}
	} else {
		chain = []credentials.Provider{
			&credentials.EnvAWS{},
			&credentials.FileAWSCredentials{},
			&credentials.IAM{
				Client: &http.Client{
					Transport: http.DefaultTransport,
				},
			},
		}
	}

	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewChainCredentials(chain),
		Secure: !config.Insecure,
		Region: config.Region,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,

			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			IdleConnTimeout:       time.Duration(config.HTTPConfig.IdleConnTimeout),
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			// The ResponseHeaderTimeout here is the only change
			// from the default minio transport, it was introduced
			// to cover cases where the tcp connection works but
			// the server never answers. Defaults to 2 minutes.
			ResponseHeaderTimeout: time.Duration(config.HTTPConfig.ResponseHeaderTimeout),
			// Set this value so that the underlying transport round-tripper
			// doesn't try to auto decode the body of objects with
			// content-encoding set to `gzip`.
			//
			// Refer: https://golang.org/src/net/http/transport.go?h=roundTrip#L1843.
			DisableCompression: true,
			TLSClientConfig:    &tls.Config{InsecureSkipVerify: config.HTTPConfig.InsecureSkipVerify},
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, "initialize s3 client")
	}
	client.SetAppInfo(fmt.Sprintf("thanos-%s", component), fmt.Sprintf("%s (%s)", version.Version, runtime.Version()))

	var sse encrypt.ServerSide
	if config.SSEEncryption {
		sse = encrypt.NewSSE()
	}

	if config.TraceConfig.Enable {
		logWriter := log.NewStdlibAdapter(level.Debug(logger), log.MessageKey("s3TraceMsg"))
		client.TraceOn(logWriter)
	}

	bkt := &Bucket{
		logger:          logger,
		name:            config.Bucket,
		client:          client,
		sse:             sse,
		putUserMetadata: config.PutUserMetadata,
		partSize:        config.PartSize,
	}
	return bkt, nil
}

// Name returns the bucket name for s3.
func (b *Bucket) Name() string {
	return b.name
}

// validate checks to see the config options are set.
func validate(conf Config) error {
	if conf.Endpoint == "" {
		return errors.New("no s3 endpoint in config file")
	}

	if conf.AccessKey == "" && conf.SecretKey != "" {
		return errors.New("no s3 acccess_key specified while secret_key is present in config file; either both should be present in config or envvars/IAM should be used.")
	}

	if conf.AccessKey != "" && conf.SecretKey == "" {
		return errors.New("no s3 secret_key specified while access_key is present in config file; either both should be present in config or envvars/IAM should be used.")
	}
	return nil
}

// ValidateForTests checks to see the config options for tests are set.
func ValidateForTests(conf Config) error {
	if conf.Endpoint == "" ||
		conf.AccessKey == "" ||
		conf.SecretKey == "" {
		return errors.New("insufficient s3 test configuration information")
	}
	return nil
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (b *Bucket) Iter(ctx context.Context, dir string, f func(string) error) error {
	// Ensure the object name actually ends with a dir suffix. Otherwise we'll just iterate the
	// object itself as one prefix item.
	if dir != "" {
		dir = strings.TrimSuffix(dir, DirDelim) + DirDelim
	}

	opts := minio.ListObjectsOptions{
		Prefix:    dir,
		Recursive: false,
	}

	for object := range b.client.ListObjects(ctx, b.name, opts) {
		// Catch the error when failed to list objects.
		if object.Err != nil {
			return object.Err
		}
		// This sometimes happens with empty buckets.
		if object.Key == "" {
			continue
		}
		// The s3 client can also return the directory itself in the ListObjects call above.
		if object.Key == dir {
			continue
		}
		if err := f(object.Key); err != nil {
			return err
		}
	}

	return nil
}

func (b *Bucket) getRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	opts := &minio.GetObjectOptions{ServerSideEncryption: b.sse}
	if length != -1 {
		if err := opts.SetRange(off, off+length-1); err != nil {
			return nil, err
		}
	} else if off > 0 {
		if err := opts.SetRange(off, 0); err != nil {
			return nil, err
		}
	}
	r, err := b.client.GetObject(ctx, b.name, name, *opts)
	if err != nil {
		return nil, err
	}

	// NotFoundObject error is revealed only after first Read. This does the initial GetRequest. Prefetch this here
	// for convenience.
	if _, err := r.Read(nil); err != nil {
		runutil.CloseWithLogOnErr(b.logger, r, "s3 get range obj close")

		// First GET Object request error.
		return nil, err
	}

	return r, nil
}

// Get returns a reader for the given object name.
func (b *Bucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.getRange(ctx, name, 0, -1)
}

// GetRange returns a new range reader for the given object name and range.
func (b *Bucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return b.getRange(ctx, name, off, length)
}

// Exists checks if the given object exists.
func (b *Bucket) Exists(ctx context.Context, name string) (bool, error) {
	_, err := b.client.StatObject(ctx, b.name, name, minio.StatObjectOptions{})
	if err != nil {
		if b.IsObjNotFoundErr(err) {
			return false, nil
		}
		return false, errors.Wrap(err, "stat s3 object")
	}

	return true, nil
}

// Upload the contents of the reader as an object into the bucket.
func (b *Bucket) Upload(ctx context.Context, name string, r io.Reader) error {
	// TODO(https://github.com/thanos-io/thanos/issues/678): Remove guessing length when minio provider will support multipart upload without this.
	size, err := objstore.TryToGetSize(r)
	if err != nil {
		level.Warn(b.logger).Log("msg", "could not guess file size for multipart upload; upload might be not optimized", "name", name, "err", err)
		size = -1
	}

	// partSize cannot be larger than object size.
	partSize := b.partSize
	if size < int64(partSize) {
		partSize = 0
	}
	if _, err := b.client.PutObject(
		ctx,
		b.name,
		name,
		r,
		size,
		minio.PutObjectOptions{
			PartSize:             partSize,
			ServerSideEncryption: b.sse,
			UserMetadata:         b.putUserMetadata,
		},
	); err != nil {
		return errors.Wrap(err, "upload s3 object")
	}

	return nil
}

// Attributes returns information about the specified object.
func (b *Bucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	objInfo, err := b.client.StatObject(ctx, b.name, name, minio.StatObjectOptions{})
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	return objstore.ObjectAttributes{
		Size:         objInfo.Size,
		LastModified: objInfo.LastModified,
	}, nil
}

// Delete removes the object with the given name.
func (b *Bucket) Delete(ctx context.Context, name string) error {
	return b.client.RemoveObject(ctx, b.name, name, minio.RemoveObjectOptions{})
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *Bucket) IsObjNotFoundErr(err error) bool {
	return minio.ToErrorResponse(err).Code == "NoSuchKey"
}

func (b *Bucket) Close() error { return nil }

func configFromEnv() Config {
	c := Config{
		Bucket:    os.Getenv("S3_BUCKET"),
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
	}

	c.Insecure, _ = strconv.ParseBool(os.Getenv("S3_INSECURE"))
	c.HTTPConfig.InsecureSkipVerify, _ = strconv.ParseBool(os.Getenv("S3_INSECURE_SKIP_VERIFY"))
	c.SignatureV2, _ = strconv.ParseBool(os.Getenv("S3_SIGNATURE_VERSION2"))
	return c
}

// NewTestBucket creates test bkt client that before returning creates temporary bucket.
// In a close function it empties and deletes the bucket.
func NewTestBucket(t testing.TB, location string) (objstore.Bucket, func(), error) {
	c := configFromEnv()
	if err := ValidateForTests(c); err != nil {
		return nil, nil, err
	}

	if c.Bucket != "" && os.Getenv("THANOS_ALLOW_EXISTING_BUCKET_USE") == "" {
		return nil, nil, errors.New("S3_BUCKET is defined. Normally this tests will create temporary bucket " +
			"and delete it after test. Unset S3_BUCKET env variable to use default logic. If you really want to run " +
			"tests against provided (NOT USED!) bucket, set THANOS_ALLOW_EXISTING_BUCKET_USE=true. WARNING: That bucket " +
			"needs to be manually cleared. This means that it is only useful to run one test in a time. This is due " +
			"to safety (accidentally pointing prod bucket for test) as well as aws s3 not being fully strong consistent.")
	}

	return NewTestBucketFromConfig(t, location, c, true)
}

func NewTestBucketFromConfig(t testing.TB, location string, c Config, reuseBucket bool) (objstore.Bucket, func(), error) {
	ctx := context.Background()

	bc, err := yaml.Marshal(c)
	if err != nil {
		return nil, nil, err
	}
	b, err := NewBucket(log.NewNopLogger(), bc, "thanos-e2e-test")
	if err != nil {
		return nil, nil, err
	}

	bktToCreate := c.Bucket
	if c.Bucket != "" && reuseBucket {
		if err := b.Iter(ctx, "", func(f string) error {
			return errors.Errorf("bucket %s is not empty", c.Bucket)
		}); err != nil {
			return nil, nil, errors.Wrapf(err, "s3 check bucket %s", c.Bucket)
		}

		t.Log("WARNING. Reusing", c.Bucket, "AWS bucket for AWS tests. Manual cleanup afterwards is required")
		return b, func() {}, nil
	}

	if c.Bucket == "" {
		bktToCreate = objstore.CreateTemporaryTestBucketName(t)
	}

	if err := b.client.MakeBucket(ctx, bktToCreate, minio.MakeBucketOptions{Region: location}); err != nil {
		return nil, nil, err
	}
	b.name = bktToCreate
	t.Log("created temporary AWS bucket for AWS tests with name", bktToCreate, "in", location)

	return b, func() {
		objstore.EmptyBucket(t, ctx, b)
		if err := b.client.RemoveBucket(ctx, bktToCreate); err != nil {
			t.Logf("deleting bucket %s failed: %s", bktToCreate, err)
		}
	}, nil
}
