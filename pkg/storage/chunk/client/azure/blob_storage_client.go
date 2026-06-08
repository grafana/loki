package azure

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blockblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/instrument"
	"github.com/mattn/go-ieproxy"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	client_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	loki_instrument "github.com/grafana/loki/v3/pkg/util/instrument"
)

const (
	// Environment
	azureGlobal       = "AzureGlobal"
	azureChinaCloud   = "AzureChinaCloud"
	azureGermanCloud  = "AzureGermanCloud"
	azureUSGovernment = "AzureUSGovernment"
)

var (
	supportedEnvironments = []string{azureGlobal, azureChinaCloud, azureGermanCloud, azureUSGovernment}

	defaultEndpoints = map[string]string{
		azureGlobal:       "blob.core.windows.net",
		azureChinaCloud:   "blob.core.chinacloudapi.cn",
		azureGermanCloud:  "blob.core.cloudapi.de",
		azureUSGovernment: "blob.core.usgovcloudapi.net",
	}

	// defaultAuthorityHosts maps environment names to their Azure AD authority host URLs.
	// Used when authenticating with token credentials in non-global environments.
	// The global environment is intentionally absent: azidentity defaults to AzurePublic and
	// automatically respects the AZURE_AUTHORITY_HOST environment variable.
	defaultAuthorityHosts = map[string]string{
		azureChinaCloud:   cloud.AzureChina.ActiveDirectoryAuthorityHost,
		azureGermanCloud:  "https://login.microsoftonline.de/",
		azureUSGovernment: cloud.AzureGovernment.ActiveDirectoryAuthorityHost,
	}

	// default Azure http client.
	defaultClientFactory = func() *http.Client {
		return &http.Client{
			Transport: &http.Transport{
				Proxy: ieproxy.GetProxyFunc(),
				Dial: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
					DualStack: true,
				}).Dial,
				MaxIdleConns:           200,
				MaxIdleConnsPerHost:    200,
				IdleConnTimeout:        90 * time.Second,
				TLSHandshakeTimeout:    10 * time.Second,
				ExpectContinueTimeout:  1 * time.Second,
				DisableKeepAlives:      false,
				DisableCompression:     false,
				MaxResponseHeaderBytes: 0,
			},
		}
	}
)

// BlobStorageConfig defines the configurable flags that can be defined when using azure blob storage.
type BlobStorageConfig struct {
	Environment             string         `yaml:"environment"`
	StorageAccountName      string         `yaml:"account_name"`
	StorageAccountKey       flagext.Secret `yaml:"account_key"`
	ConnectionString        flagext.Secret `yaml:"connection_string"`
	ContainerName           string         `yaml:"container_name"`
	ActiveDirectoryEndpoint string         `yaml:"active_directory_endpoint"`
	EndpointSuffix          string         `yaml:"endpoint_suffix"`
	UseManagedIdentity      bool           `yaml:"use_managed_identity"`
	UseFederatedToken       bool           `yaml:"use_federated_token"`
	UserAssignedID          string         `yaml:"user_assigned_id"`
	UseServicePrincipal     bool           `yaml:"use_service_principal"`
	ClientID                string         `yaml:"client_id"`
	ClientSecret            flagext.Secret `yaml:"client_secret"`
	TenantID                string         `yaml:"tenant_id"`
	ChunkDelimiter          string         `yaml:"chunk_delimiter"`
	DownloadBufferSize      int            `yaml:"download_buffer_size"`
	UploadBufferSize        int            `yaml:"upload_buffer_size"`
	UploadBufferCount       int            `yaml:"upload_buffer_count"`
	RequestTimeout          time.Duration  `yaml:"request_timeout"`
	MaxRetries              int            `yaml:"max_retries"`
	MinRetryDelay           time.Duration  `yaml:"min_retry_delay"`
	MaxRetryDelay           time.Duration  `yaml:"max_retry_delay"`
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
	f.Var(&c.ConnectionString, prefix+"azure.connection-string", "If `connection-string` is set, the values of `account-name` and `endpoint-suffix` values will not be used. Use this method over `account-key` if you need to authenticate via a SAS token. Or if you use the Azurite emulator.")
	f.StringVar(&c.ContainerName, prefix+"azure.container-name", constants.Loki, "Name of the storage account blob container used to store chunks. This container must be created before running cortex.")
	f.StringVar(&c.EndpointSuffix, prefix+"azure.endpoint-suffix", "", "Azure storage endpoint suffix without schema. The storage account name will be prefixed to this value to create the FQDN.")
	f.StringVar(&c.ActiveDirectoryEndpoint, prefix+"azure.active-directory-endpoint", "", "Azure active directory endpoint override. Use when the Azure SDK does not support your environment.")
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
	cfg     *BlobStorageConfig
	metrics BlobStorageMetrics

	containerClient        *container.Client
	hedgingContainerClient *container.Client
}

