// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

// Package swift implements common object storage abstractions against OpenStack swift APIs.
package swift

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/efficientgo/core/errcapture"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/ncw/swift"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/exthttp"
	"gopkg.in/yaml.v2"
)

const (
	// DirDelim is the delimiter used to model a directory structure in an object store bucket.
	DirDelim = '/'
	// SegmentsDir represent name of the directory in bucket, where to store file parts of SLO and DLO.
	SegmentsDir = "segments/"
)

var DefaultConfig = Config{
	AuthVersion:    0, // Means autodetect of the auth API version by the library.
	ChunkSize:      1024 * 1024 * 1024,
	Retries:        3,
	ConnectTimeout: model.Duration(10 * time.Second),
	Timeout:        model.Duration(5 * time.Minute),
	HTTPConfig:     exthttp.DefaultHTTPConfig,
}

type Config struct {
	AuthVersion                 int                `yaml:"auth_version"`
	AuthUrl                     string             `yaml:"auth_url"`
	Username                    string             `yaml:"username"`
	UserDomainName              string             `yaml:"user_domain_name"`
	UserDomainID                string             `yaml:"user_domain_id"`
	UserId                      string             `yaml:"user_id"`
	Password                    string             `yaml:"password"`
	DomainId                    string             `yaml:"domain_id"`
	DomainName                  string             `yaml:"domain_name"`
	ApplicationCredentialID     string             `yaml:"application_credential_id"`
	ApplicationCredentialName   string             `yaml:"application_credential_name"`
	ApplicationCredentialSecret string             `yaml:"application_credential_secret"`
	ProjectID                   string             `yaml:"project_id"`
	ProjectName                 string             `yaml:"project_name"`
	ProjectDomainID             string             `yaml:"project_domain_id"`
	ProjectDomainName           string             `yaml:"project_domain_name"`
	RegionName                  string             `yaml:"region_name"`
	ContainerName               string             `yaml:"container_name"`
	ChunkSize                   int64              `yaml:"large_object_chunk_size"`
	SegmentContainerName        string             `yaml:"large_object_segments_container_name"`
	Retries                     int                `yaml:"retries"`
	ConnectTimeout              model.Duration     `yaml:"connect_timeout"`
	Timeout                     model.Duration     `yaml:"timeout"`
	UseDynamicLargeObjects      bool               `yaml:"use_dynamic_large_objects"`
	HTTPConfig                  exthttp.HTTPConfig `yaml:"http_config"`
}

func parseConfig(conf []byte) (*Config, error) {
	sc := DefaultConfig
	err := yaml.UnmarshalStrict(conf, &sc)
	return &sc, err
}

func configFromEnv() (*Config, error) {
	c := swift.Connection{}
	if err := c.ApplyEnvironment(); err != nil {
		return nil, err
	}

	config := Config{
		AuthVersion:                 c.AuthVersion,
		AuthUrl:                     c.AuthUrl,
		Username:                    c.UserName,
		UserId:                      c.UserId,
		Password:                    c.ApiKey,
		DomainId:                    c.DomainId,
		DomainName:                  c.Domain,
		ApplicationCredentialID:     c.ApplicationCredentialId,
		ApplicationCredentialName:   c.ApplicationCredentialName,
		ApplicationCredentialSecret: c.ApplicationCredentialSecret,
		ProjectID:                   c.TenantId,
		ProjectName:                 c.Tenant,
		ProjectDomainID:             c.TenantDomainId,
		ProjectDomainName:           c.TenantDomain,
		RegionName:                  c.Region,
		ContainerName:               os.Getenv("OS_CONTAINER_NAME"),
		ChunkSize:                   DefaultConfig.ChunkSize,
		SegmentContainerName:        os.Getenv("SWIFT_SEGMENTS_CONTAINER_NAME"),
		Retries:                     c.Retries,
		ConnectTimeout:              model.Duration(c.ConnectTimeout),
		Timeout:                     model.Duration(c.Timeout),
		UseDynamicLargeObjects:      false,
		HTTPConfig:                  DefaultConfig.HTTPConfig,
	}
	if os.Getenv("SWIFT_CHUNK_SIZE") != "" {
		var err error
		config.ChunkSize, err = strconv.ParseInt(os.Getenv("SWIFT_CHUNK_SIZE"), 10, 64)
		if err != nil {
			return nil, errors.Wrap(err, "parsing chunk size")
		}
	}
	if strings.ToLower(os.Getenv("SWIFT_USE_DYNAMIC_LARGE_OBJECTS")) == "true" {
		config.UseDynamicLargeObjects = true
	}
	return &config, nil
}

