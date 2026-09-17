package ui

import (
	"flag"
	"testing"

	dstls "github.com/grafana/dskit/crypto/tls"
	"github.com/stretchr/testify/require"
)

func TestConfigRegisterFlagsProxyTLS(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		var cfg Config
		fs := flag.NewFlagSet("test-defaults", flag.ContinueOnError)

		cfg.RegisterFlags(fs)

		require.NoError(t, fs.Parse(nil))
		require.False(t, cfg.Proxy.TLSEnabled)
		require.Equal(t, dstls.ClientConfig{}, cfg.Proxy.TLS)
	})

	t.Run("custom TLS flags", func(t *testing.T) {
		var cfg Config
		fs := flag.NewFlagSet("test-custom", flag.ContinueOnError)

		cfg.RegisterFlags(fs)

		args := []string{
			"-ui.proxy.tls-enabled=true",
			"-ui.proxy.tls-cert-path=/tmp/cert",
			"-ui.proxy.tls-key-path=/tmp/key",
			"-ui.proxy.tls-ca-path=/tmp/ca",
			"-ui.proxy.tls-server-name=upstream",
			"-ui.proxy.tls-insecure-skip-verify=true",
			"-ui.proxy.tls-cipher-suites=TLS_RSA_WITH_AES_256_GCM_SHA384",
			"-ui.proxy.tls-min-version=VersionTLS13",
		}
		require.NoError(t, fs.Parse(args))

		require.True(t, cfg.Proxy.TLSEnabled)
		require.Equal(t, "/tmp/cert", cfg.Proxy.TLS.CertPath)
		require.Equal(t, "/tmp/key", cfg.Proxy.TLS.KeyPath)
		require.Equal(t, "/tmp/ca", cfg.Proxy.TLS.CAPath)
		require.Equal(t, "upstream", cfg.Proxy.TLS.ServerName)
		require.True(t, cfg.Proxy.TLS.InsecureSkipVerify)
		require.Equal(t, "TLS_RSA_WITH_AES_256_GCM_SHA384", cfg.Proxy.TLS.CipherSuites)
		require.Equal(t, "VersionTLS13", cfg.Proxy.TLS.MinVersion)
	})
}
