// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

// Package swift implements common object storage abstractions against OpenStack swift APIs.
package swift

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/go-kit/kit/log"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/gophercloud/openstack/objectstorage/v1/containers"
	"github.com/gophercloud/gophercloud/openstack/objectstorage/v1/objects"
	"github.com/gophercloud/gophercloud/pagination"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/thanos/pkg/objstore"
)

// DirDelim is the delimiter used to model a directory structure in an object store bucket.
const DirDelim = "/"

type SwiftConfig struct {
	AuthUrl           string `yaml:"auth_url"`
	Username          string `yaml:"username"`
	UserDomainName    string `yaml:"user_domain_name"`
	UserDomainID      string `yaml:"user_domain_id"`
	UserId            string `yaml:"user_id"`
	Password          string `yaml:"password"`
	DomainId          string `yaml:"domain_id"`
	DomainName        string `yaml:"domain_name"`
	ProjectID         string `yaml:"project_id"`
	ProjectName       string `yaml:"project_name"`
	ProjectDomainID   string `yaml:"project_domain_id"`
	ProjectDomainName string `yaml:"project_domain_name"`
	RegionName        string `yaml:"region_name"`
	ContainerName     string `yaml:"container_name"`
}

type Container struct {
	logger log.Logger
	client *gophercloud.ServiceClient
	name   string
}

func NewContainer(logger log.Logger, conf []byte) (*Container, error) {
	sc, err := parseConfig(conf)
	if err != nil {
		return nil, err
	}

	provider, err := openstack.AuthenticatedClient(authOptsFromConfig(sc))
	if err != nil {
		return nil, err
	}

	client, err := openstack.NewObjectStorageV1(provider, gophercloud.EndpointOpts{
		Region: sc.RegionName,
	})
	if err != nil {
		return nil, err
	}

	return &Container{
		logger: logger,
		client: client,
		name:   sc.ContainerName,
	}, nil
}

// Name returns the container name for swift.
func (c *Container) Name() string {
	return c.name
}

// Iter calls f for each entry in the given directory. The argument to f is the full
// object name including the prefix of the inspected directory.
func (c *Container) Iter(ctx context.Context, dir string, f func(string) error) error {
	// Ensure the object name actually ends with a dir suffix. Otherwise we'll just iterate the
	// object itself as one prefix item.
	if dir != "" {
		dir = strings.TrimSuffix(dir, DirDelim) + DirDelim
	}

	options := &objects.ListOpts{Full: true, Prefix: dir, Delimiter: DirDelim}
	return objects.List(c.client, c.name, options).EachPage(func(page pagination.Page) (bool, error) {
		objectNames, err := objects.ExtractNames(page)
		if err != nil {
			return false, err
		}
		for _, objectName := range objectNames {
			if err := f(objectName); err != nil {
				return false, err
			}
		}

		return true, nil
	})
}

// Get returns a reader for the given object name.
func (c *Container) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if name == "" {
		return nil, errors.New("error, empty container name passed")
	}
	response := objects.Download(c.client, c.name, name, nil)
	return response.Body, response.Err
}

// GetRange returns a new range reader for the given object name and range.
func (c *Container) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	lowerLimit := ""
	upperLimit := ""
	if off >= 0 {
		lowerLimit = fmt.Sprintf("%d", off)
	}
	if length > 0 {
		upperLimit = fmt.Sprintf("%d", off+length-1)
	}
	options := objects.DownloadOpts{
		Newest: true,
		Range:  fmt.Sprintf("bytes=%s-%s", lowerLimit, upperLimit),
	}
	response := objects.Download(c.client, c.name, name, options)
	return response.Body, response.Err
}

// Attributes returns information about the specified object.
func (c *Container) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	response := objects.Get(c.client, c.name, name, nil)
	headers, err := response.Extract()
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	return objstore.ObjectAttributes{
		Size:         headers.ContentLength,
		LastModified: headers.LastModified,
	}, nil
}

// Exists checks if the given object exists.
func (c *Container) Exists(ctx context.Context, name string) (bool, error) {
	err := objects.Get(c.client, c.name, name, nil).Err
	if err == nil {
		return true, nil
	}

	if _, ok := err.(gophercloud.ErrDefault404); ok {
		return false, nil
	}

	return false, err
}

// IsObjNotFoundErr returns true if error means that object is not found. Relevant to Get operations.
func (c *Container) IsObjNotFoundErr(err error) bool {
	_, ok := err.(gophercloud.ErrDefault404)
	return ok
}

// Upload writes the contents of the reader as an object into the container.
func (c *Container) Upload(ctx context.Context, name string, r io.Reader) error {
	options := &objects.CreateOpts{Content: r}
	res := objects.Create(c.client, c.name, name, options)
	return res.Err
}

// Delete removes the object with the given name.
func (c *Container) Delete(ctx context.Context, name string) error {
	return objects.Delete(c.client, c.name, name, nil).Err
}

func (*Container) Close() error {
	// Nothing to close.
	return nil
}

func parseConfig(conf []byte) (*SwiftConfig, error) {
	var sc SwiftConfig
	err := yaml.UnmarshalStrict(conf, &sc)
	return &sc, err
}

