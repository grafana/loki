package openstack

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"time"

	swift "github.com/ncw/swift/v2"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	bucket_swift "github.com/grafana/loki/v3/pkg/storage/bucket/swift"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/util/log"
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
	if err := c.ContainerCreate(context.Background(), cfg.Config.ContainerName, nil); err != nil {
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
		AuthVersion:    cfg.Config.AuthVersion,
		AuthUrl:        cfg.Config.AuthURL,
		Internal:       cfg.Config.Internal,
		ApiKey:         cfg.Config.Password,
		UserName:       cfg.Config.Username,
		UserId:         cfg.Config.UserID,
		Retries:        cfg.Config.MaxRetries,
		ConnectTimeout: cfg.Config.ConnectTimeout,
		Timeout:        cfg.Config.RequestTimeout,
		TenantId:       cfg.Config.ProjectID,
		Tenant:         cfg.Config.ProjectName,
		TenantDomain:   cfg.Config.ProjectDomainName,
		TenantDomainId: cfg.Config.ProjectDomainID,
		Domain:         cfg.Config.DomainName,
		DomainId:       cfg.Config.DomainID,
		Region:         cfg.Config.RegionName,
		Transport:      defaultTransport,
	}

	switch {
	case cfg.Config.UserDomainName != "":
		c.Domain = cfg.Config.UserDomainName
	case cfg.Config.UserDomainID != "":
		c.DomainId = cfg.Config.UserDomainID
	}
	if hedging {
		var err error
		c.Transport, err = hedgingCfg.RoundTripperWithRegisterer(c.Transport, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
	}

	err := c.Authenticate(context.TODO())
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (s *SwiftObjectClient) Stop() {
	s.conn.UnAuthenticate()
	s.hedgingConn.UnAuthenticate()
}

func (s *SwiftObjectClient) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	_, _, err := s.hedgingConn.Object(ctx, s.cfg.Config.ContainerName, objectKey)
	if err != nil {
		return false, err
	}

	return true, nil
}

// GetObject returns a reader and the size for the specified object key from the configured swift container.
func (s *SwiftObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var buf bytes.Buffer
	_, err := s.hedgingConn.ObjectGet(ctx, s.cfg.Config.ContainerName, objectKey, &buf, false, nil)
	if err != nil {
		return nil, 0, err
	}

	return io.NopCloser(&buf), int64(buf.Len()), nil
}

// GetObject returns a reader and the size for the specified object key from the configured swift container.
func (s *SwiftObjectClient) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	var buf bytes.Buffer
	h := swift.Headers{
		"Range": fmt.Sprintf("bytes=%d-%d", offset, offset+length-1),
	}
	_, err := s.hedgingConn.ObjectGet(ctx, s.cfg.Config.ContainerName, objectKey, &buf, false, h)
	if err != nil {
		return nil, err
	}

	return io.NopCloser(&buf), nil
}

// PutObject puts the specified bytes into the configured Swift container at the provided key
func (s *SwiftObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	_, err := s.conn.ObjectPut(ctx, s.cfg.Config.ContainerName, objectKey, object, false, "", "", nil)
	return err
}

// List only objects from the store non-recursively
func (s *SwiftObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	if len(delimiter) > 1 {
		return nil, nil, fmt.Errorf("delimiter must be a single character but was %s", delimiter)
	}

	opts := &swift.ObjectsOpts{
		Prefix: prefix,
	}
	if len(delimiter) > 0 {
		opts.Delimiter = []rune(delimiter)[0]
	}

	objs, err := s.conn.ObjectsAll(ctx, s.cfg.Config.ContainerName, opts)
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
func (s *SwiftObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	return s.conn.ObjectDelete(ctx, s.cfg.Config.ContainerName, objectKey)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *SwiftObjectClient) IsObjectNotFoundErr(err error) bool {
	return errors.Is(err, swift.ObjectNotFound)
}

// TODO(dannyk): implement for client
func (s *SwiftObjectClient) IsRetryableErr(error) bool { return false }