func connectionFromConfig(sc *Config, rt http.RoundTripper) *swift.Connection {
	connection := swift.Connection{
		AuthVersion:                 sc.AuthVersion,
		AuthUrl:                     sc.AuthUrl,
		UserName:                    sc.Username,
		UserId:                      sc.UserId,
		ApiKey:                      sc.Password,
		DomainId:                    sc.DomainId,
		Domain:                      sc.DomainName,
		ApplicationCredentialId:     sc.ApplicationCredentialID,
		ApplicationCredentialName:   sc.ApplicationCredentialName,
		ApplicationCredentialSecret: sc.ApplicationCredentialSecret,
		TenantId:                    sc.ProjectID,
		Tenant:                      sc.ProjectName,
		TenantDomain:                sc.ProjectDomainName,
		TenantDomainId:              sc.ProjectDomainID,
		Region:                      sc.RegionName,
		Retries:                     sc.Retries,
		ConnectTimeout:              time.Duration(sc.ConnectTimeout),
		Timeout:                     time.Duration(sc.Timeout),
		Transport:                   rt,
	}
	return &connection
}

type Container struct {
	logger                 log.Logger
	name                   string
	connection             *swift.Connection
	chunkSize              int64
	useDynamicLargeObjects bool
	segmentsContainer      string
}

func NewContainer(logger log.Logger, conf []byte) (*Container, error) {
	sc, err := parseConfig(conf)
	if err != nil {
		return nil, errors.Wrap(err, "parse config")
	}
	return NewContainerFromConfig(logger, sc, false)
}

func ensureContainer(connection *swift.Connection, name string, createIfNotExist bool) error {
	if _, _, err := connection.Container(name); err != nil {
		if err != swift.ContainerNotFound {
			return errors.Wrapf(err, "verify container %s", name)
		}
		if !createIfNotExist {
			return fmt.Errorf("unable to find the expected container %s", name)
		}
		if err = connection.ContainerCreate(name, swift.Headers{}); err != nil {
			return errors.Wrapf(err, "create container %s", name)
		}
		return nil
	}
	return nil
}

func NewContainerFromConfig(logger log.Logger, sc *Config, createContainer bool) (*Container, error) {

	// Check if a roundtripper has been set in the config
	// otherwise build the default transport.
	var rt http.RoundTripper
	if sc.HTTPConfig.Transport != nil {
		rt = sc.HTTPConfig.Transport
	} else {
		var err error
		rt, err = exthttp.DefaultTransport(sc.HTTPConfig)
		if err != nil {
			return nil, err
		}
	}

	connection := connectionFromConfig(sc, rt)
	if err := connection.Authenticate(); err != nil {
		return nil, errors.Wrap(err, "authentication")
	}

	if err := ensureContainer(connection, sc.ContainerName, createContainer); err != nil {
		return nil, err
	}
	if sc.SegmentContainerName == "" {
		sc.SegmentContainerName = sc.ContainerName
	} else if err := ensureContainer(connection, sc.SegmentContainerName, createContainer); err != nil {
		return nil, err
	}

	return &Container{
		logger:                 logger,
		name:                   sc.ContainerName,
		connection:             connection,
		chunkSize:              sc.ChunkSize,
		useDynamicLargeObjects: sc.UseDynamicLargeObjects,
		segmentsContainer:      sc.SegmentContainerName,
	}, nil
}

