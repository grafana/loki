// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package oci

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
	"github.com/oracle/oci-go-sdk/v65/objectstorage"
	"github.com/oracle/oci-go-sdk/v65/objectstorage/transfer"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/objstore"
)

// DirDelim is the delimiter used to model a directory structure in an object store bucket.
const DirDelim = "/"

type Provider string

const (
	defaultConfigProvider             = Provider("default")
	instancePrincipalConfigProvider   = Provider("instance-principal")
	rawConfigProvider                 = Provider("raw")
	okeWorkloadIdentityConfigProvider = Provider("oke-workload-identity")
)

var DefaultConfig = Config{
	HTTPConfig: HTTPConfig{
		IdleConnTimeout:       model.Duration(90 * time.Second),
		ResponseHeaderTimeout: model.Duration(2 * time.Minute),
		TLSHandshakeTimeout:   model.Duration(10 * time.Second),
		ExpectContinueTimeout: model.Duration(1 * time.Second),
		InsecureSkipVerify:    false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       0,
		DisableCompression:    false,
		ClientTimeout:         90 * time.Second,
	},
}

// HTTPConfig stores the http.Transport configuration for the OCI client.
type HTTPConfig struct {
	IdleConnTimeout       model.Duration `yaml:"idle_conn_timeout"`
	ResponseHeaderTimeout model.Duration `yaml:"response_header_timeout"`
	InsecureSkipVerify    bool           `yaml:"insecure_skip_verify"`

	TLSHandshakeTimeout   model.Duration    `yaml:"tls_handshake_timeout"`
	ExpectContinueTimeout model.Duration    `yaml:"expect_continue_timeout"`
	MaxIdleConns          int               `yaml:"max_idle_conns"`
	MaxIdleConnsPerHost   int               `yaml:"max_idle_conns_per_host"`
	MaxConnsPerHost       int               `yaml:"max_conns_per_host"`
	DisableCompression    bool              `yaml:"disable_compression"`
	ClientTimeout         time.Duration     `yaml:"client_timeout"`
	Transport             http.RoundTripper `yaml:"-"`
}

// Config stores the configuration for oci bucket.
type Config struct {
	Provider             string     `yaml:"provider"`
	Bucket               string     `yaml:"bucket"`
	Compartment          string     `yaml:"compartment_ocid"`
	Tenancy              string     `yaml:"tenancy_ocid"`
	User                 string     `yaml:"user_ocid"`
	Region               string     `yaml:"region"`
	Fingerprint          string     `yaml:"fingerprint"`
	PrivateKey           string     `yaml:"privatekey"`
	Passphrase           string     `yaml:"passphrase"`
	PartSize             int64      `yaml:"part_size"`
	MaxRequestRetries    int        `yaml:"max_request_retries"`
	RequestRetryInterval int        `yaml:"request_retry_interval"`
	HTTPConfig           HTTPConfig `yaml:"http_config"`
}

// Bucket implements the store.Bucket interface against OCI APIs.
type Bucket struct {
	logger          log.Logger
	name            string
	namespace       string
	client          *objectstorage.ObjectStorageClient
	partSize        int64
	requestMetadata common.RequestMetadata
}

func (b *Bucket) Provider() objstore.ObjProvider { return objstore.OCI }

// Name returns the bucket name for the provider.
func (b *Bucket) Name() string {
	return b.name
}

