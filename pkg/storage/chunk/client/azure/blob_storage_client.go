package azure

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/Azure/go-autorest/autorest/adal"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/grafana/dskit/flagext"
	"github.com/mattn/go-ieproxy"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	client_util "github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/log"
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
	noClientKey           = azblob.ClientProvidedKeyOptions{}

	defaultEndpoints = map[string]string{
		azureGlobal:       "blob.core.windows.net",
		azureChinaCloud:   "blob.core.chinacloudapi.cn",
		azureGermanCloud:  "blob.core.cloudapi.de",
		azureUSGovernment: "blob.core.usgovcloudapi.net",
	}

	defaultAuthFunctions = authFunctions{
		NewOAuthConfigFunc: adal.NewOAuthConfig,
		NewServicePrincipalTokenFromFederatedTokenFunc: adal.NewServicePrincipalTokenFromFederatedToken, //nolint:staticcheck // SA1019: use of deprecated function.
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
	Environment         string         `yaml:"environment"`
	StorageAccountName  string         `yaml:"account_name"`
	StorageAccountKey   flagext.Secret `yaml:"account_key"`
	ContainerName       string         `yaml:"container_name"`
	Endpoint            string         `yaml:"endpoint_suffix"`
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
}

type authFunctions struct {
	NewOAuthConfigFunc                             func(activeDirectoryEndpoint, tenantID string) (*adal.OAuthConfig, error)
	NewServicePrincipalTokenFromFederatedTokenFunc func(oauthConfig adal.OAuthConfig, clientID string, jwt string, resource string, callbacks ...adal.TokenRefreshCallback) (*adal.ServicePrincipalToken, error)
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
	f.StringVar(&c.ContainerName, prefix+"azure.container-name", "loki", "Name of the storage account blob container used to store chunks. This container must be created before running cortex.")
	f.StringVar(&c.Endpoint, prefix+"azure.endpoint-suffix", "", "Azure storage endpoint suffix without schema. The storage account name will be prefixed to this value to create the FQDN.")
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
			Namespace: "loki",
			Name:      "azure_blob_request_duration_seconds",
			Help:      "Time spent doing azure blob requests.",
			// Latency seems to range from a few ms to a few secs and is
			// important. So use 6 buckets from 5ms to 5s.
			Buckets: prometheus.ExponentialBuckets(0.005, 4, 6),
		}, []string{"operation", "status_code"}),
		egressBytesTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
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

	containerURL azblob.ContainerURL

	pipeline        pipeline.Pipeline
	hedgingPipeline pipeline.Pipeline
	tc              azblob.TokenCredential
	lock            sync.Mutex
}

// NewBlobStorage creates a new instance of the BlobStorage struct.
func NewBlobStorage(cfg *BlobStorageConfig, metrics BlobStorageMetrics, hedgingCfg hedging.Config) (*BlobStorage, error) {
	log.WarnExperimentalUse("Azure Blob Storage", log.Logger)
	blobStorage := &BlobStorage{
		cfg:     cfg,
		metrics: metrics,
	}
	pipeline, err := blobStorage.newPipeline(hedgingCfg, false)
	if err != nil {
		return nil, err
	}
	blobStorage.pipeline = pipeline
	hedgingPipeline, err := blobStorage.newPipeline(hedgingCfg, true)
	if err != nil {
		return nil, err
	}
	blobStorage.hedgingPipeline = hedgingPipeline
	blobStorage.containerURL, err = blobStorage.buildContainerURL()
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	_, err = blobStorage.containerURL.GetProperties(ctx, azblob.LeaseAccessConditions{})
	if err != nil {
		if bloberror, ok := err.(azblob.StorageError); ok && bloberror.ServiceCode() == azblob.ServiceCodeContainerNotFound {
			_, err = blobStorage.containerURL.Create(ctx, azblob.Metadata{}, azblob.PublicAccessNone)
			if err != nil {
				return nil, fmt.Errorf("failed to create create Azure blob container '%s': %w", blobStorage.cfg.ContainerName, err)
			}
		}
	}

	return blobStorage, nil
}

