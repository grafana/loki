package openstack

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/ncw/swift"
	thanos "github.com/thanos-io/thanos/pkg/objstore/swift"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"
)

type SwiftObjectClient struct {
	conn      *swift.Connection
	cfg       SwiftConfig
	delimiter rune
}

// SwiftConfig is config for the Swift Chunk Client.
type SwiftConfig struct {
	thanos.SwiftConfig `yaml:",inline"`
}

// RegisterFlags registers flags.
func (cfg *SwiftConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", f)
}

// Validate config and returns error on failure
func (cfg *SwiftConfig) Validate() error {
	return nil
}

// RegisterFlagsWithPrefix registers flags with prefix.
func (cfg *SwiftConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.ContainerName, prefix+"swift.container-name", "cortex", "Name of the Swift container to put chunks in.")
	f.StringVar(&cfg.DomainName, prefix+"swift.domain-name", "", "Openstack user's domain name.")
	f.StringVar(&cfg.DomainId, prefix+"swift.domain-id", "", "Openstack user's domain id.")
	f.StringVar(&cfg.UserDomainName, prefix+"swift.user-domain-name", "", "Openstack user's domain name.")
	f.StringVar(&cfg.UserDomainID, prefix+"swift.user-domain-id", "", "Openstack user's domain id.")
	f.StringVar(&cfg.Username, prefix+"swift.username", "", "Openstack username for the api.")
	f.StringVar(&cfg.UserId, prefix+"swift.user-id", "", "Openstack userid for the api.")
	f.StringVar(&cfg.Password, prefix+"swift.password", "", "Openstack api key.")
	f.StringVar(&cfg.AuthUrl, prefix+"swift.auth-url", "", "Openstack authentication URL.")
	f.StringVar(&cfg.RegionName, prefix+"swift.region-name", "", "Openstack Region to use eg LON, ORD - default is use first region (v2,v3 auth only)")
	f.StringVar(&cfg.ProjectName, prefix+"swift.project-name", "", "Openstack project name (v2,v3 auth only).")
	f.StringVar(&cfg.ProjectID, prefix+"swift.project-id", "", "Openstack project id (v2,v3 auth only).")
	f.StringVar(&cfg.ProjectDomainName, prefix+"swift.project-domain-name", "", "Name of the project's domain (v3 auth only), only needed if it differs from the user domain.")
	f.StringVar(&cfg.ProjectDomainID, prefix+"swift.project-domain-id", "", "Id of the project's domain (v3 auth only), only needed if it differs the from user domain.")
}

// NewSwiftObjectClient makes a new chunk.Client that writes chunks to OpenStack Swift.
func NewSwiftObjectClient(cfg SwiftConfig, delimiter string) (*SwiftObjectClient, error) {
	util.WarnExperimentalUse("OpenStack Swift Storage")

	// Create a connection
	c := &swift.Connection{
		AuthUrl:  cfg.AuthUrl,
		ApiKey:   cfg.Password,
		UserName: cfg.Username,
		UserId:   cfg.UserId,

		TenantId:       cfg.ProjectID,
		Tenant:         cfg.ProjectName,
		TenantDomain:   cfg.ProjectDomainName,
		TenantDomainId: cfg.ProjectDomainID,

		Domain:   cfg.DomainName,
		DomainId: cfg.DomainId,

		Region: cfg.RegionName,
	}

	switch {
	case cfg.UserDomainName != "":
		c.Domain = cfg.UserDomainName
	case cfg.UserDomainID != "":
		c.DomainId = cfg.UserDomainID
	}

	if len(delimiter) > 1 {
		return nil, fmt.Errorf("delimiter must be a single character but was %s", delimiter)
	}
	var delim rune
	if len(delimiter) != 0 {
		delim = []rune(delimiter)[0]
	}

	// Authenticate
	err := c.Authenticate()
	if err != nil {
		return nil, err
	}

	// Ensure the container is created, no error is returned if it already exists.
	if err := c.ContainerCreate(cfg.ContainerName, nil); err != nil {
		return nil, err
	}

	return &SwiftObjectClient{
		conn:      c,
		cfg:       cfg,
		delimiter: delim,
	}, nil
}

func (s *SwiftObjectClient) Stop() {
	s.conn.UnAuthenticate()
}

// GetObject returns a reader for the specified object key from the configured swift container. If the
// key does not exist a generic chunk.ErrStorageObjectNotFound error is returned.
func (s *SwiftObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	var buf bytes.Buffer
	_, err := s.conn.ObjectGet(s.cfg.ContainerName, objectKey, &buf, false, nil)
	if err != nil {
		if err == swift.ObjectNotFound {
			return nil, chunk.ErrStorageObjectNotFound
		}
		return nil, err
	}

	return ioutil.NopCloser(&buf), nil
}

// PutObject puts the specified bytes into the configured Swift container at the provided key
func (s *SwiftObjectClient) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	_, err := s.conn.ObjectPut(s.cfg.ContainerName, objectKey, object, false, "", "", nil)
	return err
}

// List only objects from the store non-recursively
func (s *SwiftObjectClient) List(ctx context.Context, prefix string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	objs, err := s.conn.Objects(s.cfg.ContainerName, &swift.ObjectsOpts{
		Prefix:    prefix,
		Delimiter: s.delimiter,
	})
	if err != nil {
		return nil, nil, err
	}

	var storageObjects []chunk.StorageObject
	var storagePrefixes []chunk.StorageCommonPrefix

	for _, obj := range objs {
		// based on the docs when subdir is set, it means it's a pseudo directory.
		// see https://docs.openstack.org/swift/latest/api/pseudo-hierarchical-folders-directories.html
		if obj.SubDir != "" {
			storagePrefixes = append(storagePrefixes, chunk.StorageCommonPrefix(obj.SubDir))
			continue
		}

		storageObjects = append(storageObjects, chunk.StorageObject{
			Key:        obj.Name,
			ModifiedAt: obj.LastModified,
		})
	}

	return storageObjects, storagePrefixes, nil
}

// DeleteObject deletes the specified object key from the configured Swift container. If the
// key does not exist a generic chunk.ErrStorageObjectNotFound error is returned.
func (s *SwiftObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	err := s.conn.ObjectDelete(s.cfg.ContainerName, objectKey)
	if err == nil {
		return nil
	}
	if err == swift.ObjectNotFound {
		return chunk.ErrStorageObjectNotFound
	}
	return err
}

func (s *SwiftObjectClient) PathSeparator() string {
	return string(s.delimiter)
}