func authOptsFromConfig(sc *SwiftConfig) gophercloud.AuthOptions {
	authOpts := gophercloud.AuthOptions{
		IdentityEndpoint: sc.AuthUrl,
		Username:         sc.Username,
		UserID:           sc.UserId,
		Password:         sc.Password,
		DomainID:         sc.DomainId,
		DomainName:       sc.DomainName,
		TenantID:         sc.ProjectID,
		TenantName:       sc.ProjectName,

		// Allow Gophercloud to re-authenticate automatically.
		AllowReauth: true,
	}

	// Support for cross-domain scoping (user in different domain than project).
	// If a userDomainName or userDomainID is given, the user is scoped to this domain.
	switch {
	case sc.UserDomainName != "":
		authOpts.DomainName = sc.UserDomainName
	case sc.UserDomainID != "":
		authOpts.DomainID = sc.UserDomainID
	}

	// A token can be scoped to a domain or project.
	// The project can be in another domain than the user, which is indicated by setting either projectDomainName or projectDomainID.
	switch {
	case sc.ProjectDomainName != "":
		authOpts.Scope = &gophercloud.AuthScope{
			DomainName: sc.ProjectDomainName,
		}
	case sc.ProjectDomainID != "":
		authOpts.Scope = &gophercloud.AuthScope{
			DomainID: sc.ProjectDomainID,
		}
	}
	if authOpts.Scope != nil {
		switch {
		case sc.ProjectName != "":
			authOpts.Scope.ProjectName = sc.ProjectName
		case sc.ProjectID != "":
			authOpts.Scope.ProjectID = sc.ProjectID
		}
	}
	return authOpts
}

func (c *Container) createContainer(name string) error {
	return containers.Create(c.client, name, nil).Err
}

func (c *Container) deleteContainer(name string) error {
	return containers.Delete(c.client, name).Err
}

func configFromEnv() SwiftConfig {
	c := SwiftConfig{
		AuthUrl:           os.Getenv("OS_AUTH_URL"),
		Username:          os.Getenv("OS_USERNAME"),
		Password:          os.Getenv("OS_PASSWORD"),
		RegionName:        os.Getenv("OS_REGION_NAME"),
		ContainerName:     os.Getenv("OS_CONTAINER_NAME"),
		ProjectID:         os.Getenv("OS_PROJECT_ID"),
		ProjectName:       os.Getenv("OS_PROJECT_NAME"),
		UserDomainID:      os.Getenv("OS_USER_DOMAIN_ID"),
		UserDomainName:    os.Getenv("OS_USER_DOMAIN_NAME"),
		ProjectDomainID:   os.Getenv("OS_PROJECT_DOMAIN_ID"),
		ProjectDomainName: os.Getenv("OS_PROJECT_DOMAIN_NAME"),
	}

	return c
}

// validateForTests checks to see the config options for tests are set.
func validateForTests(conf SwiftConfig) error {
	if conf.AuthUrl == "" ||
		conf.Username == "" ||
		conf.Password == "" ||
		(conf.ProjectName == "" && conf.ProjectID == "") ||
		conf.RegionName == "" {
		return errors.New("insufficient swift test configuration information")
	}
	return nil
}

// NewTestContainer creates test objStore client that before returning creates temporary container.
// In a close function it empties and deletes the container.
func NewTestContainer(t testing.TB) (objstore.Bucket, func(), error) {
	config := configFromEnv()
	if err := validateForTests(config); err != nil {
		return nil, nil, err
	}
	containerConfig, err := yaml.Marshal(config)
	if err != nil {
		return nil, nil, err
	}

	c, err := NewContainer(log.NewNopLogger(), containerConfig)
	if err != nil {
		return nil, nil, err
	}

	if config.ContainerName != "" {
		if os.Getenv("THANOS_ALLOW_EXISTING_BUCKET_USE") == "" {
			return nil, nil, errors.New("OS_CONTAINER_NAME is defined. Normally this tests will create temporary container " +
				"and delete it after test. Unset OS_CONTAINER_NAME env variable to use default logic. If you really want to run " +
				"tests against provided (NOT USED!) container, set THANOS_ALLOW_EXISTING_BUCKET_USE=true. WARNING: That container " +
				"needs to be manually cleared. This means that it is only useful to run one test in a time. This is due " +
				"to safety (accidentally pointing prod container for test) as well as swift not being fully strong consistent.")
		}

		if err := c.Iter(context.Background(), "", func(f string) error {
			return errors.Errorf("container %s is not empty", config.ContainerName)
		}); err != nil {
			return nil, nil, errors.Wrapf(err, "swift check container %s", config.ContainerName)
		}

		t.Log("WARNING. Reusing", config.ContainerName, "container for Swift tests. Manual cleanup afterwards is required")
		return c, func() {}, nil
	}

	tmpContainerName := objstore.CreateTemporaryTestBucketName(t)

	if err := c.createContainer(tmpContainerName); err != nil {
		return nil, nil, err
	}

	c.name = tmpContainerName
	t.Log("created temporary container for swift tests with name", tmpContainerName)

	return c, func() {
		objstore.EmptyBucket(t, context.Background(), c)
		if err := c.deleteContainer(tmpContainerName); err != nil {
			t.Logf("deleting container %s failed: %s", tmpContainerName, err)
		}
	}, nil
}