// Stop is a no op, as there are no background workers with this driver currently
func (b *BlobStorage) Stop() {}

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
	err := instrument.CollectedRequest(ctx, "azure.GetObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		var err error
		rc, size, err = b.getObject(ctx, objectKey)
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

func (b *BlobStorage) getObject(ctx context.Context, objectKey string) (rc io.ReadCloser, size int64, err error) {
	blockBlobURL, err := b.getBlobURL(objectKey, true)
	if err != nil {
		return nil, 0, err
	}

	// Request access to the blob
	downloadResponse, err := blockBlobURL.Download(ctx, 0, azblob.CountToEnd, azblob.BlobAccessConditions{}, false, noClientKey)
	if err != nil {
		return nil, 0, err
	}

	return downloadResponse.Body(azblob.RetryReaderOptions{MaxRetryRequests: b.cfg.MaxRetries}), downloadResponse.ContentLength(), nil
}

func (b *BlobStorage) PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error {
	return instrument.CollectedRequest(ctx, "azure.PutObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		blockBlobURL, err := b.getBlobURL(objectKey, false)
		if err != nil {
			return err
		}

		bufferSize := b.cfg.UploadBufferSize
		maxBuffers := b.cfg.UploadBufferCount
		_, err = azblob.UploadStreamToBlockBlob(ctx, object, blockBlobURL,
			azblob.UploadStreamToBlockBlobOptions{BufferSize: bufferSize, MaxBuffers: maxBuffers})

		return err
	})
}

func (b *BlobStorage) getBlobURL(blobID string, hedging bool) (azblob.BlockBlobURL, error) {
	blobID = strings.Replace(blobID, ":", b.cfg.ChunkDelimiter, -1)

	// generate url for new chunk blob
	u, err := url.Parse(fmt.Sprintf(b.selectBlobURLFmt(), b.cfg.StorageAccountName, b.cfg.ContainerName, blobID))
	if err != nil {
		return azblob.BlockBlobURL{}, err
	}
	pipeline := b.pipeline
	if hedging {
		pipeline = b.hedgingPipeline
	}

	return azblob.NewBlockBlobURL(*u, pipeline), nil
}

func (b *BlobStorage) buildContainerURL() (azblob.ContainerURL, error) {
	u, err := url.Parse(fmt.Sprintf(b.selectContainerURLFmt(), b.cfg.StorageAccountName, b.cfg.ContainerName))
	if err != nil {
		return azblob.ContainerURL{}, err
	}

	return azblob.NewContainerURL(*u, b.pipeline), nil
}

func (b *BlobStorage) newPipeline(hedgingCfg hedging.Config, hedging bool) (pipeline.Pipeline, error) {
	// defining the Azure Pipeline Options
	opts := azblob.PipelineOptions{
		Retry: azblob.RetryOptions{
			Policy:        azblob.RetryPolicyExponential,
			MaxTries:      (int32)(b.cfg.MaxRetries),
			TryTimeout:    b.cfg.RequestTimeout,
			RetryDelay:    b.cfg.MinRetryDelay,
			MaxRetryDelay: b.cfg.MaxRetryDelay,
		},
	}

	client := defaultClientFactory()

	opts.HTTPSender = pipeline.FactoryFunc(func(next pipeline.Policy, po *pipeline.PolicyOptions) pipeline.PolicyFunc {
		return func(ctx context.Context, request pipeline.Request) (pipeline.Response, error) {
			resp, err := client.Do(request.WithContext(ctx))
			return pipeline.NewHTTPResponse(resp), err
		}
	})

	if hedging {
		client, err := hedgingCfg.ClientWithRegisterer(client, prometheus.WrapRegistererWithPrefix("loki_", prometheus.DefaultRegisterer))
		if err != nil {
			return nil, err
		}
		opts.HTTPSender = pipeline.FactoryFunc(func(next pipeline.Policy, po *pipeline.PolicyOptions) pipeline.PolicyFunc {
			return func(ctx context.Context, request pipeline.Request) (pipeline.Response, error) {
				resp, err := client.Do(request.WithContext(ctx))
				return pipeline.NewHTTPResponse(resp), err
			}
		})
	}

	if !b.cfg.UseFederatedToken && !b.cfg.UseManagedIdentity && !b.cfg.UseServicePrincipal && b.cfg.UserAssignedID == "" {
		credential, err := azblob.NewSharedKeyCredential(b.cfg.StorageAccountName, b.cfg.StorageAccountKey.String())
		if err != nil {
			return nil, err
		}

		return azblob.NewPipeline(credential, opts), nil
	}

	tokenCredential, err := b.getOAuthToken()
	if err != nil {
		return nil, err
	}

	return azblob.NewPipeline(tokenCredential, opts), nil
}

