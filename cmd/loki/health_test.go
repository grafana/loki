package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loki"
)

func TestGetTLSConfig_NoFlags(t *testing.T) {
	cfg, err := getTLSConfig(parseHealthFlags([]string{}), nil)
	require.NoError(t, err)
	require.Nil(t, cfg)
}

func TestGetTLSConfig_CertWithoutKey(t *testing.T) {
	_, err := getTLSConfig(parseHealthFlags([]string{"-health.tls.cert=foo.crt"}), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "both -health.tls.cert and -health.tls.key must be provided together")
}

func TestGetTLSConfig_SkipVerify(t *testing.T) {
	cfg, err := getTLSConfig(parseHealthFlags([]string{"-health.tls.skip-verify"}), nil)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.True(t, cfg.InsecureSkipVerify)
}

func TestGetTLSConfig_InvalidCAPath(t *testing.T) {
	_, err := getTLSConfig(parseHealthFlags([]string{"-health.tls.ca=/nonexistent/ca.crt"}), nil)
	require.Error(t, err)
}

func TestParseHealthFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected healthFlags
	}{
		{
			name: "no flags",
			args: []string{},
			expected: healthFlags{
				health: false,
			},
		},
		{
			name: "health only",
			args: []string{"-health"},
			expected: healthFlags{
				health: true,
			},
		},
		{
			name: "health with double dashes",
			args: []string{"--health"},
			expected: healthFlags{
				health: true,
			},
		},
		{
			name: "health and url",
			args: []string{"-health", "-health.url=http://localhost:3100"},
			expected: healthFlags{
				health:    true,
				healthURL: "http://localhost:3100",
			},
		},
		{
			name: "health and url with space",
			args: []string{"-health", "-health.url", "http://localhost:3100"},
			expected: healthFlags{
				health:    true,
				healthURL: "http://localhost:3100",
			},
		},
		{
			name: "health with unknown flags",
			args: []string{"-some-unknown-flag", "-health", "-health.url=http://localhost:3100"},
			expected: healthFlags{
				health:    true,
				healthURL: "http://localhost:3100",
			},
		},
		{
			name: "config.expand-env flag",
			args: []string{"-config.expand-env=true"},
			expected: healthFlags{
				configExpandEnv: true,
			},
		},
		{
			name: "health.tls.skip-verify",
			args: []string{"-health.tls.skip-verify"},
			expected: healthFlags{
				skipVerify: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseHealthFlags(tc.args)
			require.Equal(t, tc.expected, *got)
		})
	}
}

func TestGetTLSConfig_ServerAndInconsistent(t *testing.T) {
	// Test inconsistent options
	_, err := getTLSConfig(parseHealthFlags([]string{"-health.tls.skip-verify", "-health.tls.ca=ca.crt"}), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot use both -health.tls.skip-verify and -health.tls.ca")

	// Test server config TLS auto-downgrade avoidance
	serverCfg := &loki.Config{}
	serverCfg.Server.HTTPTLSConfig.TLSCertPath = "server.crt"
	serverCfg.Server.HTTPTLSConfig.ClientCAs = "" // no client CAs

	_, err = getTLSConfig(parseHealthFlags([]string{}), serverCfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "server TLS is enabled but client CA is not configured; use -health.tls.skip-verify to skip verification")

	// With skip-verify explicitly set, it should succeed
	cfg, err := getTLSConfig(parseHealthFlags([]string{"-health.tls.skip-verify"}), serverCfg)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.True(t, cfg.InsecureSkipVerify)
}

func TestLoadServerHealthConfig(t *testing.T) {
	// Create a minimal config file (only server block, no schema or storage config)
	tmpFile, err := os.CreateTemp("", "loki-health-test-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	configContent := `
server:
  http_listen_port: ${TEST_LOKI_PORT}
`
	_, err = tmpFile.WriteString(configContent)
	require.NoError(t, err)
	err = tmpFile.Close()
	require.NoError(t, err)

	os.Setenv("TEST_LOKI_PORT", "9999")
	defer os.Unsetenv("TEST_LOKI_PORT")

	t.Run("fails to parse env variable without expand-env flag", func(t *testing.T) {
		f := &healthFlags{
			configFile:      tmpFile.Name(),
			configExpandEnv: false,
		}
		_, err := loadServerHealthConfig(f)
		require.Error(t, err)
	})

	t.Run("successfully parses and expands env variable with expand-env flag", func(t *testing.T) {
		f := &healthFlags{
			configFile:      tmpFile.Name(),
			configExpandEnv: true,
		}
		cfg, err := loadServerHealthConfig(f)
		require.NoError(t, err)
		require.NotNil(t, cfg)
		require.Equal(t, 9999, cfg.Server.HTTPListenPort)
	})
}

