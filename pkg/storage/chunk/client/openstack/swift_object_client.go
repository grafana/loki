package openstack

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/grafana/dskit/flagext"
	swift "github.com/ncw/swift/v2"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/util/log"
)

// Config stores the http.Client configuration for the storage clients.
type HTTPConfig struct {
	Transport http.RoundTripper `yaml:"-"`
	TLSConfig TLSConfig         `yaml:",inline"`
}

// TLSConfig configures the options for TLS connections.
type TLSConfig struct {
	CAPath string `yaml:"tls_ca_path" category:"advanced"`
}

func defaultTransport(config HTTPConfig) (http.RoundTripper, error) {
	if config.Transport != nil {
		return config.Transport, nil
	}

	tlsConfig := &tls.Config{}
	if len(config.TLSConfig.CAPath) > 0 {
		caPath := config.TLSConfig.CAPath
		data, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("unable to load specified CA cert %s: %s", caPath, err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(data) {
			return nil, fmt.Errorf("unable to use specified CA cert %s", caPath)
		}
		tlsConfig.RootCAs = caCertPool
	}

	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   200,
		ExpectContinueTimeout: 5 * time.Second,
		TLSClientConfig:       tlsConfig,
	}, nil
}

type SwiftObjectClient struct {
	conn        *swift.Connection
	hedgingConn *swift.Connection
	cfg         SwiftConfig
}

// SwiftConfig is config for the Swift Chunk Client.
type SwiftConfig struct {
	AuthVersion       int            `yaml:"auth_version"`
	AuthURL           string         `yaml:"auth_url"`
	Internal          bool           `yaml:"internal"`
	Username          string         `yaml:"username"`
	UserDomainName    string         `yaml:"user_domain_name"`
	UserDomainID      string         `yaml:"user_domain_id"`
	UserID            string         `yaml:"user_id"`
	Password          flagext.Secret `yaml:"password"`
	DomainID          string         `yaml:"domain_id"`
	DomainName        string         `yaml:"domain_name"`
	ProjectID         string         `yaml:"project_id"`
	ProjectName       string         `yaml:"project_name"`
	ProjectDomainID   string         `yaml:"project_domain_id"`
	ProjectDomainName string         `yaml:"project_domain_name"`
	RegionName        string         `yaml:"region_name"`
	ContainerName     string         `yaml:"container_name"`
	MaxRetries        int            `yaml:"max_retries" category:"advanced"`
	ConnectTimeout    time.Duration  `yaml:"connect_timeout" category:"advanced"`
	RequestTimeout    time.Duration  `yaml:"request_timeout" category:"advanced"`
	HTTP              HTTPConfig     `yaml:"http"`
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
	f.IntVar(&cfg.AuthVersion, prefix+"swift.auth-version", 0, "OpenStack Swift authentication API version. 0 to autodetect.")
	f.StringVar(&cfg.AuthURL, prefix+"swift.auth-url", "", "OpenStack Swift authentication URL")
	f.BoolVar(&cfg.Internal, prefix+"swift.internal", false, "Set this to true to use the internal OpenStack Swift endpoint URL")
	f.StringVar(&cfg.Username, prefix+"swift.username", "", "OpenStack Swift username.")
	f.StringVar(&cfg.UserDomainName, prefix+"swift.user-domain-name", "", "OpenStack Swift user's domain name.")
	f.StringVar(&cfg.UserDomainID, prefix+"swift.user-domain-id", "", "OpenStack Swift user's domain ID.")
	f.StringVar(&cfg.UserID, prefix+"swift.user-id", "", "OpenStack Swift user ID.")
	f.Var(&cfg.Password, prefix+"swift.password", "OpenStack Swift API key.")
	f.StringVar(&cfg.DomainID, prefix+"swift.domain-id", "", "OpenStack Swift user's domain ID.")
	f.StringVar(&cfg.DomainName, prefix+"swift.domain-name", "", "OpenStack Swift user's domain name.")
	f.StringVar(&cfg.ProjectID, prefix+"swift.project-id", "", "OpenStack Swift project ID (v2,v3 auth only).")
	f.StringVar(&cfg.ProjectName, prefix+"swift.project-name", "", "OpenStack Swift project name (v2,v3 auth only).")
	f.StringVar(&cfg.ProjectDomainID, prefix+"swift.project-domain-id", "", "ID of the OpenStack Swift project's domain (v3 auth only), only needed if it differs the from user domain.")
	f.StringVar(&cfg.ProjectDomainName, prefix+"swift.project-domain-name", "", "Name of the OpenStack Swift project's domain (v3 auth only), only needed if it differs from the user domain.")
	f.StringVar(&cfg.RegionName, prefix+"swift.region-name", "", "OpenStack Swift Region to use (v2,v3 auth only).")
	f.StringVar(&cfg.ContainerName, prefix+"swift.container-name", "", "Name of the OpenStack Swift container to put chunks in.")
	f.IntVar(&cfg.MaxRetries, prefix+"swift.max-retries", 3, "Max retries on requests error.")
	f.DurationVar(&cfg.ConnectTimeout, prefix+"swift.connect-timeout", 10*time.Second, "Time after which a connection attempt is aborted.")
	f.DurationVar(&cfg.RequestTimeout, prefix+"swift.request-timeout", 5*time.Second, "Time after which an idle request is aborted. The timeout watchdog is reset each time some data is received, so the timeout triggers after X time no data is received on a request.")
	f.StringVar(&cfg.HTTP.TLSConfig.CAPath, prefix+"swift.http.tls-ca-path", "", "Path to the CA certificates to validate server certificate against. If not set, the host's root CA certificates are used.")
}

