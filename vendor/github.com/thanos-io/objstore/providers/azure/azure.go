// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package azure

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
)

// DefaultConfig for Azure objstore client.
var DefaultConfig = Config{
	Endpoint:               "blob.core.windows.net",
	StorageCreateContainer: true,
	HTTPConfig: exthttp.HTTPConfig{
		IdleConnTimeout:       model.Duration(90 * time.Second),
		ResponseHeaderTimeout: model.Duration(2 * time.Minute),
		TLSHandshakeTimeout:   model.Duration(10 * time.Second),
		ExpectContinueTimeout: model.Duration(1 * time.Second),
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   100,
		MaxConnsPerHost:       0,
		DisableCompression:    false,
	},
}

// Config Azure storage configuration.
type Config struct {
	AzTenantID              string             `yaml:"az_tenant_id"`
	ClientID                string             `yaml:"client_id"`
	ClientSecret            string             `yaml:"client_secret"`
	StorageAccountName      string             `yaml:"storage_account"`
	StorageAccountKey       string             `yaml:"storage_account_key"`
	StorageConnectionString string             `yaml:"storage_connection_string"`
	StorageCreateContainer  bool               `yaml:"storage_create_container"`
	ContainerName           string             `yaml:"container"`
	Endpoint                string             `yaml:"endpoint"`
	UserAssignedID          string             `yaml:"user_assigned_id"`
	MaxRetries              int                `yaml:"max_retries"`
	ReaderConfig            ReaderConfig       `yaml:"reader_config"`
	PipelineConfig          PipelineConfig     `yaml:"pipeline_config"`
	HTTPConfig              exthttp.HTTPConfig `yaml:"http_config"`

	// Deprecated: Is automatically set by the Azure SDK.
	MSIResource string `yaml:"msi_resource"`
}

type ReaderConfig struct {
	MaxRetryRequests int `yaml:"max_retry_requests"`
}

type PipelineConfig struct {
	MaxTries      int32          `yaml:"max_tries"`
	TryTimeout    model.Duration `yaml:"try_timeout"`
	RetryDelay    model.Duration `yaml:"retry_delay"`
	MaxRetryDelay model.Duration `yaml:"max_retry_delay"`
}

// Validate checks to see if any of the config options are set.
func (conf *Config) validate() error {
	var errMsg []string
	if conf.UserAssignedID != "" && conf.StorageAccountKey != "" {
		errMsg = append(errMsg, "user_assigned_id cannot be set when using storage_account_key authentication")
	}

	if conf.UserAssignedID != "" && conf.StorageConnectionString != "" {
		errMsg = append(errMsg, "user_assigned_id cannot be set when using storage_connection_string authentication")
	}

	if conf.UserAssignedID != "" && conf.ClientID != "" {
		errMsg = append(errMsg, "user_assigned_id cannot be set when using client_id authentication")
	}

	if (conf.AzTenantID != "" || conf.ClientSecret != "" || conf.ClientID != "") && (conf.AzTenantID == "" || conf.ClientSecret == "" || conf.ClientID == "") {
		errMsg = append(errMsg, "az_tenant_id, client_id, and client_secret must be set together")
	}

	if conf.StorageAccountKey != "" && conf.StorageConnectionString != "" {
		errMsg = append(errMsg, "storage_account_key and storage_connection_string cannot both be set")
	}

	if conf.StorageAccountName == "" {
		errMsg = append(errMsg, "storage_account_name is required but not configured")
	}

	if conf.ContainerName == "" {
		errMsg = append(errMsg, "no container specified")
	}

	if conf.PipelineConfig.MaxTries < 0 {
		errMsg = append(errMsg, "The value of max_tries must be greater than or equal to 0 in the config file")
	}

	if conf.ReaderConfig.MaxRetryRequests < 0 {
		errMsg = append(errMsg, "The value of max_retry_requests must be greater than or equal to 0 in the config file")
	}

	if len(errMsg) > 0 {
		return errors.New(strings.Join(errMsg, ", "))
	}

	return nil
}

// HTTPConfig exists here only because Cortex depends on it, and we depend on Cortex.
// Deprecated.
// TODO(bwplotka): Remove it, once we remove Cortex cycle dep, or Cortex stops using this.
type HTTPConfig = exthttp.HTTPConfig

// parseConfig unmarshals a buffer into a Config with default values.
func parseConfig(conf []byte) (Config, error) {
	config := DefaultConfig
	if err := yaml.UnmarshalStrict(conf, &config); err != nil {
		return Config{}, err
	}

	// If we don't have config specific retry values but we do have the generic MaxRetries.
	// This is for backwards compatibility but also ease of configuration.
	if config.MaxRetries > 0 {
		if config.PipelineConfig.MaxTries == 0 {
			config.PipelineConfig.MaxTries = int32(config.MaxRetries)
		}
		if config.ReaderConfig.MaxRetryRequests == 0 {
			config.ReaderConfig.MaxRetryRequests = config.MaxRetries
		}
	}

	return config, nil
}

