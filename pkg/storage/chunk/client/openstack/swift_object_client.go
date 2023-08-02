package openstack

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ncw/swift"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	bucket_swift "github.com/grafana/loki/pkg/storage/bucket/swift"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/pkg/util/log"
)

var defaultTransport http.RoundTripper = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	MaxIdleConnsPerHost:   200,
	MaxIdleConns:          200,
	ExpectContinueTimeout: 5 * time.Second,
}

type SwiftObjectClient struct {
	conn        *swift.Connection
	hedgingConn *swift.Connection
	cfg         SwiftConfig
}

// SwiftConfig is config for the Swift Chunk Client.
type SwiftConfig struct {
	bucket_swift.Config `yaml:",inline"`
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
func NewSwiftObjectClient(cfg SwiftConfig, hedgingCfg hedging.Config) (*SwiftObjectClient, error) {
	log.WarnExperimentalUse("OpenStack Swift Storage", log.Logger)

	c, err := createConnection(cfg, hedgingCfg, false)
	if err != nil {
		return nil, err
	}
	// Ensure the container is created, no error is returned if it already exists.
	if err := c.ContainerCreate(cfg.ContainerName, nil); err != nil {
		return nil, err
	}
	hedging, err := createConnection(cfg, hedgingCfg, true)
	if err != nil {
		return nil, err
	}
	return &SwiftObjectClient{
		conn:        c,
		hedgingConn: hedging,
		cfg:         cfg,
	}, nil
}

func createConnection(cfg SwiftConfig, hedgingCfg hedging.Config, hedging bool) (*swift.Connection, error) {
	// Create a connection
	c := &swift.Connection{
		AuthVersion:    cfg.AuthVersion,
		AuthUrl:        cfg.AuthURL,
		Internal:       cfg.Internal,
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
		Transport:      defaultTransport,
	}

	switch {
	case cfg.UserDomainName != "":
		c.Domain = cfg.UserDomainName
	case cfg.UserDomainID != "":
		c.DomainId = cfg.UserDomainID
	}
	if hedging {
		var err error
		c.Transport, err = hedgingCfg.RoundTripperWithRegisterer(c.Transport, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
	}

	err := c.Authenticate()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (s *SwiftObjectClient) Stop() {
	s.conn.UnAuthenticate()
	s.hedgingConn.UnAuthenticate()
}

// GetObject returns a reader and the size for the specified object key from the configured swift container.
func (s *SwiftObjectClient) GetObject(_ context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var buf bytes.Buffer
	_, err := s.hedgingConn.ObjectGet(s.cfg.ContainerName, objectKey, &buf, false, nil)
	if err != nil {
		return nil, 0, err
	}

	return io.NopCloser(&buf), int64(buf.Len()), nil
}

// PutObject puts the specified bytes into the configured Swift container at the provided key
func (s *SwiftObjectClient) PutObject(_ context.Context, objectKey string, object io.ReadSeeker) error {
	_, err := s.conn.ObjectPut(s.cfg.ContainerName, objectKey, object, false, "", "", nil)
	return err
}

// List only objects from the store non-recursively
func (s *SwiftObjectClient) List(_ context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	if len(delimiter) > 1 {
		return nil, nil, fmt.Errorf("delimiter must be a single character but was %s", delimiter)
	}

	opts := &swift.ObjectsOpts{
		Prefix: prefix,
	}
	if len(delimiter) > 0 {
		opts.Delimiter = []rune(delimiter)[0]
	}

	objs, err := s.conn.ObjectsAll(s.cfg.ContainerName, opts)
	if err != nil {
		return nil, nil, err
	}

	var storageObjects []client.StorageObject
	var storagePrefixes []client.StorageCommonPrefix

	for _, obj := range objs {
		// based on the docs when subdir is set, it means it's a pseudo directory.
		// see https://docs.openstack.org/swift/latest/api/pseudo-hierarchical-folders-directories.html
		if obj.SubDir != "" {
			storagePrefixes = append(storagePrefixes, client.StorageCommonPrefix(obj.SubDir))
			continue
		}

		storageObjects = append(storageObjects, client.StorageObject{
			Key:        obj.Name,
			ModifiedAt: obj.LastModified,
		})
	}

	return storageObjects, storagePrefixes, nil
}

// DeleteObject deletes the specified object key from the configured Swift container.
func (s *SwiftObjectClient) DeleteObject(_ context.Context, objectKey string) error {
	return s.conn.ObjectDelete(s.cfg.ContainerName, objectKey)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *SwiftObjectClient) IsObjectNotFoundErr(err error) bool {
	return errors.Is(err, swift.ObjectNotFound)
}

// TODO(dannyk): implement for client
func (s *SwiftObjectClient) IsRetryableErr(err error) bool { return false }