// NewSwiftObjectClient makes a new chunk.Client that writes chunks to OpenStack Swift.
func NewSwiftObjectClient(cfg SwiftConfig, hedgingCfg hedging.Config) (*SwiftObjectClient, error) {
	log.WarnExperimentalUse("OpenStack Swift Storage", log.Logger)

	c, err := createConnection(cfg, hedgingCfg, false)
	if err != nil {
		return nil, err
	}
	// Ensure the container is created, no error is returned if it already exists.
	if err := c.ContainerCreate(context.Background(), cfg.ContainerName, nil); err != nil {
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
	defaultTransport, err := defaultTransport(cfg.HTTP)
	if err != nil {
		return nil, err
	}

	c := &swift.Connection{
		AuthVersion:    cfg.AuthVersion,
		AuthUrl:        cfg.AuthURL,
		Internal:       cfg.Internal,
		ApiKey:         cfg.Password.String(),
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

	// Create a connection
	err = c.Authenticate(context.TODO())
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
	if _, err := s.GetAttributes(ctx, objectKey); err != nil {
		if s.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *SwiftObjectClient) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	info, _, err := s.hedgingConn.Object(ctx, s.cfg.ContainerName, objectKey)
	if err != nil {
		return client.ObjectAttributes{}, nil
	}

	return client.ObjectAttributes{Size: info.Bytes}, nil
}

// GetObject returns a reader and the size for the specified object key from the configured swift container.
func (s *SwiftObjectClient) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var buf bytes.Buffer
	_, err := s.hedgingConn.ObjectGet(ctx, s.cfg.ContainerName, objectKey, &buf, false, nil)
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
	_, err := s.hedgingConn.ObjectGet(ctx, s.cfg.ContainerName, objectKey, &buf, false, h)
	if err != nil {
		return nil, err
	}

	return io.NopCloser(&buf), nil
}

// PutObject puts the specified bytes into the configured Swift container at the provided key
func (s *SwiftObjectClient) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	_, err := s.conn.ObjectPut(ctx, s.cfg.ContainerName, objectKey, object, false, "", "", nil)
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

	objs, err := s.conn.ObjectsAll(ctx, s.cfg.ContainerName, opts)
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
	return s.conn.ObjectDelete(ctx, s.cfg.ContainerName, objectKey)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (s *SwiftObjectClient) IsObjectNotFoundErr(err error) bool {
	return errors.Is(err, swift.ObjectNotFound)
}

// TODO(dannyk): implement for client
func IsRetryableErr(error) bool { return false }

func (s *SwiftObjectClient) IsRetryableErr(err error) bool {
	return IsRetryableErr(err)
}
