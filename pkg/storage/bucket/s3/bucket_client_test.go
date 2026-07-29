package s3

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	bucket_http "github.com/grafana/loki/v3/pkg/storage/bucket/http"
)

func TestNewBaseHTTPTransport_PreservesTLSConfig(t *testing.T) {
	caPath := writeSelfSignedCA(t)

	cfg := Config{
		HTTP: bucket_http.Config{
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   100,
			TLSConfig: bucket_http.TLSConfig{
				CAPath:     caPath,
				ServerName: "example.com",
			},
		},
	}

	rt, err := NewBaseHTTPTransport(cfg)
	require.NoError(t, err)
	require.NotNil(t, rt)

	transport, ok := rt.(*http.Transport)
	require.True(t, ok, "expected *http.Transport, got %T", rt)
	require.NotNil(t, transport.TLSClientConfig)
	require.NotNil(t, transport.TLSClientConfig.RootCAs, "RootCAs must be populated from CAPath; otherwise hedged S3 requests bypass TLS config — see #21854")
	require.Equal(t, "example.com", transport.TLSClientConfig.ServerName)
	require.False(t, transport.TLSClientConfig.InsecureSkipVerify)
	require.Equal(t, 90*time.Second, transport.IdleConnTimeout)
	require.Equal(t, 10*time.Second, transport.TLSHandshakeTimeout)
}

func TestNewBaseHTTPTransport_InsecureSkipVerify(t *testing.T) {
	cfg := Config{
		HTTP: bucket_http.Config{
			InsecureSkipVerify: true,
		},
	}

	rt, err := NewBaseHTTPTransport(cfg)
	require.NoError(t, err)

	transport := rt.(*http.Transport)
	require.True(t, transport.TLSClientConfig.InsecureSkipVerify)
}

func TestNewBaseHTTPTransport_InvalidCAPath(t *testing.T) {
	cfg := Config{
		HTTP: bucket_http.Config{
			TLSConfig: bucket_http.TLSConfig{
				CAPath: "/does/not/exist.crt",
			},
		},
	}

	_, err := NewBaseHTTPTransport(cfg)
	require.Error(t, err)
}

// writeSelfSignedCA generates a throwaway self-signed CA certificate and writes it to a
// temp file, returning the file path. The temp file is cleaned up at test end.
func writeSelfSignedCA(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	dir := t.TempDir()
	path := filepath.Join(dir, "ca.crt")
	require.NoError(t, os.WriteFile(path, pemBytes, 0600))
	return path
}