// Bucket implements the store.Bucket interface against Azure APIs.
type Bucket struct {
	logger           log.Logger
	containerClient  *container.Client
	containerName    string
	readerMaxRetries int
}

// NewBucket returns a new Bucket using the provided Azure config.
func NewBucket(logger log.Logger, azureConfig []byte, component string, wrapRoundtripper func(http.RoundTripper) http.RoundTripper) (*Bucket, error) {
	level.Debug(logger).Log("msg", "creating new Azure bucket connection", "component", component)
	conf, err := parseConfig(azureConfig)
	if err != nil {
		return nil, err
	}
	if conf.MSIResource != "" {
		level.Warn(logger).Log("msg", "The field msi_resource has been deprecated and should no longer be set")
	}
	return NewBucketWithConfig(logger, conf, component, wrapRoundtripper)
}

// NewBucketWithConfig returns a new Bucket using the provided Azure config struct.
func NewBucketWithConfig(logger log.Logger, conf Config, component string, wrapRoundtripper func(http.RoundTripper) http.RoundTripper) (*Bucket, error) {
	if err := conf.validate(); err != nil {
		return nil, err
	}

	containerClient, err := getContainerClient(conf, wrapRoundtripper)
	if err != nil {
		return nil, err
	}

	// Check if storage account container already exists, and create one if it does not.
	if conf.StorageCreateContainer {
		ctx := context.Background()
		_, err = containerClient.GetProperties(ctx, &container.GetPropertiesOptions{})
		if err != nil {
			if !bloberror.HasCode(err, bloberror.ContainerNotFound) {
				return nil, err
			}
			_, err := containerClient.Create(ctx, nil)
			if err != nil {
				return nil, errors.Wrapf(err, "error creating Azure blob container: %s", conf.ContainerName)
			}
			level.Info(logger).Log("msg", "Azure blob container successfully created", "address", conf.ContainerName)
		}
	}
	bkt := &Bucket{
		logger:           logger,
		containerClient:  containerClient,
		containerName:    conf.ContainerName,
		readerMaxRetries: conf.ReaderConfig.MaxRetryRequests,
	}
	return bkt, nil
}

func (b *Bucket) Provider() objstore.ObjProvider { return objstore.AZURE }

func (b *Bucket) SupportedIterOptions() []objstore.IterOptionType {
	return []objstore.IterOptionType{objstore.Recursive, objstore.UpdatedAt}
}

func (b *Bucket) IterWithAttributes(ctx context.Context, dir string, f func(attrs objstore.IterObjectAttributes) error, options ...objstore.IterOption) error {
	if err := objstore.ValidateIterOptions(b.SupportedIterOptions(), options...); err != nil {
		return err
	}

	prefix := dir
	if prefix != "" && !strings.HasSuffix(prefix, DirDelim) {
		prefix += DirDelim
	}

	params := objstore.ApplyIterOptions(options...)
	if params.Recursive {
		opt := &container.ListBlobsFlatOptions{Prefix: &prefix}
		pager := b.containerClient.NewListBlobsFlatPager(opt)
		for pager.More() {
			resp, err := pager.NextPage(ctx)
			if err != nil {
				return err
			}
			for _, blob := range resp.Segment.BlobItems {
				attrs := objstore.IterObjectAttributes{
					Name: *blob.Name,
				}
				if params.LastModified {
					attrs.SetLastModified(*blob.Properties.LastModified)
				}
				if err := f(attrs); err != nil {
					return err
				}
			}
		}
		return nil
	}

	opt := &container.ListBlobsHierarchyOptions{Prefix: &prefix}
	pager := b.containerClient.NewListBlobsHierarchyPager(DirDelim, opt)
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, blobItem := range resp.Segment.BlobItems {
			attrs := objstore.IterObjectAttributes{
				Name: *blobItem.Name,
			}
			if params.LastModified {
				attrs.SetLastModified(*blobItem.Properties.LastModified)
			}
			if err := f(attrs); err != nil {
				return err
			}
		}
		for _, blobPrefix := range resp.Segment.BlobPrefixes {
			if err := f(objstore.IterObjectAttributes{Name: *blobPrefix.Name}); err != nil {
				return err
			}
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

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *Bucket) IsObjNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return bloberror.HasCode(err, bloberror.BlobNotFound) || bloberror.HasCode(err, bloberror.InvalidURI)
}

// IsAccessDeniedErr returns true if access to object is denied.
func (b *Bucket) IsAccessDeniedErr(err error) bool {
	if err == nil {
		return false
	}
	return bloberror.HasCode(err, bloberror.AuthorizationPermissionMismatch) || bloberror.HasCode(err, bloberror.InsufficientAccountPermissions)
}