func (b *Bucket) SupportedIterOptions() []objstore.IterOptionType {
	return []objstore.IterOptionType{objstore.Recursive}
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (b *Bucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	// Ensure the object name actually ends with a dir suffix. Otherwise we'll just iterate the
	// object itself as one prefix item.
	if dir != "" {
		dir = strings.TrimSuffix(dir, DirDelim) + DirDelim
	}

	objectNames, err := listAllObjects(ctx, *b, dir, options...)
	if err != nil {
		return errors.Wrapf(err, "cannot list objects in directory '%s'", dir)
	}

	level.Debug(b.logger).Log("NumberOfObjects", len(objectNames))

	for _, objectName := range objectNames {
		if objectName == "" || objectName == dir {
			continue
		}

		if err := f(objectName); err != nil {
			return err
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

// Get returns a reader for the given object name.
func (b *Bucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	response, err := getObject(ctx, *b, name, "")
	if err != nil {
		return nil, err
	}
	return objstore.ObjectSizerReadCloser{
		ReadCloser: response.Content,
		Size: func() (int64, error) {
			return *response.ContentLength, nil
		},
	}, nil
}

// GetRange returns a new range reader for the given object name and range.
func (b *Bucket) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	level.Debug(b.logger).Log("msg", "getting object", "name", name, "off", offset, "length", length)

	// A single byte range to fetch, as described in RFC 7233 (https://tools.ietf.org/html/rfc7233#section-2.1).
	byteRange := ""

	if offset >= 0 {
		if length > 0 {
			byteRange = fmt.Sprintf("bytes=%d-%d", offset, offset+length-1)
		} else {
			byteRange = fmt.Sprintf("bytes=%d-", offset)
		}
	} else {
		if length > 0 {
			byteRange = fmt.Sprintf("bytes=-%d", length)
		} else {
			return nil, errors.New(fmt.Sprintf("invalid range specified: offset=%d length=%d", offset, length))
		}
	}

	level.Debug(b.logger).Log("byteRange", byteRange)

	response, err := getObject(ctx, *b, name, byteRange)
	if err != nil {
		return nil, err
	}
	return objstore.ObjectSizerReadCloser{ReadCloser: response.Content,
		Size: func() (int64, error) {
			return *response.ContentLength, nil
		},
	}, nil
}

// Upload the contents of the reader as an object into the bucket.
// Upload should be idempotent.
func (b *Bucket) Upload(ctx context.Context, name string, r io.Reader) (err error) {
	req := transfer.UploadStreamRequest{
		UploadRequest: transfer.UploadRequest{
			NamespaceName:                       common.String(b.namespace),
			BucketName:                          common.String(b.name),
			ObjectName:                          &name,
			EnableMultipartChecksumVerification: common.Bool(true), // TODO: should we check?
			ObjectStorageClient:                 b.client,
			RequestMetadata:                     b.requestMetadata,
		},
		StreamReader: r,
	}
	if b.partSize > 0 {
		req.UploadRequest.PartSize = &b.partSize
	}

	uploadManager := transfer.NewUploadManager()
	_, err = uploadManager.UploadStream(ctx, req)

	return err
}

// Exists checks if the given object exists in the bucket.
func (b *Bucket) Exists(ctx context.Context, name string) (bool, error) {
	_, err := getObject(ctx, *b, name, "")
	if err != nil {
		if b.IsObjNotFoundErr(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "cannot get OCI object '%s'", name)
	}
	return true, nil
}

// Delete removes the object with the given name.
// If object does not exists in the moment of deletion, Delete should throw error.
func (b *Bucket) Delete(ctx context.Context, name string) (err error) {
	request := objectstorage.DeleteObjectRequest{
		NamespaceName:   &b.namespace,
		BucketName:      &b.name,
		ObjectName:      &name,
		RequestMetadata: b.requestMetadata,
	}
	_, err = b.client.DeleteObject(ctx, request)
	return err
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *Bucket) IsObjNotFoundErr(err error) bool {
	failure, isServiceError := common.IsServiceError(err)
	if isServiceError {
		k := failure.GetHTTPStatusCode()
		match := k == http.StatusNotFound
		level.Debug(b.logger).Log("msg", match)
		return failure.GetHTTPStatusCode() == http.StatusNotFound
	}
	return false
}

// IsAccessDeniedErr returns true if access to object is denied.
func (b *Bucket) IsAccessDeniedErr(err error) bool {
	failure, isServiceError := common.IsServiceError(err)
	if isServiceError {
		return failure.GetHTTPStatusCode() == http.StatusForbidden
	}
	return false
}

// ObjectSize returns the size of the specified object.
func (b *Bucket) ObjectSize(ctx context.Context, name string) (uint64, error) {
	response, err := getObject(ctx, *b, name, "")
	if err != nil {
		return 0, err
	}
	return uint64(*response.ContentLength), nil
}

// Close closes bucket.
func (b *Bucket) Close() error {
	return nil
}

// Attributes returns information about the specified object.
func (b *Bucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	response, err := getObject(ctx, *b, name, "")
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}
	return objstore.ObjectAttributes{
		Size:         *response.ContentLength,
		LastModified: response.LastModified.Time,
	}, nil
}

// createBucket creates bucket.
func (b *Bucket) createBucket(ctx context.Context, compartmentId string) (err error) {
	request := objectstorage.CreateBucketRequest{
		NamespaceName:   &b.namespace,
		RequestMetadata: b.requestMetadata,
	}
	request.CompartmentId = &compartmentId
	request.Name = &b.name
	request.Metadata = make(map[string]string)
	request.PublicAccessType = objectstorage.CreateBucketDetailsPublicAccessTypeNopublicaccess
	_, err = b.client.CreateBucket(ctx, request)
	return err
}

// deleteBucket deletes bucket.
func (b *Bucket) deleteBucket(ctx context.Context) (err error) {
	request := objectstorage.DeleteBucketRequest{
		NamespaceName:   &b.namespace,
		BucketName:      &b.name,
		RequestMetadata: b.requestMetadata,
	}
	_, err = b.client.DeleteBucket(ctx, request)
	return err
}

// NewBucket returns a new Bucket using the provided oci config values.
func NewBucket(logger log.Logger, ociConfig []byte, wrapRoundtripper func(http.RoundTripper) http.RoundTripper) (*Bucket, error) {
	level.Debug(logger).Log("msg", "creating new oci bucket connection")
	var config = DefaultConfig
	var configurationProvider common.ConfigurationProvider
	var err error

	if err := yaml.Unmarshal(ociConfig, &config); err != nil {
		return nil, errors.Wrapf(err, "unable to unmarshal the given oci configurations")
	}

	provider := Provider(strings.ToLower(config.Provider))
	level.Info(logger).Log("msg", "creating OCI client", "provider", provider)
	switch provider {
	case defaultConfigProvider:
		configurationProvider = common.DefaultConfigProvider()
	case instancePrincipalConfigProvider:
		configurationProvider, err = auth.InstancePrincipalConfigurationProvider()
		if err != nil {
			return nil, errors.Wrapf(err, "unable to create OCI instance principal config provider")
		}
	case rawConfigProvider:
		if err := config.validateConfig(); err != nil {
			return nil, errors.Wrapf(err, "invalid oci configurations")
		}
		configurationProvider = common.NewRawConfigurationProvider(config.Tenancy, config.User, config.Region,
			config.Fingerprint, config.PrivateKey, &config.Passphrase)
	case okeWorkloadIdentityConfigProvider:
		if err := os.Setenv(auth.ResourcePrincipalVersionEnvVar, auth.ResourcePrincipalVersion2_2); err != nil {
			return nil, errors.Wrapf(err, "unable to set environment variable: %s", auth.ResourcePrincipalVersionEnvVar)
		}
		if err := os.Setenv(auth.ResourcePrincipalRegionEnvVar, config.Region); err != nil {
			return nil, errors.Wrapf(err, "unable to set environment variable: %s", auth.ResourcePrincipalRegionEnvVar)
		}

		configurationProvider, err = auth.OkeWorkloadIdentityConfigurationProvider()
		if err != nil {
			return nil, errors.Wrapf(err, "unable to create OKE workload identity config provider")
		}
	default:
		return nil, fmt.Errorf("unsupported OCI provider: %s", provider)
	}

	client, err := objectstorage.NewObjectStorageClientWithConfigurationProvider(configurationProvider)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to create ObjectStorage client with the given oci configurations")
	}
	var rt http.RoundTripper
	rt = CustomTransport(config)
	if config.HTTPConfig.Transport != nil {
		rt = config.HTTPConfig.Transport
	}
	if wrapRoundtripper != nil {
		rt = wrapRoundtripper(rt)
	}
	httpClient := http.Client{
		Transport: rt,
		Timeout:   config.HTTPConfig.ClientTimeout,
	}
	client.HTTPClient = &httpClient

	requestMetadata := getRequestMetadata(config.MaxRequestRetries, config.RequestRetryInterval)

	level.Info(logger).Log("msg", "getting namespace, it might take some time")
	namespace, err := getNamespace(client, requestMetadata)
	if err != nil {
		return nil, err
	}
	level.Debug(logger).Log("msg", fmt.Sprintf("Oracle Cloud Infrastructure tenancy namespace: %s", *namespace))

	bkt := Bucket{
		logger:          logger,
		name:            config.Bucket,
		namespace:       *namespace,
		client:          &client,
		partSize:        config.PartSize,
		requestMetadata: requestMetadata,
	}

	return &bkt, nil
}

// NewTestBucket creates test bkt client that before returning creates temporary bucket.
// In a close function it empties and deletes the bucket.
func NewTestBucket(t testing.TB) (objstore.Bucket, func(), error) {
	config, err := getConfigFromEnv()
	if err != nil {
		return nil, nil, err
	}

	ociConfig, err := yaml.Marshal(config)
	if err != nil {
		return nil, nil, err
	}

	bkt, err := NewBucket(log.NewNopLogger(), ociConfig, nil)
	if err != nil {
		return nil, nil, err
	}

	ctx := context.Background()
	bkt.name = objstore.CreateTemporaryTestBucketName(t)
	if err := bkt.createBucket(ctx, config.Compartment); err != nil {
		t.Errorf("failed to create temporary Oracle Cloud Infrastructure bucket '%s' for testing", bkt.name)
		return nil, nil, err
	}

	t.Logf("created temporary Oracle Cloud Infrastructure bucket '%s' for testing", bkt.name)
	return bkt, func() {
		objstore.EmptyBucket(t, ctx, bkt)
		if err := bkt.deleteBucket(ctx); err != nil {
			t.Logf("failed to delete temporary Oracle Cloud Infrastructure bucket %s for testing: %s", bkt.name, err)
		}
		t.Logf("deleted temporary Oracle Cloud Infrastructure bucket '%s' for testing", bkt.name)
	}, nil
}

func (b *Bucket) GetAndReplace(ctx context.Context, name string, f func(io.Reader) (io.Reader, error)) error {
	panic("unimplemented: OCI.GetAndReplace")
}
