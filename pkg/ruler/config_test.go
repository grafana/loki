package ruler

import (
	"flag"
	"net/url"
	"testing"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/storage/remote/azuread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteWriteConfig(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ExitOnError)

	r := RemoteWriteConfig{}
	r.RegisterFlags(fs)

	// assert new multi clients config is backward compatible if with single tenant.
	// if not provided
	assert.NotNil(t, r.Clients)
}

// TestRemoteWriteConfigClone_RestoresAzureADClientSecret guards against azuread.OAuthConfig.ClientSecret
// being obfuscated by Clone's YAML round-trip once that field becomes Secret-typed, as it already is on
// Loki main and in Grafana's CVE-2026-42151 backport.
func TestRemoteWriteConfigClone_RestoresAzureADClientSecret(t *testing.T) {
	remoteURL, err := url.Parse("http://azuread.example.com")
	require.NoError(t, err)

	azureADClient := config.RemoteWriteConfig{
		URL: &config_util.URL{URL: remoteURL},
		AzureADConfig: &azuread.AzureADConfig{
			Cloud: azuread.AzurePublic,
			OAuth: &azuread.OAuthConfig{
				ClientID:     "11111111-1111-1111-1111-111111111111",
				ClientSecret: "secret-azuread-client-secret",
				TenantID:     "tenant-id",
			},
		},
	}

	orig := &RemoteWriteConfig{
		Client: &azureADClient,
		Clients: map[string]config.RemoteWriteConfig{
			"azuread": azureADClient,
		},
	}

	cloned, err := orig.Clone()
	require.NoError(t, err)

	require.NotNil(t, cloned.Client.AzureADConfig)
	require.NotNil(t, cloned.Client.AzureADConfig.OAuth)
	// The string conversion is a no-op today, but keeps this assertion working once
	// ClientSecret's underlying type changes from string to a Secret-like named string type.
	assert.Equal(t, "secret-azuread-client-secret", string(cloned.Client.AzureADConfig.OAuth.ClientSecret)) //nolint:unconvert

	require.NotNil(t, cloned.Clients["azuread"].AzureADConfig)
	require.NotNil(t, cloned.Clients["azuread"].AzureADConfig.OAuth)
	assert.Equal(t, "secret-azuread-client-secret", string(cloned.Clients["azuread"].AzureADConfig.OAuth.ClientSecret)) //nolint:unconvert
}
