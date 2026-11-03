package azure

import (
	"net/http"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/azure"
)

// captureFactory returns a factory function that records the azure.Config it
// receives so the test can inspect what newBucketClient passed through.
func captureFactory(captured *azure.Config) func(log.Logger, azure.Config, string, func(http.RoundTripper) http.RoundTripper) (*azure.Bucket, error) {
	return func(_ log.Logger, cfg azure.Config, _ string, _ func(http.RoundTripper) http.RoundTripper) (*azure.Bucket, error) {
		*captured = cfg
		// Return a nil bucket — the tests only care about the config that was
		// handed to the factory, not the bucket itself.
		return nil, nil
	}
}

// TestNewBucketClientTLSConfig verifies that TLS fields set on the Loki Config
// are forwarded to the upstream azure.Config.HTTPConfig.TLSConfig.
func TestNewBucketClientTLSConfig(t *testing.T) {
	cfg := Config{
		ChunkDelimiter: "-",
	}
	cfg.HTTP.TLSConfig.CAPath = "/etc/ssl/ca.crt"
	cfg.HTTP.TLSConfig.CertPath = "/etc/ssl/client.crt"
	cfg.HTTP.TLSConfig.KeyPath = "/etc/ssl/client.key"
	cfg.HTTP.TLSConfig.ServerName = "my-azure-endpoint"

	var captured azure.Config
	_, err := newBucketClient(cfg, "test", log.NewNopLogger(), nil, captureFactory(&captured))
	require.NoError(t, err)

	require.Equal(t, "/etc/ssl/ca.crt", captured.HTTPConfig.TLSConfig.CAFile)
	require.Equal(t, "/etc/ssl/client.crt", captured.HTTPConfig.TLSConfig.CertFile)
	require.Equal(t, "/etc/ssl/client.key", captured.HTTPConfig.TLSConfig.KeyFile)
	require.Equal(t, "my-azure-endpoint", captured.HTTPConfig.TLSConfig.ServerName)
}

// TestNewBucketClientDefaultsUnchanged verifies that when no http_config fields
// are set, the TLS config forwarded to the upstream azure.Config is empty —
// i.e. this PR doesn't silently change behaviour for existing deployments.
func TestNewBucketClientDefaultsUnchanged(t *testing.T) {
	cfg := Config{
		ChunkDelimiter: "-",
	}
	// Deliberately leave cfg.HTTP at zero value.

	var captured azure.Config
	_, err := newBucketClient(cfg, "test", log.NewNopLogger(), nil, captureFactory(&captured))
	require.NoError(t, err)

	require.Empty(t, captured.HTTPConfig.TLSConfig.CAFile)
	require.Empty(t, captured.HTTPConfig.TLSConfig.CertFile)
	require.Empty(t, captured.HTTPConfig.TLSConfig.KeyFile)
	require.Empty(t, captured.HTTPConfig.TLSConfig.ServerName)
}

// Compile-time check: newBucketClient returns an objstore.Bucket, so the
// captureFactory signature must satisfy the factory parameter type.
var _ func(log.Logger, azure.Config, string, func(http.RoundTripper) http.RoundTripper) (*azure.Bucket, error) = captureFactory(nil)
var _ objstore.Bucket // keep objstore imported