func (b *BlobStorage) getOAuthToken() (azblob.TokenCredential, error) {
	b.lock.Lock()
	defer b.lock.Unlock()

	// this method is called a few times when we create each Pipeline, so we need to re-use TokenCredentials.
	if b.tc != nil {
		return b.tc, nil
	}
	spt, err := b.getServicePrincipalToken(defaultAuthFunctions)
	if err != nil {
		return nil, err
	}

	// Refresh obtains a fresh token
	err = spt.Refresh()
	if err != nil {
		return nil, err
	}

	b.tc = azblob.NewTokenCredential(spt.Token().AccessToken, func(tc azblob.TokenCredential) time.Duration {
		err := spt.Refresh()
		if err != nil {
			// something went wrong, prevent the refresher from being triggered again
			return 0
		}

		// set the new token value
		tc.SetToken(spt.Token().AccessToken)

		// get the next token slightly before the current one expires
		return time.Until(spt.Token().Expires()) - 10*time.Second
	})
	return b.tc, nil
}

func (b *BlobStorage) getServicePrincipalToken(authFunctions authFunctions) (*adal.ServicePrincipalToken, error) {
	var endpoint string
	if b.cfg.Endpoint != "" {
		endpoint = b.cfg.Endpoint
	} else {
		endpoint = defaultEndpoints[b.cfg.Environment]
	}

	resource := fmt.Sprintf("https://%s.%s", b.cfg.StorageAccountName, endpoint)

	if b.cfg.UseFederatedToken {
		token, err := b.servicePrincipalTokenFromFederatedToken(resource, authFunctions.NewOAuthConfigFunc, authFunctions.NewServicePrincipalTokenFromFederatedTokenFunc)
		var customRefreshFunc adal.TokenRefresh = func(context context.Context, resource string) (*adal.Token, error) {
			newToken, err := b.servicePrincipalTokenFromFederatedToken(resource, authFunctions.NewOAuthConfigFunc, authFunctions.NewServicePrincipalTokenFromFederatedTokenFunc)
			if err != nil {
				return nil, err
			}

			err = newToken.Refresh()
			if err != nil {
				return nil, err
			}

			token := newToken.Token()

			return &token, nil
		}

		token.SetCustomRefreshFunc(customRefreshFunc)
		return token, err
	}

	if b.cfg.UseServicePrincipal {
		config := auth.NewClientCredentialsConfig(b.cfg.ClientID, b.cfg.ClientSecret.String(), b.cfg.TenantID)
		config.Resource = resource
		return config.ServicePrincipalToken()
	}

	msiConfig := auth.MSIConfig{
		Resource: resource,
	}

	if b.cfg.UserAssignedID != "" {
		msiConfig.ClientID = b.cfg.UserAssignedID
	}

	return msiConfig.ServicePrincipalToken()
}

