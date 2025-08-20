package azure

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

var metrics = NewBlobStorageMetrics()

type RoundTripperFunc func(*http.Request) (*http.Response, error)

func (fn RoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func NewTestBlobStorage(cfg *BlobStorageConfig, metrics BlobStorageMetrics, hedgingCfg hedging.Config) (*BlobStorage, error) {
	blobStorage := &BlobStorage{
		cfg:     cfg,
		metrics: metrics,
	}
	_, err := blobStorage.getTestOAuthToken()
	if err != nil {
		return nil, err
	}
	return blobStorage, nil
}

func (b *BlobStorage) getTestOAuthToken() (azcore.TokenCredential, error) {
	b.lock.Lock()
	defer b.lock.Unlock()

	// this method is called a few times when we create each Pipeline, so we need to re-use TokenCredentials.
	if b.tc != nil {
		return b.tc, nil
	}
	token, err := azidentity.NewDefaultAzureCredential(nil)
	b.tc = token
	return b.tc, err

}

func Test_DefaultContainerURL(t *testing.T) {
	c, err := NewTestBlobStorage(&BlobStorageConfig{
		ContainerName:      "foo",
		StorageAccountName: "bar",
		Environment:        azureGlobal,
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("https://bar.blob.core.windows.net/foo")
	containerUrl, _ := url.Parse(c.fmtContainerURL())
	require.Equal(t, *expect, *containerUrl)
}

func Test_EndpointSuffixWithContainer(t *testing.T) {
	c, err := NewTestBlobStorage(&BlobStorageConfig{
		ContainerName:      "foo",
		StorageAccountName: "bar",
		Environment:        azureGlobal,
		EndpointSuffix:     "test.com",
		TenantID:           "",
		ClientID:           "",
		ClientSecret:       flagext.SecretWithValue("test"),
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("https://bar.test.com/foo")
	containerUrl, _ := url.Parse(c.fmtContainerURL())
	require.Equal(t, *expect, *containerUrl)
}

func Test_ConnectionStringWithContainer(t *testing.T) {
	c, err := NewTestBlobStorage(&BlobStorageConfig{
		ContainerName:    "foo",
		ConnectionString: "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=shorter=;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;",
		TenantID:         "",
		ClientID:         "",
		ClientSecret:     flagext.SecretWithValue("test"),
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("http://127.0.0.1:10000/devstoreaccount1/foo")
	containerUrl, _ := url.Parse(c.fmtContainerURL())
	require.Equal(t, *expect, *containerUrl)
}

func Test_DefaultBlobURL(t *testing.T) {
	c, err := NewTestBlobStorage(&BlobStorageConfig{
		ContainerName:      "foo",
		StorageAccountName: "bar",
		Environment:        azureGlobal,
		TenantID:           "",
		ClientID:           "",
		ClientSecret:       flagext.SecretWithValue("test"),
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("https://bar.blob.core.windows.net/foo/blob")
	bloburl, _ := url.Parse(c.fmtBlobURL("blob"))
	require.NoError(t, err)
	require.Equal(t, *expect, *bloburl)
}

func Test_EndpointSuffixWithBlob(t *testing.T) {
	c, err := NewTestBlobStorage(&BlobStorageConfig{
		ContainerName:      "foo",
		StorageAccountName: "bar",
		Environment:        azureGlobal,
		EndpointSuffix:     "test.com",
		TenantID:           "",
		ClientID:           "",
		ClientSecret:       flagext.SecretWithValue("test"),
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("https://bar.test.com/foo/blob")
	bloburl, err := url.Parse(c.fmtBlobURL("blob"))
	require.NoError(t, err)
	require.Equal(t, *expect, *bloburl)
}

func Test_ConnectionStringWithBlob(t *testing.T) {
	c, err := NewTestBlobStorage(&BlobStorageConfig{
		ContainerName:    "foo",
		ConnectionString: "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=shorter=;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;",
		TenantID:         "",
		ClientID:         "",
		ClientSecret:     flagext.SecretWithValue("test"),
	}, metrics, hedging.Config{})
	require.NoError(t, err)
	expect, _ := url.Parse("http://127.0.0.1:10000/devstoreaccount1/foo/blob")
	bloburl, err := url.Parse(c.fmtBlobURL("blob"))
	require.NoError(t, err)
	require.Equal(t, *expect, *bloburl)
}

func Test_ConfigValidation(t *testing.T) {
	t.Run("expected validation error if environment is not supported", func(t *testing.T) {
		cfg := &BlobStorageConfig{
			Environment: "",
		}

		require.EqualError(t, cfg.Validate(), "unsupported Azure blob storage environment: , please select one of: AzureGlobal, AzureChinaCloud, AzureGermanCloud, AzureUSGovernment ")
	})
	t.Run("expected validation error if tenant_id is empty and UseServicePrincipal is enabled", func(t *testing.T) {
		cfg := createServicePrincipalStorageConfig("", "", "")

		require.EqualError(t, cfg.Validate(), "tenant_id is required if authentication using Service Principal is enabled")
	})
	t.Run("expected validation error if client_id is empty and UseServicePrincipal is enabled", func(t *testing.T) {
		cfg := createServicePrincipalStorageConfig("fake_tenant", "", "")

		require.EqualError(t, cfg.Validate(), "client_id is required if authentication using Service Principal is enabled")
	})
	t.Run("expected validation error if client_secret is empty and UseServicePrincipal is enabled", func(t *testing.T) {
		cfg := createServicePrincipalStorageConfig("fake_tenant", "fake_client", "")

		require.EqualError(t, cfg.Validate(), "client_secret is required if authentication using Service Principal is enabled")
	})
	t.Run("expected no errors if UseServicePrincipal is enabled and required fields are set", func(t *testing.T) {
		cfg := createServicePrincipalStorageConfig("fake_tenant", "fake_client", "fake_secret")

		require.NoError(t, cfg.Validate())
	})
	t.Run("expected no errors if UseServicePrincipal is disabled and fields are empty", func(t *testing.T) {
		cfg := &BlobStorageConfig{
			Environment:         azureGlobal,
			UseServicePrincipal: false,
		}

		require.NoError(t, cfg.Validate())
	})
}

func createServicePrincipalStorageConfig(tenantID string, clientID string, clientSecret string) *BlobStorageConfig {
	return &BlobStorageConfig{
		Environment:         azureGlobal,
		UseServicePrincipal: true,
		TenantID:            tenantID,
		ClientID:            clientID,
		ClientSecret:        flagext.SecretWithValue(clientSecret),
	}
}