// NewBlobStorage creates a new instance of the BlobStorage struct.
func NewBlobStorage(cfg *BlobStorageConfig, metrics BlobStorageMetrics, hedgingCfg hedging.Config) (*BlobStorage, error) {
	blobStorage := &BlobStorage{
		cfg:     cfg,
		metrics: metrics,
	}

	var err error
	blobStorage.containerClient, err = blobStorage.newContainerClient(hedgingCfg, false)
	if err != nil {
		return nil, err
	}
	blobStorage.hedgingContainerClient, err = blobStorage.newContainerClient(hedgingCfg, true)
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
		blobClient := b.getBlobClient(objectKey, false)

		response, err := blobClient.GetProperties(ctx, nil)
		if err != nil {
			return err
		}
		if response.ContentLength != nil {
			objectSize = *response.ContentLength
		}
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

// GetObjectRange returns a reader for the specified byte range of the object key.
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
	blobClient := b.getBlobClient(objectKey, true)

	// A zero-value HTTPRange (offset=0, count=0) downloads the entire blob.
	downloadResponse, err := blobClient.DownloadStream(ctx, &blob.DownloadStreamOptions{
		Range: blob.HTTPRange{
			Offset: offset,
			Count:  length,
		},
	})
	if err != nil {
		return nil, 0, err
	}

	if downloadResponse.ContentLength != nil {
		size = *downloadResponse.ContentLength
	}
	retryReader := downloadResponse.NewRetryReader(ctx, &blob.RetryReaderOptions{
		MaxRetries: int32(b.cfg.MaxRetries),
	})
	return retryReader, size, nil
}

func (b *BlobStorage) PutObject(ctx context.Context, objectKey string, object io.Reader) error {
	return loki_instrument.TimeRequest(ctx, "azure.PutObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		blobClient := b.getBlobClient(objectKey, false)

		_, err := blobClient.UploadStream(ctx, object, &blockblob.UploadStreamOptions{
			BlockSize:   int64(b.cfg.UploadBufferSize),
			Concurrency: b.cfg.UploadBufferCount,
		})
		return err
	})
}

// getBlobClient returns a block blob client for the given blob ID.
// When hedging is true, the underlying HTTP client is the hedging-wrapped one.
func (b *BlobStorage) getBlobClient(blobID string, hedging bool) *blockblob.Client {
	blobID = strings.ReplaceAll(blobID, ":", b.cfg.ChunkDelimiter)
	if hedging {
		return b.hedgingContainerClient.NewBlockBlobClient(blobID)
	}
	return b.containerClient.NewBlockBlobClient(blobID)
}

// newContainerClient creates an Azure Blob Storage container client.
// When isHedging is true, the underlying HTTP client is wrapped for request hedging.
func (b *BlobStorage) newContainerClient(hedgingCfg hedging.Config, isHedging bool) (*container.Client, error) {
	httpClient := defaultClientFactory()
	if isHedging {
		var err error
		httpClient, err = hedgingCfg.ClientWithRegisterer(httpClient, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
	}

	opts := &container.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Retry: policy.RetryOptions{
				// The legacy SDK used MaxTries (total number of attempts) while azcore
				// uses MaxRetries (retries after the first attempt), so subtract one.
				// azcore also treats MaxRetries==0 as "use the default (3)", so clamp
				// to -1 (which azcore normalises to 0 retries = 1 total attempt).
				MaxRetries:    totalTriesToMaxRetries(b.cfg.MaxRetries),
				TryTimeout:    b.cfg.RequestTimeout,
				RetryDelay:    b.cfg.MinRetryDelay,
				MaxRetryDelay: b.cfg.MaxRetryDelay,
			},
			// *http.Client satisfies the policy.Transporter interface.
			Transport: httpClient,
		},
	}

	if b.cfg.ConnectionString.String() != "" {
		return container.NewClientFromConnectionString(b.cfg.ConnectionString.String(), b.cfg.ContainerName, opts)
	}

	containerURL := b.fmtContainerURL()

	if !b.cfg.UseFederatedToken && !b.cfg.UseManagedIdentity && !b.cfg.UseServicePrincipal && b.cfg.UserAssignedID == "" {
		cred, err := container.NewSharedKeyCredential(b.cfg.StorageAccountName, b.cfg.StorageAccountKey.String())
		if err != nil {
			return nil, err
		}
		return container.NewClientWithSharedKeyCredential(containerURL, cred, opts)
	}

	tokenCred, err := b.buildTokenCredential()
	if err != nil {
		return nil, err
	}
	return container.NewClient(containerURL, tokenCred, opts)
}