func (b *Bucket) getBlobReader(ctx context.Context, name string, httpRange blob.HTTPRange) (io.ReadCloser, error) {
	level.Debug(b.logger).Log("msg", "getting blob", "blob", name, "offset", httpRange.Offset, "length", httpRange.Count)
	if name == "" {
		return nil, errors.New("blob name cannot be empty")
	}
	blobClient := b.containerClient.NewBlobClient(name)
	downloadOpt := &blob.DownloadStreamOptions{
		Range: httpRange,
	}
	resp, err := blobClient.DownloadStream(ctx, downloadOpt)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot download blob, address: %s", blobClient.URL())
	}
	retryOpts := azblob.RetryReaderOptions{MaxRetries: int32(b.readerMaxRetries)}

	return objstore.ObjectSizerReadCloser{
		ReadCloser: resp.NewRetryReader(ctx, &retryOpts),
		Size: func() (int64, error) {
			return *resp.ContentLength, nil
		},
	}, nil
}

// Get returns a reader for the given object name.
func (b *Bucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.getBlobReader(ctx, name, blob.HTTPRange{})
}

// GetRange returns a new range reader for the given object name and range.
func (b *Bucket) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	return b.getBlobReader(ctx, name, blob.HTTPRange{Offset: offset, Count: length})
}

// Attributes returns information about the specified object.
func (b *Bucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	level.Debug(b.logger).Log("msg", "Getting blob attributes", "blob", name)
	blobClient := b.containerClient.NewBlobClient(name)
	resp, err := blobClient.GetProperties(ctx, nil)
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}
	return objstore.ObjectAttributes{
		Size:         *resp.ContentLength,
		LastModified: *resp.LastModified,
	}, nil
}

// Exists checks if the given object exists.
func (b *Bucket) Exists(ctx context.Context, name string) (bool, error) {
	level.Debug(b.logger).Log("msg", "checking if blob exists", "blob", name)
	blobClient := b.containerClient.NewBlobClient(name)
	if _, err := blobClient.GetProperties(ctx, nil); err != nil {
		if b.IsObjNotFoundErr(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "cannot get properties for Azure blob, address: %s", name)
	}
	return true, nil
}

// Upload the contents of the reader as an object into the bucket.
func (b *Bucket) Upload(ctx context.Context, name string, r io.Reader) error {
	level.Debug(b.logger).Log("msg", "uploading blob", "blob", name)
	blobClient := b.containerClient.NewBlockBlobClient(name)
	opts := &blockblob.UploadStreamOptions{
		BlockSize:   3 * 1024 * 1024,
		Concurrency: 4,
	}
	if _, err := blobClient.UploadStream(ctx, r, opts); err != nil {
		return errors.Wrapf(err, "cannot upload Azure blob, address: %s", name)
	}
	return nil
}

// Delete removes the object with the given name.
func (b *Bucket) Delete(ctx context.Context, name string) error {
	level.Debug(b.logger).Log("msg", "deleting blob", "blob", name)
	blobClient := b.containerClient.NewBlobClient(name)
	opt := &blob.DeleteOptions{
		DeleteSnapshots: to.Ptr(blob.DeleteSnapshotsOptionTypeInclude),
	}
	if _, err := blobClient.Delete(ctx, opt); err != nil {
		return errors.Wrapf(err, "error deleting blob, address: %s", name)
	}
	return nil
}

// Name returns Azure container name.
func (b *Bucket) Name() string {
	return b.containerName
}

// NewTestBucket creates test bkt client that before returning creates temporary bucket.
// In a close function it empties and deletes the bucket.
func NewTestBucket(t testing.TB, component string) (objstore.Bucket, func(), error) {
	t.Log("Using test Azure bucket.")

	conf := &DefaultConfig
	conf.StorageAccountName = os.Getenv("AZURE_STORAGE_ACCOUNT")
	conf.StorageAccountKey = os.Getenv("AZURE_STORAGE_ACCESS_KEY")
	conf.ContainerName = objstore.CreateTemporaryTestBucketName(t)

	bc, err := yaml.Marshal(conf)
	if err != nil {
		return nil, nil, err
	}
	bkt, err := NewBucket(log.NewNopLogger(), bc, component, nil)
	if err != nil {
		t.Errorf("Cannot create Azure storage container:")
		return nil, nil, err
	}
	ctx := context.Background()
	return bkt, func() {
		objstore.EmptyBucket(t, ctx, bkt)
		_, err := bkt.containerClient.Delete(ctx, &container.DeleteOptions{})
		if err != nil {
			t.Logf("deleting bucket failed: %s", err)
		}
	}, nil
}

// Close bucket.
func (b *Bucket) Close() error {
	return nil
}
