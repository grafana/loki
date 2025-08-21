package azure

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	client_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_instrument "github.com/grafana/loki/v3/pkg/util/instrument"
	"github.com/grafana/loki/v3/pkg/util/log"
)

const (
	// Environment
	azureGlobal       = "AzureGlobal"
	azurePublicCloud  = "AzurePublicCloud"
	azureChinaCloud   = "AzureChinaCloud"
	azureGermanCloud  = "AzureGermanCloud"
	azureUSGovernment = "AzureUSGovernment"
)

var (
	supportedEnvironments = []string{azureGlobal, azureChinaCloud, azureGermanCloud, azureUSGovernment}
	//noClientKey           = azblob.ClientProvidedKeyOptions{}

	defaultEndpoints = map[string]string{
		azureGlobal:       "blob.core.windows.net",
		azureChinaCloud:   "blob.core.chinacloudapi.cn",
		azureGermanCloud:  "blob.core.cloudapi.de",
		azureUSGovernment: "blob.core.usgovcloudapi.net",
	}
)

// BlobStorageConfig defines the configurable flags that can be defined when using azure blob storage.
type BlobStorageConfig struct {
	Environment         string         `yaml:"environment"`
	StorageAccountName  string         `yaml:"account_name"`
	StorageAccountKey   flagext.Secret `yaml:"account_key"`
	ConnectionString    string         `yaml:"connection_string"`
	ContainerName       string         `yaml:"container_name"`
	EndpointSuffix      string         `yaml:"endpoint_suffix"`
	UseManagedIdentity  bool           `yaml:"use_managed_identity"`
	UseFederatedToken   bool           `yaml:"use_federated_token"`
	UserAssignedID      string         `yaml:"user_assigned_id"`
	UseServicePrincipal bool           `yaml:"use_service_principal"`
	ClientID            string         `yaml:"client_id"`
	ClientSecret        flagext.Secret `yaml:"client_secret"`
	TenantID            string         `yaml:"tenant_id"`
	ChunkDelimiter      string         `yaml:"chunk_delimiter"`
	DownloadBufferSize  int            `yaml:"download_buffer_size"`
	UploadBufferSize    int            `yaml:"upload_buffer_size"`
	UploadBufferCount   int            `yaml:"upload_buffer_count"`
	RequestTimeout      time.Duration  `yaml:"request_timeout"`
	MaxRetries          int            `yaml:"max_retries"`
	MinRetryDelay       time.Duration  `yaml:"min_retry_delay"`
	MaxRetryDelay       time.Duration  `yaml:"max_retry_delay"`
	MaxResults          int            `yaml:"max_results"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (c *BlobStorageConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (c *BlobStorageConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.Environment, prefix+"azure.environment", azureGlobal, fmt.Sprintf("Azure Cloud environment. Supported values are: %s.", strings.Join(supportedEnvironments, ", ")))
	f.StringVar(&c.StorageAccountName, prefix+"azure.account-name", "", "Azure storage account name.")
	f.Var(&c.StorageAccountKey, prefix+"azure.account-key", "Azure storage account key.")
	f.StringVar(&c.ConnectionString, prefix+"azure.connection-string", "", "If `connection-string` is set, the values of `account-name` and `endpoint-suffix` values will not be used. Use this method over `account-key` if you need to authenticate via a SAS token. Or if you use the Azurite emulator.")
	f.StringVar(&c.ContainerName, prefix+"azure.container-name", constants.Loki, "Name of the storage account blob container used to store chunks. This container must be created before running cortex.")
	f.StringVar(&c.EndpointSuffix, prefix+"azure.endpoint-suffix", "", "Azure storage endpoint suffix without schema. The storage account name will be prefixed to this value to create the FQDN.")
	f.BoolVar(&c.UseManagedIdentity, prefix+"azure.use-managed-identity", false, "Use Managed Identity to authenticate to the Azure storage account.")
	f.BoolVar(&c.UseFederatedToken, prefix+"azure.use-federated-token", false, "Use Federated Token to authenticate to the Azure storage account.")
	f.StringVar(&c.UserAssignedID, prefix+"azure.user-assigned-id", "", "User assigned identity ID to authenticate to the Azure storage account.")
	f.StringVar(&c.ChunkDelimiter, prefix+"azure.chunk-delimiter", "-", "Chunk delimiter for blob ID to be used")
	f.DurationVar(&c.RequestTimeout, prefix+"azure.request-timeout", 30*time.Second, "Timeout for requests made against azure blob storage.")
	f.IntVar(&c.DownloadBufferSize, prefix+"azure.download-buffer-size", 512000, "Preallocated buffer size for downloads.")
	f.IntVar(&c.UploadBufferSize, prefix+"azure.upload-buffer-size", 256000, "Preallocated buffer size for uploads.")
	f.IntVar(&c.UploadBufferCount, prefix+"azure.download-buffer-count", 1, "Number of buffers used to used to upload a chunk.")
	f.IntVar(&c.MaxRetries, prefix+"azure.max-retries", 5, "Number of retries for a request which times out.")
	f.DurationVar(&c.MinRetryDelay, prefix+"azure.min-retry-delay", 10*time.Millisecond, "Minimum time to wait before retrying a request.")
	f.DurationVar(&c.MaxRetryDelay, prefix+"azure.max-retry-delay", 500*time.Millisecond, "Maximum time to wait before retrying a request.")
	f.BoolVar(&c.UseServicePrincipal, prefix+"azure.use-service-principal", false, "Use Service Principal to authenticate through Azure OAuth.")
	f.StringVar(&c.TenantID, prefix+"azure.tenant-id", "", "Azure Tenant ID is used to authenticate through Azure OAuth.")
	f.StringVar(&c.ClientID, prefix+"azure.client-id", "", "Azure Service Principal ID(GUID).")
	f.Var(&c.ClientSecret, prefix+"azure.client-secret", "Azure Service Principal secret key.")
	f.IntVar(&c.MaxResults, prefix+"azure.max-results", 5000, "Max number of results to return at once when listing container contents.")
}

type BlobStorageMetrics struct {
	requestDuration  *prometheus.HistogramVec
	egressBytesTotal prometheus.Counter
}

// NewBlobStorageMetrics creates the blob storage metrics struct and registers all of it's metrics.
func NewBlobStorageMetrics() BlobStorageMetrics {
	b := BlobStorageMetrics{
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "azure_blob_request_duration_seconds",
			Help:      "Time spent doing azure blob requests.",
			// Latency seems to range from a few ms to a few secs and is
			// important. So use 6 buckets from 5ms to 5s.
			Buckets: prometheus.ExponentialBuckets(0.005, 4, 6),
		}, []string{"operation", "status_code"}),
		egressBytesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Name:      "azure_blob_egress_bytes_total",
			Help:      "Total bytes downloaded from Azure Blob Storage.",
		}),
	}
	prometheus.MustRegister(b.requestDuration)
	prometheus.MustRegister(b.egressBytesTotal)
	return b
}

// Unregister unregisters the blob storage metrics with the prometheus default registerer, useful for tests
// where we frequently need to create multiple instances of the metrics struct, but not globally.
func (bm *BlobStorageMetrics) Unregister() {
	prometheus.Unregister(bm.requestDuration)
	prometheus.Unregister(bm.egressBytesTotal)
}

// BlobStorage is used to interact with azure blob storage for setting or getting time series chunks.
// Implements ObjectStorage
type BlobStorage struct {
	// blobService storage.Serv
	cfg *BlobStorageConfig

	metrics BlobStorageMetrics

	tc   azcore.TokenCredential
	lock sync.Mutex
}

// NewBlobStorage creates a new instance of the BlobStorage struct.
func NewBlobStorage(cfg *BlobStorageConfig, metrics BlobStorageMetrics, hedgingCfg hedging.Config) (*BlobStorage, error) {
	blobStorage := &BlobStorage{
		cfg:     cfg,
		metrics: metrics,
	}
	_, err := blobStorage.getOAuthToken()
	if err != nil {
		return nil, err
	}

	return blobStorage, nil
}

// Stop is a no op, as there are no background workers with this driver currently
func (b *BlobStorage) Stop() {}

func (b *BlobStorage) ObjectExists(ctx context.Context, objectKey string) (bool, error) {
	if _, err := b.objectAttributes(ctx, objectKey, "azure.ObjectExists"); err != nil {
		if b.IsObjectNotFoundErr(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (b *BlobStorage) GetAttributes(ctx context.Context, objectKey string) (client.ObjectAttributes, error) {
	return b.objectAttributes(ctx, objectKey, "azure.GetAttributes")
}

func (b *BlobStorage) objectAttributes(ctx context.Context, objectKey, source string) (client.ObjectAttributes, error) {
	var objectSize int64
	err := loki_instrument.TimeRequest(ctx, source, instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		client, err := b.getBlobClient(objectKey)
		if err != nil {
			return err
		}
		prop, err := client.GetProperties(ctx, &blob.GetPropertiesOptions{})
		if err != nil {
			return err
		}
		objectSize = *prop.ContentLength
		return nil
	})
	if err != nil {
		return client.ObjectAttributes{}, err
	}

	return client.ObjectAttributes{Size: objectSize}, nil
}

// GetObject returns a reader and the size for the specified object key.
func (b *BlobStorage) GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	var cancel context.CancelFunc = func() {}
	if b.cfg.RequestTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, (time.Duration(b.cfg.MaxRetries)*b.cfg.RequestTimeout)+(time.Duration(b.cfg.MaxRetries-1)*b.cfg.MaxRetryDelay)) // timeout only after azure client's built in retries
	}

	var (
		size int64
		rc   io.ReadCloser
	)
	err := loki_instrument.TimeRequest(ctx, "azure.GetObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		rc, size, err = b.getObject(ctx, objectKey, 0, 0)
		return err
	})
	b.metrics.egressBytesTotal.Add(float64(size))
	if err != nil {
		// cancel the context if there is an error.
		cancel()
		return nil, 0, err
	}
	// else return a wrapped ReadCloser which cancels the context while closing the reader.
	return client_util.NewReadCloserWithContextCancelFunc(rc, cancel), size, nil
}

// GetObject returns a reader and the size for the specified object key.
func (b *BlobStorage) GetObjectRange(ctx context.Context, objectKey string, offset, length int64) (io.ReadCloser, error) {
	var cancel context.CancelFunc = func() {}
	if b.cfg.RequestTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, (time.Duration(b.cfg.MaxRetries)*b.cfg.RequestTimeout)+(time.Duration(b.cfg.MaxRetries-1)*b.cfg.MaxRetryDelay)) // timeout only after azure client's built in retries
	}

	var (
		size int64
		rc   io.ReadCloser
	)
	err := loki_instrument.TimeRequest(ctx, "azure.GetObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		rc, size, err = b.getObject(ctx, objectKey, offset, length)
		return err
	})
	b.metrics.egressBytesTotal.Add(float64(size))
	if err != nil {
		// cancel the context if there is an error.
		cancel()
		return nil, err
	}
	// else return a wrapped ReadCloser which cancels the context while closing the reader.
	return client_util.NewReadCloserWithContextCancelFunc(rc, cancel), nil
}

func (b *BlobStorage) getObject(ctx context.Context, objectKey string, offset, length int64) (rc io.ReadCloser, size int64, err error) {
	if offset == 0 && length == 0 {
		length = blob.CountToEnd // blob.CountToEnd == 0 but leaving this here for clarity
	}

	client, err := b.getClient(objectKey)
	if err != nil {
		return nil, 0, err
	}
	downloadResponse, err := client.DownloadStream(ctx, b.cfg.ContainerName, objectKey, &azblob.DownloadStreamOptions{Range: blob.HTTPRange{Offset: offset, Count: length}})
	if err != nil {
		return nil, 0, err
	}

	return downloadResponse.NewRetryReader(ctx, &blob.RetryReaderOptions{MaxRetries: int32(b.cfg.MaxRetries)}), *downloadResponse.ContentLength, nil
}

func (b *BlobStorage) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return loki_instrument.TimeRequest(ctx, "azure.PutObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		client, err := b.getClient(objectKey)
		if err != nil {
			return err
		}

		bufferSize := b.cfg.UploadBufferSize
		maxBuffers := b.cfg.UploadBufferCount
		_, err = client.UploadStream(ctx, b.cfg.ContainerName, objectKey, object, &blockblob.UploadStreamOptions{BlockSize: int64(bufferSize), Concurrency: maxBuffers})
		return err
	})
}

func (b *BlobStorage) getClient(blobID string) (azblob.Client, error) {
	var client *azblob.Client
	var err error
	if b.cfg.UseManagedIdentity {
		client, err = azblob.NewClient(b.fmtBlobURL(blobID), b.tc, nil)
	} else {
		cred, err := azblob.NewSharedKeyCredential(b.cfg.StorageAccountName, b.cfg.StorageAccountKey.String())
		if err != nil {
			return azblob.Client{}, err
		}
		client, err = azblob.NewClientWithSharedKeyCredential(b.fmtResourceURL(), cred, nil)
		if err != nil {
			return azblob.Client{}, err
		}
	}
	return *client, err
}

func (b *BlobStorage) getBlobClient(blobID string) (blob.Client, error) {
	client, err := blob.NewClient(b.fmtBlobURL(blobID), b.tc, nil)
	return *client, err
}

func (b *BlobStorage) getOAuthToken() (azcore.TokenCredential, error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	// this method is called a few times when we create each Pipeline, so we need to re-use TokenCredentials.
	if b.tc != nil {
		return b.tc, nil
	}

	if b.cfg.UseFederatedToken {
		jwtBytes, err := os.ReadFile(os.Getenv("AZURE_FEDERATED_TOKEN_FILE"))
		if err != nil {
			return nil, err
		}
		tokenCredential, err := azidentity.NewClientSecretCredential(b.cfg.TenantID, b.cfg.ClientID, string(jwtBytes), nil)
		if err != nil {
			return nil, err
		}
		b.tc = tokenCredential
		return b.tc, nil
	} else if b.cfg.UseServicePrincipal {
		tokenCredential, err := azidentity.NewClientSecretCredential(b.cfg.TenantID, b.cfg.ClientID, b.cfg.ClientSecret.String(), nil)
		if err != nil {
			return nil, err
		}
		b.tc = tokenCredential
		return b.tc, nil
	} else {
		if b.cfg.UserAssignedID != "" {
			opts := azidentity.ManagedIdentityCredentialOptions{ID: azidentity.ClientID(b.cfg.UserAssignedID)}
			tokenCredential, err := azidentity.NewManagedIdentityCredential(&opts)
			if err != nil {
				return nil, err
			}
			b.tc = tokenCredential
			return b.tc, nil
		} else {
			tokenCredential, err := azidentity.NewManagedIdentityCredential(nil)
			if err != nil {
				return nil, err
			}
			b.tc = tokenCredential
			return b.tc, nil
		}
	}
}

// List implements chunk.ObjectClient.
func (b *BlobStorage) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix
	var containerClient *container.Client
	var err error
	if b.cfg.UseManagedIdentity {
		containerClient, err = container.NewClient(b.fmtContainerURL(), b.tc, nil)
		if err != nil {
			return nil, nil, err
		}
	} else {
		cred, err := azblob.NewSharedKeyCredential(b.cfg.StorageAccountName, b.cfg.StorageAccountKey.String())
		if err != nil {
			return nil, nil, err
		}
		containerClient, err = container.NewClientWithSharedKeyCredential(b.fmtResourceURL(), cred, nil)
		if err != nil {
			return nil, nil, err
		}
	}
	maxResults := int32(b.cfg.MaxResults)
	pager := containerClient.NewListBlobsHierarchyPager("/", &container.ListBlobsHierarchyOptions{
		Include:    container.ListBlobsInclude{Metadata: true, Tags: true},
		MaxResults: &maxResults,
		Prefix:     &prefix,
	})
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return nil, nil, err
		}

		err = loki_instrument.TimeRequest(ctx, "azure.List", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
			// Process the blobs returned in this result segment (if the segment is empty, the loop body won't execute)
			for _, blobInfo := range resp.Segment.BlobItems {
				storageObjects = append(storageObjects, client.StorageObject{
					Key:        *blobInfo.Name,
					ModifiedAt: *blobInfo.Properties.LastModified,
				})
			}

			// Process the BlobPrefixes so called commonPrefixes or synthetic directories in the listed synthetic directory
			for _, blobPrefix := range resp.Segment.BlobPrefixes {
				commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(*blobPrefix.Name))
			}

			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}

	return storageObjects, commonPrefixes, nil
}

func (b *BlobStorage) DeleteObject(ctx context.Context, blobID string) error {
	return loki_instrument.TimeRequest(ctx, "azure.DeleteObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		client, err := b.getBlobClient(blobID)
		if err != nil {
			return err
		}
		_, err = client.Delete(ctx, nil)
		return err
	})
}

// Validate the config.
func (c *BlobStorageConfig) Validate() error {
	if !util.StringsContain(supportedEnvironments, c.Environment) {
		return fmt.Errorf("unsupported Azure blob storage environment: %s, please select one of: %s ", c.Environment, strings.Join(supportedEnvironments, ", "))
	}
	if c.UseServicePrincipal {
		if strings.TrimSpace(c.TenantID) == "" {
			return fmt.Errorf("tenant_id is required if authentication using Service Principal is enabled")
		}
		if strings.TrimSpace(c.ClientID) == "" {
			return fmt.Errorf("client_id is required if authentication using Service Principal is enabled")
		}
		if strings.TrimSpace(c.ClientSecret.String()) == "" {
			return fmt.Errorf("client_secret is required if authentication using Service Principal is enabled")
		}
	}
	return nil
}

func (b *BlobStorage) fmtResourceURL() string {
	if b.cfg.ConnectionString != "" {
		return b.endpointFromConnectionString()
	}

	var endpoint string
	if b.cfg.EndpointSuffix != "" {
		endpoint = b.cfg.EndpointSuffix
	} else {
		endpoint = defaultEndpoints[b.cfg.Environment]
	}

	return fmt.Sprintf("https://%s.%s", b.cfg.StorageAccountName, endpoint)
}

func (b *BlobStorage) endpointFromConnectionString() string {
	parsed, err := parseConnectionString(b.cfg.ConnectionString)
	if err != nil || parsed.ServiceURL == "" {
		level.Warn(log.Logger).Log("msg", "could not get resource URL from connection string", "err", err)
	}
	return parsed.ServiceURL
}

func (b *BlobStorage) fmtBlobURL(blobID string) string {
	return fmt.Sprintf("%s/%s", b.fmtContainerURL(), blobID)
}

func (b *BlobStorage) fmtContainerURL() string {
	return fmt.Sprintf("%s/%s", b.fmtResourceURL(), b.cfg.ContainerName)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (b *BlobStorage) IsObjectNotFoundErr(err error) bool {
	return bloberror.HasCode(err, bloberror.BlobNotFound)
}

var errConnectionString = errors.New("connection string is either blank or malformed. The expected connection string " +
	"should contain key value pairs separated by semicolons. For example 'DefaultEndpointsProtocol=https;AccountName=<accountName>;" +
	"AccountKey=<accountKey>;EndpointSuffix=core.windows.net'")

type ParsedConnectionString struct {
	ServiceURL  string
	AccountName string
	AccountKey  string
}

// parseConnectionString dissects a connection string into url, account and key
// (copied from github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal/shared)
func parseConnectionString(connectionString string) (ParsedConnectionString, error) {
	const (
		defaultScheme = "https"
		defaultSuffix = "core.windows.net"
	)

	connStrMap := make(map[string]string)
	connectionString = strings.TrimRight(connectionString, ";")

	splitString := strings.Split(connectionString, ";")
	if len(splitString) == 0 {
		return ParsedConnectionString{}, errConnectionString
	}
	for _, stringPart := range splitString {
		parts := strings.SplitN(stringPart, "=", 2)
		if len(parts) != 2 {
			return ParsedConnectionString{}, errConnectionString
		}
		connStrMap[parts[0]] = parts[1]
	}

	accountName, ok := connStrMap["AccountName"]
	if !ok {
		return ParsedConnectionString{}, errors.New("connection string missing AccountName")
	}

	accountKey, ok := connStrMap["AccountKey"]
	if !ok {
		sharedAccessSignature, ok := connStrMap["SharedAccessSignature"]
		if !ok {
			return ParsedConnectionString{}, errors.New("connection string missing AccountKey and SharedAccessSignature")
		}
		return ParsedConnectionString{
			ServiceURL: fmt.Sprintf("%v://%v.blob.%v/?%v", defaultScheme, accountName, defaultSuffix, sharedAccessSignature),
		}, nil
	}

	protocol, ok := connStrMap["DefaultEndpointsProtocol"]
	if !ok {
		protocol = defaultScheme
	}

	suffix, ok := connStrMap["EndpointSuffix"]
	if !ok {
		suffix = defaultSuffix
	}

	if blobEndpoint, ok := connStrMap["BlobEndpoint"]; ok {
		return ParsedConnectionString{
			ServiceURL:  blobEndpoint,
			AccountName: accountName,
			AccountKey:  accountKey,
		}, nil
	}

	return ParsedConnectionString{
		ServiceURL:  fmt.Sprintf("%v://%v.blob.%v", protocol, accountName, suffix),
		AccountName: accountName,
		AccountKey:  accountKey,
	}, nil
}

// TODO(dannyk): implement for client
func (b *BlobStorage) IsRetryableErr(error) bool { return false }