// buildTokenCredential creates an Azure token credential based on the configured authentication method.
func (b *BlobStorage) buildTokenCredential() (azcore.TokenCredential, error) {
	authorityHost := b.authorityHost()

	if b.cfg.UseServicePrincipal {
		opts := &azidentity.ClientSecretCredentialOptions{}
		if authorityHost != "" {
			opts.Cloud = cloud.Configuration{ActiveDirectoryAuthorityHost: authorityHost}
		}
		return azidentity.NewClientSecretCredential(b.cfg.TenantID, b.cfg.ClientID, b.cfg.ClientSecret.String(), opts)
	}

	if b.cfg.UseFederatedToken {
		opts := &azidentity.WorkloadIdentityCredentialOptions{}
		if authorityHost != "" {
			opts.Cloud = cloud.Configuration{ActiveDirectoryAuthorityHost: authorityHost}
		}
		return azidentity.NewWorkloadIdentityCredential(opts)
	}

	// UseManagedIdentity or UserAssignedID
	msiOpts := &azidentity.ManagedIdentityCredentialOptions{}
	if b.cfg.UserAssignedID != "" {
		msiOpts.ID = azidentity.ClientID(b.cfg.UserAssignedID)
	}
	return azidentity.NewManagedIdentityCredential(msiOpts)
}

// authorityHost returns the Azure AD authority host for the configured environment and active
// directory endpoint. An empty string signals that azidentity should use its default (Azure
// Public Cloud, honouring the AZURE_AUTHORITY_HOST environment variable).
func (b *BlobStorage) authorityHost() string {
	if b.cfg.ActiveDirectoryEndpoint != "" {
		return b.cfg.ActiveDirectoryEndpoint
	}
	return defaultAuthorityHosts[b.cfg.Environment] // empty string for azureGlobal
}

// List implements chunk.ObjectClient.
func (b *BlobStorage) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix

	pager := b.containerClient.NewListBlobsHierarchyPager(delimiter, &container.ListBlobsHierarchyOptions{
		Prefix: new(prefix),
	})

	for pager.More() {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		err := loki_instrument.TimeRequest(ctx, "azure.List", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return err
			}

			if page.Segment == nil {
				return nil
			}

			for _, blobInfo := range page.Segment.BlobItems {
				if blobInfo.Name == nil || blobInfo.Properties == nil || blobInfo.Properties.LastModified == nil {
					continue
				}
				storageObjects = append(storageObjects, client.StorageObject{
					Key:        *blobInfo.Name,
					ModifiedAt: *blobInfo.Properties.LastModified,
				})
			}

			for _, blobPrefix := range page.Segment.BlobPrefixes {
				if blobPrefix.Name == nil {
					continue
				}
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
		blobClient := b.getBlobClient(blobID, false)

		_, err := blobClient.Delete(ctx, &blob.DeleteOptions{
			DeleteSnapshots: to.Ptr(blob.DeleteSnapshotsOptionTypeInclude),
		})
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

func (b *BlobStorage) fmtServiceURL() string {
	var endpoint string
	if b.cfg.EndpointSuffix != "" {
		endpoint = b.cfg.EndpointSuffix
	} else {
		endpoint = defaultEndpoints[b.cfg.Environment]
	}
	return fmt.Sprintf("https://%s.%s", b.cfg.StorageAccountName, endpoint)
}

func (b *BlobStorage) fmtContainerURL() string {
	return fmt.Sprintf("%s/%s", b.fmtServiceURL(), b.cfg.ContainerName)
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (b *BlobStorage) IsObjectNotFoundErr(err error) bool {
	return bloberror.HasCode(err, bloberror.BlobNotFound)
}

// IsRetryableErr returns true if the error is retriable.
// TODO(dannyk): implement for client
func (b *BlobStorage) IsRetryableErr(error) bool { return false }

// totalTriesToMaxRetries converts a "total number of attempts" value (as used by the
// legacy azure-storage-blob-go SDK's MaxTries) into an azcore MaxRetries value.
//
// azcore.RetryOptions.MaxRetries counts only the retries that follow the initial
// attempt, so the conversion is simply n-1. There is one special case: azcore
// interprets MaxRetries==0 as "use the built-in default (3 retries)" rather than
// "no retries", so when the subtraction would yield 0 we return -1, which azcore
// normalises to 0 retries (i.e. one total attempt).
func totalTriesToMaxRetries(totalTries int) int32 {
	if totalTries <= 1 {
		return -1 // -1 → azcore: no retries (1 total attempt)
	}
	return int32(totalTries) - 1
}
