// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package azure

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"os"
	"strings"
	"testing"

	blob "github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/objstore"
	yaml "gopkg.in/yaml.v2"
)

const (
	azureDefaultEndpoint = "blob.core.windows.net"
)

// Config Azure storage configuration.
type Config struct {
	StorageAccountName string `yaml:"storage_account"`
	StorageAccountKey  string `yaml:"storage_account_key"`
	ContainerName      string `yaml:"container"`
	Endpoint           string `yaml:"endpoint"`
	MaxRetries         int    `yaml:"max_retries"`
}

// Bucket implements the store.Bucket interface against Azure APIs.
type Bucket struct {
	logger       log.Logger
	containerURL blob.ContainerURL
	config       *Config
}

// Validate checks to see if any of the config options are set.
func (conf *Config) validate() error {
	if conf.StorageAccountName == "" ||
		conf.StorageAccountKey == "" {
		return errors.New("invalid Azure storage configuration")
	}
	if conf.StorageAccountName == "" && conf.StorageAccountKey != "" {
		return errors.New("no Azure storage_account specified while storage_account_key is present in config file; both should be present")
	}
	if conf.StorageAccountName != "" && conf.StorageAccountKey == "" {
		return errors.New("no Azure storage_account_key specified while storage_account is present in config file; both should be present")
	}
	if conf.ContainerName == "" {
		return errors.New("no Azure container specified")
	}
	if conf.Endpoint == "" {
		conf.Endpoint = azureDefaultEndpoint
	}
	if conf.MaxRetries < 0 {
		return errors.New("the value of maxretries must be greater than or equal to 0 in the config file")
	}
	return nil
}

// NewBucket returns a new Bucket using the provided Azure config.
func NewBucket(logger log.Logger, azureConfig []byte, component string) (*Bucket, error) {
	level.Debug(logger).Log("msg", "creating new Azure bucket connection", "component", component)

	var conf Config
	if err := yaml.Unmarshal(azureConfig, &conf); err != nil {
		return nil, err
	}

	if err := conf.validate(); err != nil {
		return nil, err
	}

	ctx := context.Background()
	container, err := createContainer(ctx, conf)
	if err != nil {
		ret, ok := err.(blob.StorageError)
		if !ok {
			return nil, errors.Wrapf(err, "Azure API return unexpected error: %T\n", err)
		}
		if ret.ServiceCode() == "ContainerAlreadyExists" {
			level.Debug(logger).Log("msg", "Getting connection to existing Azure blob container", "container", conf.ContainerName)
			container, err = getContainer(ctx, conf)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot get existing Azure blob container: %s", container)
			}
		} else {
			return nil, errors.Wrapf(err, "error creating Azure blob container: %s", container)
		}
	} else {
		level.Info(logger).Log("msg", "Azure blob container successfully created", "address", container)
	}

	bkt := &Bucket{
		logger:       logger,
		containerURL: container,
		config:       &conf,
	}
	return bkt, nil
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (b *Bucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	prefix := dir
	if prefix != "" && !strings.HasSuffix(prefix, DirDelim) {
		prefix += DirDelim
	}

	marker := blob.Marker{}
	params := objstore.ApplyIterOptions(options...)
	listOptions := blob.ListBlobsSegmentOptions{Prefix: prefix}

	for i := 1; ; i++ {
		var (
			blobPrefixes []blob.BlobPrefix
			blobItems    []blob.BlobItem
		)

		if params.Recursive {
			list, err := b.containerURL.ListBlobsFlatSegment(ctx, marker, listOptions)
			if err != nil {
				return errors.Wrapf(err, "cannot list flat blobs with prefix %s (iteration #%d)", dir, i)
			}

			marker = list.NextMarker
			blobItems = list.Segment.BlobItems
			blobPrefixes = nil
		} else {
			list, err := b.containerURL.ListBlobsHierarchySegment(ctx, marker, DirDelim, listOptions)
			if err != nil {
				return errors.Wrapf(err, "cannot list hierarchy blobs with prefix %s (iteration #%d)", dir, i)
			}

			marker = list.NextMarker
			blobItems = list.Segment.BlobItems
			blobPrefixes = list.Segment.BlobPrefixes
		}

		var listNames []string

		for _, blob := range blobItems {
			listNames = append(listNames, blob.Name)
		}

		for _, blobPrefix := range blobPrefixes {
			listNames = append(listNames, blobPrefix.Name)
		}

		for _, name := range listNames {
			if err := f(name); err != nil {
				return err
			}
		}

		// Continue iterating if we are not done.
		if !marker.NotDone() {
			break
		}

		level.Debug(b.logger).Log("msg", "requesting next iteration of listing blobs", "last_entries", len(listNames), "iteration", i)
	}

	return nil
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (b *Bucket) IsObjNotFoundErr(err error) bool {
	if err == nil {
		return false
	}

	errorCode := parseError(err.Error())
	if errorCode == "InvalidUri" || errorCode == "BlobNotFound" {
		return true
	}

	return false
}

func (b *Bucket) getBlobReader(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	level.Debug(b.logger).Log("msg", "getting blob", "blob", name, "offset", offset, "length", length)
	if len(name) == 0 {
		return nil, errors.New("X-Ms-Error-Code: [EmptyContainerName]")
	}
	exists, err := b.Exists(ctx, name)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get blob reader: %s", name)
	}

	if !exists {
		return nil, errors.New("X-Ms-Error-Code: [BlobNotFound]")
	}

	blobURL, err := getBlobURL(ctx, *b.config, name)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get Azure blob URL, address: %s", name)
	}
	var props *blob.BlobGetPropertiesResponse
	props, err = blobURL.GetProperties(ctx, blob.BlobAccessConditions{})
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get properties for container: %s", name)
	}

	var size int64
	// If a length is specified and it won't go past the end of the file,
	// then set it as the size.
	if length > 0 && length <= props.ContentLength()-offset {
		size = length
		level.Debug(b.logger).Log("msg", "set size to length", "size", size, "length", length, "offset", offset, "name", name)
	} else {
		size = props.ContentLength() - offset
		level.Debug(b.logger).Log("msg", "set size to go to EOF", "contentlength", props.ContentLength(), "size", size, "length", length, "offset", offset, "name", name)
	}

	destBuffer := make([]byte, size)

	if err := blob.DownloadBlobToBuffer(context.Background(), blobURL.BlobURL, offset, size,
		destBuffer, blob.DownloadFromBlobOptions{
			BlockSize:   blob.BlobDefaultDownloadBlockSize,
			Parallelism: uint16(3),
			Progress:    nil,
			RetryReaderOptionsPerBlock: blob.RetryReaderOptions{
				MaxRetryRequests: b.config.MaxRetries,
			},
		},
	); err != nil {
		return nil, errors.Wrapf(err, "cannot download blob, address: %s", blobURL.BlobURL)
	}

	return ioutil.NopCloser(bytes.NewReader(destBuffer)), nil
}