func (b *BlobStorage) servicePrincipalTokenFromFederatedToken(resource string, newOAuthConfigFunc func(activeDirectoryEndpoint, tenantID string) (*adal.OAuthConfig, error), newServicePrincipalTokenFromFederatedTokenFunc func(oauthConfig adal.OAuthConfig, clientID string, jwt string, resource string, callbacks ...adal.TokenRefreshCallback) (*adal.ServicePrincipalToken, error)) (*adal.ServicePrincipalToken, error) {
	environmentName := azurePublicCloud
	if b.cfg.Environment != azureGlobal {
		environmentName = b.cfg.Environment
	}

	env, err := azure.EnvironmentFromName(environmentName)
	if err != nil {
		return nil, err
	}

	azClientID := os.Getenv("AZURE_CLIENT_ID")
	azTenantID := os.Getenv("AZURE_TENANT_ID")

	jwtBytes, err := os.ReadFile(os.Getenv("AZURE_FEDERATED_TOKEN_FILE"))
	if err != nil {
		return nil, err
	}

	jwt := string(jwtBytes)

	oauthConfig, err := newOAuthConfigFunc(env.ActiveDirectoryEndpoint, azTenantID)
	if err != nil {
		return nil, err
	}

	return newServicePrincipalTokenFromFederatedTokenFunc(*oauthConfig, azClientID, jwt, resource)
}

// List implements chunk.ObjectClient.
func (b *BlobStorage) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	var storageObjects []client.StorageObject
	var commonPrefixes []client.StorageCommonPrefix

	for marker := (azblob.Marker{}); marker.NotDone(); {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}

		err := instrument.CollectedRequest(ctx, "azure.List", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
			listBlob, err := b.containerURL.ListBlobsHierarchySegment(ctx, marker, delimiter, azblob.ListBlobsSegmentOptions{Prefix: prefix})
			if err != nil {
				return err
			}

			marker = listBlob.NextMarker

			// Process the blobs returned in this result segment (if the segment is empty, the loop body won't execute)
			for _, blobInfo := range listBlob.Segment.BlobItems {
				storageObjects = append(storageObjects, client.StorageObject{
					Key:        blobInfo.Name,
					ModifiedAt: blobInfo.Properties.LastModified,
				})
			}

			// Process the BlobPrefixes so called commonPrefixes or synthetic directories in the listed synthetic directory
			for _, blobPrefix := range listBlob.Segment.BlobPrefixes {
				commonPrefixes = append(commonPrefixes, client.StorageCommonPrefix(blobPrefix.Name))
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
	return instrument.CollectedRequest(ctx, "azure.DeleteObject", instrument.NewHistogramCollector(b.metrics.requestDuration), instrument.ErrorCode, func(ctx context.Context) error {
		blockBlobURL, err := b.getBlobURL(blobID, false)
		if err != nil {
			return err
		}

		_, err = blockBlobURL.Delete(ctx, azblob.DeleteSnapshotsOptionInclude, azblob.BlobAccessConditions{})
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

func (b *BlobStorage) selectBlobURLFmt() string {
	if b.cfg.Endpoint != "" {
		return fmt.Sprintf("https://%%s.%s/%%s/%%s", b.cfg.Endpoint)
	}
	return fmt.Sprintf("https://%%s.%s/%%s/%%s", defaultEndpoints[b.cfg.Environment])
}

func (b *BlobStorage) selectContainerURLFmt() string {
	if b.cfg.Endpoint != "" {
		return fmt.Sprintf("https://%%s.%s/%%s", b.cfg.Endpoint)
	}
	return fmt.Sprintf("https://%%s.%s/%%s", defaultEndpoints[b.cfg.Environment])
}

// IsObjectNotFoundErr returns true if error means that object is not found. Relevant to GetObject and DeleteObject operations.
func (b *BlobStorage) IsObjectNotFoundErr(err error) bool {
	var e azblob.StorageError
	if errors.As(err, &e) && e.ServiceCode() == azblob.ServiceCodeBlobNotFound {
		return true
	}

	return false
}
