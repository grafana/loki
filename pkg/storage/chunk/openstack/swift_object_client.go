package openstack

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"io/ioutil"

	cortex_swift "github.com/cortexproject/cortex/pkg/storage/bucket/swift"
	"github.com/grafana/dskit/dslog"
	"github.com/ncw/swift"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/chunk"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type SwiftObjectClient struct {
	conn *swift.Connection
	cfg  SwiftConfig
}

// SwiftConfig is config for the Swift Chunk Client.
type SwiftConfig struct {
	cortex_swift.Config `yaml:",inline"`
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
	cfg.Config.RegisterFlagsWithPrefix(prefix, f)
}

// NewSwiftObjectClient makes a new chunk.Client that writes chunks to OpenStack Swift.
func NewSwiftObjectClient(cfg SwiftConfig) (*SwiftObjectClient, error) {
	dslog.WarnExperimentalUse("OpenStack Swift Storage", util_log.Logger)

	// Create a connection
	c := &swift.Connection{
		AuthVersion:    cfg.AuthVersion,
		AuthUrl:        cfg.AuthURL,
		ApiKey:         cfg.Password,
		UserName:       cfg.Username,
		UserId:         cfg.UserID,
		Retries:        cfg.MaxRetries,
		ConnectTimeout: cfg.ConnectTimeout,
		Timeout:        cfg.RequestTimeout,
		TenantId:       cfg.ProjectID,
		Tenant:         cfg.ProjectName,
		TenantDomain:   cfg.ProjectDomainName,
		TenantDomainId: cfg.ProjectDomainID,
		Domain:         cfg.DomainName,
		DomainId:       cfg.DomainID,
		Region:         cfg.RegionName,
	}

	switch {
	case cfg.UserDomainName != "":
		c.Domain = cfg.UserDomainName
	case cfg.UserDomainID != "":
		c.DomainId = cfg.UserDomainID
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
		conn: c,
		cfg:  cfg,
	}, nil
}

func (s *SwiftObjectClient) Stop() {
	s.conn.UnAuthenticate()
}

// GetObject returns a reader for the specified object key from the configured swift container.
func (s *SwiftObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	var buf bytes.Buffer
	_, err := s.conn.ObjectGet(s.cfg.ContainerName, objectKey, &buf, false, nil)
	if err != nil {
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
func (s *SwiftObjectClient) List(ctx context.Context, prefix, delimiter string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	if len(delimiter) > 1 {
		return nil, nil, fmt.Errorf("delimiter must be a single character but was %s", delimiter)
	}

	opts := &swift.ObjectsOpts{
		Prefix: prefix,
	}
	if len(delimiter) > 0 {
		opts.Delimiter = []rune(delimiter)[0]
	}

	objs, err := s.conn.Objects(s.cfg.ContainerName, opts)
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

// DeleteObject deletes the specified object key from the configured Swift container.
func (s *SwiftObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return s.conn.ObjectDelete(s.cfg.ContainerName, objectKey)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *SwiftObjectClient) IsObjectNotFoundErr(err error) bool {
	return errors.Is(err, swift.ObjectNotFound)
}