// Get returns a reader for the given object name.
func (b *Bucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	return b.getBlobReader(ctx, name, 0, blob.CountToEnd)
}

// GetRange returns a new range reader for the given object name and range.
func (b *Bucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	return b.getBlobReader(ctx, name, off, length)
}

// Attributes returns information about the specified object.
func (b *Bucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	blobURL, err := getBlobURL(ctx, *b.config, name)
	if err != nil {
		return objstore.ObjectAttributes{}, errors.Wrapf(err, "cannot get Azure blob URL, blob: %s", name)
	}

	var props *blob.BlobGetPropertiesResponse
	props, err = blobURL.GetProperties(ctx, blob.BlobAccessConditions{})
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	return objstore.ObjectAttributes{
		Size:         props.ContentLength(),
		LastModified: props.LastModified(),
	}, nil
}

// Exists checks if the given object exists.
func (b *Bucket) Exists(ctx context.Context, name string) (bool, error) {
	level.Debug(b.logger).Log("msg", "check if blob exists", "blob", name)
	blobURL, err := getBlobURL(ctx, *b.config, name)
	if err != nil {
		return false, errors.Wrapf(err, "cannot get Azure blob URL, address: %s", name)
	}

	if _, err = blobURL.GetProperties(ctx, blob.BlobAccessConditions{}); err != nil {
		if b.IsObjNotFoundErr(err) {
			return false, nil
		}
		return false, errors.Wrapf(err, "cannot get properties for Azure blob, address: %s", name)
	}

	return true, nil
}

// Upload the contents of the reader as an object into the bucket.
func (b *Bucket) Upload(ctx context.Context, name string, r io.Reader) error {
	level.Debug(b.logger).Log("msg", "Uploading blob", "blob", name)
	blobURL, err := getBlobURL(ctx, *b.config, name)
	if err != nil {
		return errors.Wrapf(err, "cannot get Azure blob URL, address: %s", name)
	}
	if _, err = blob.UploadStreamToBlockBlob(ctx, r, blobURL,
		blob.UploadStreamToBlockBlobOptions{
			BufferSize: 3 * 1024 * 1024,
			MaxBuffers: 4,
		},
	); err != nil {
		return errors.Wrapf(err, "cannot upload Azure blob, address: %s", name)
	}
	return nil
}

// Delete removes the object with the given name.
func (b *Bucket) Delete(ctx context.Context, name string) error {
	level.Debug(b.logger).Log("msg", "Deleting blob", "blob", name)
	blobURL, err := getBlobURL(ctx, *b.config, name)
	if err != nil {
		return errors.Wrapf(err, "cannot get Azure blob URL, address: %s", name)
	}

	if _, err = blobURL.Delete(ctx, blob.DeleteSnapshotsOptionInclude, blob.BlobAccessConditions{}); err != nil {
		return errors.Wrapf(err, "error deleting blob, address: %s", name)
	}
	return nil
}

// Name returns Azure container name.
func (b *Bucket) Name() string {
	return b.config.ContainerName
}

// NewTestBucket creates test bkt client that before returning creates temporary bucket.
// In a close function it empties and deletes the bucket.
func NewTestBucket(t testing.TB, component string) (objstore.Bucket, func(), error) {
	t.Log("Using test Azure bucket.")

	conf := &Config{
		StorageAccountName: os.Getenv("AZURE_STORAGE_ACCOUNT"),
		StorageAccountKey:  os.Getenv("AZURE_STORAGE_ACCESS_KEY"),
		ContainerName:      objstore.CreateTemporaryTestBucketName(t),
	}

	bc, err := yaml.Marshal(conf)
	if err != nil {
		return nil, nil, err
	}

	ctx := context.Background()

	bkt, err := NewBucket(log.NewNopLogger(), bc, component)
	if err != nil {
		t.Errorf("Cannot create Azure storage container:")
		return nil, nil, err
	}

	return bkt, func() {
		objstore.EmptyBucket(t, ctx, bkt)
		err = bkt.Delete(ctx, conf.ContainerName)
		if err != nil {
			t.Logf("deleting bucket failed: %s", err)
		}
	}, nil
}

// Close bucket.
func (b *Bucket) Close() error {
	return nil
}
