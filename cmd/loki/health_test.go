package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetTLSConfig_NoFlags(t *testing.T) {
	cfg, err := getTLSConfig([]string{})
	require.NoError(t, err)
	require.Nil(t, cfg)
}

func TestGetTLSConfig_CertWithoutKey(t *testing.T) {
	_, err := getTLSConfig([]string{"-health.tls.cert=foo.crt"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "both health.tls.cert and health.tls.key must be set")
}

func TestGetTLSConfig_SkipVerify(t *testing.T) {
	cfg, err := getTLSConfig([]string{"-health.tls.skip-verify"})
	require.NoError(t, err)
	require.NotNil(t, cfg)
	require.True(t, cfg.InsecureSkipVerify)
}

func TestGetTLSConfig_InvalidCAPath(t *testing.T) {
	_, err := getTLSConfig([]string{"-health.tls.ca=/nonexistent/ca.crt"})
	require.Error(t, err)
}