// Name returns the container name for swift.
func (c *Container) Name() string {
	return c.name
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (c *Container) Iter(_ context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	if dir != "" {
		dir = strings.TrimSuffix(dir, string(DirDelim)) + string(DirDelim)
	}

	listOptions := &swift.ObjectsOpts{
		Prefix:    dir,
		Delimiter: DirDelim,
	}
	if objstore.ApplyIterOptions(options...).Recursive {
		listOptions.Delimiter = rune(0)
	}

	return c.connection.ObjectsWalk(c.name, listOptions, func(opts *swift.ObjectsOpts) (interface{}, error) {
		objects, err := c.connection.ObjectNames(c.name, opts)
		if err != nil {
			return objects, errors.Wrap(err, "list object names")
		}
		for _, object := range objects {
			if object == SegmentsDir {
				continue
			}
			if err := f(object); err != nil {
				return objects, errors.Wrap(err, "iteration over objects")
			}
		}
		return objects, nil
	})
}

func (c *Container) get(name string, headers swift.Headers, checkHash bool) (io.ReadCloser, error) {
	if name == "" {
		return nil, errors.New("object name cannot be empty")
	}
	file, _, err := c.connection.ObjectOpen(c.name, name, checkHash, headers)
	if err != nil {
		return nil, errors.Wrap(err, "open object")
	}
	return file, err
}

// Get returns a reader for the given object name.
func (c *Container) Get(_ context.Context, name string) (io.ReadCloser, error) {
	return c.get(name, swift.Headers{}, true)
}

func (c *Container) GetRange(_ context.Context, name string, off, length int64) (io.ReadCloser, error) {
	// Set Range HTTP header, see the docs https://docs.openstack.org/api-ref/object-store/?expanded=show-container-details-and-list-objects-detail,get-object-content-and-metadata-detail#id76.
	bytesRange := fmt.Sprintf("bytes=%d-", off)
	if length != -1 {
		bytesRange = fmt.Sprintf("%s%d", bytesRange, off+length-1)
	}
	return c.get(name, swift.Headers{"Range": bytesRange}, false)
}

// Attributes returns information about the specified object.
func (c *Container) Attributes(_ context.Context, name string) (objstore.ObjectAttributes, error) {
	if name == "" {
		return objstore.ObjectAttributes{}, errors.New("object name cannot be empty")
	}
	info, _, err := c.connection.Object(c.name, name)
	if err != nil {
		return objstore.ObjectAttributes{}, errors.Wrap(err, "get object attributes")
	}
	return objstore.ObjectAttributes{
		Size:         info.Bytes,
		LastModified: info.LastModified,
	}, nil
}

// Exists checks if the given object exists.
func (c *Container) Exists(_ context.Context, name string) (bool, error) {
	found := true
	_, _, err := c.connection.Object(c.name, name)
	if c.IsObjNotFoundErr(err) {
		err = nil
		found = false
	}
	return found, err
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (c *Container) IsObjNotFoundErr(err error) bool {
	return errors.Is(err, swift.ObjectNotFound)
}

// IsAccessDeniedErr returns true if access to object is denied.
func (c *Container) IsAccessDeniedErr(err error) bool {
	return errors.Is(err, swift.Forbidden)
}

// Upload writes the contents of the reader as an object into the container.
func (c *Container) Upload(_ context.Context, name string, r io.Reader) (err error) {
	size, err := objstore.TryToGetSize(r)
	if err != nil {
		level.Warn(c.logger).Log("msg", "could not guess file size, using large object to avoid issues if the file is larger than limit", "name", name, "err", err)
		// Anything higher or equal to chunk size so the SLO is used.
		size = c.chunkSize
	}
	var file io.WriteCloser
	if size >= c.chunkSize {
		opts := swift.LargeObjectOpts{
			Container:        c.name,
			ObjectName:       name,
			ChunkSize:        c.chunkSize,
			SegmentContainer: c.segmentsContainer,
			CheckHash:        true,
		}
		if c.useDynamicLargeObjects {
			if file, err = c.connection.DynamicLargeObjectCreateFile(&opts); err != nil {
				return errors.Wrap(err, "create DLO file")
			}
		} else {
			if file, err = c.connection.StaticLargeObjectCreateFile(&opts); err != nil {
				return errors.Wrap(err, "create SLO file")
			}
		}
	} else {
		if file, err = c.connection.ObjectCreate(c.name, name, true, "", "", swift.Headers{}); err != nil {
			return errors.Wrap(err, "create file")
		}
	}
	defer errcapture.Do(&err, file.Close, "upload object close")
	if _, err := io.Copy(file, r); err != nil {
		return errors.Wrap(err, "uploading object")
	}
	return nil
}

// Delete removes the object with the given name.
func (c *Container) Delete(_ context.Context, name string) error {
	return errors.Wrap(c.connection.LargeObjectDelete(c.name, name), "delete object")
}

func (*Container) Close() error {
	// Nothing to close.
	return nil
}

// NewTestContainer creates test objStore client that before returning creates temporary container.
// In a close function it empties and deletes the container.
func NewTestContainer(t testing.TB) (objstore.Bucket, func(), error) {
	config, err := configFromEnv()
	if err != nil {
		return nil, nil, errors.Wrap(err, "loading config from ENV")
	}
	if config.ContainerName != "" {
		if os.Getenv("THANOS_ALLOW_EXISTING_BUCKET_USE") == "" {
			return nil, nil, errors.New("OS_CONTAINER_NAME is defined. Normally this tests will create temporary container " +
				"and delete it after test. Unset OS_CONTAINER_NAME env variable to use default logic. If you really want to run " +
				"tests against provided (NOT USED!) container, set THANOS_ALLOW_EXISTING_BUCKET_USE=true. WARNING: That container " +
				"needs to be manually cleared. This means that it is only useful to run one test in a time. This is due " +
				"to safety (accidentally pointing prod container for test) as well as swift not being fully strong consistent.")
		}
		c, err := NewContainerFromConfig(log.NewNopLogger(), config, false)
		if err != nil {
			return nil, nil, errors.Wrap(err, "initializing new container")
		}
		if err := c.Iter(context.Background(), "", func(f string) error {
			return errors.Errorf("container %s is not empty", c.Name())
		}); err != nil {
			return nil, nil, errors.Wrapf(err, "check container %s", c.Name())
		}
		t.Log("WARNING. Reusing", c.Name(), "container for Swift tests. Manual cleanup afterwards is required")
		return c, func() {}, nil
	}
	config.ContainerName = objstore.CreateTemporaryTestBucketName(t)
	config.SegmentContainerName = config.ContainerName
	c, err := NewContainerFromConfig(log.NewNopLogger(), config, true)
	if err != nil {
		return nil, nil, errors.Wrap(err, "initializing new container")
	}
	t.Log("created temporary container for swift tests with name", c.Name())

	return c, func() {
		objstore.EmptyBucket(t, context.Background(), c)
		if err := c.connection.ContainerDelete(c.name); err != nil {
			t.Logf("deleting container %s failed: %s", c.Name(), err)
		}
		if err := c.connection.ContainerDelete(c.segmentsContainer); err != nil {
			t.Logf("deleting segments container %s failed: %s", c.segmentsContainer, err)
		}
	}, nil
}
