package passthroughgateway

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
)

type TLSConfig struct {
	MinVersion   string
	MaxVersion   string
	CipherSuites []string
	CurvePrefs   []string
	ClientAuth   string
}

var tlsClientAuthTypes = map[string]tls.ClientAuthType{
	"NoClientCert":               tls.NoClientCert,
	"RequestClientCert":          tls.RequestClientCert,
	"RequireAnyClientCert":       tls.RequireAnyClientCert,
	"VerifyClientCertIfGiven":    tls.VerifyClientCertIfGiven,
	"RequireAndVerifyClientCert": tls.RequireAndVerifyClientCert,
}

var tlsVersions = map[string]uint16{
	"VersionTLS10": tls.VersionTLS10,
	"VersionTLS11": tls.VersionTLS11,
	"VersionTLS12": tls.VersionTLS12,
	"VersionTLS13": tls.VersionTLS13,
}

var tlsCipherSuites = map[string]uint16{
	// TLS 1.2 cipher suites
	"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
	"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA":            tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
	"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA":            tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
	"TLS_RSA_WITH_AES_128_GCM_SHA256":               tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
	"TLS_RSA_WITH_AES_256_GCM_SHA384":               tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	"TLS_RSA_WITH_AES_128_CBC_SHA256":               tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
	// TLS 1.3 cipher suites (always enabled when TLS 1.3 is used)
	"TLS_AES_128_GCM_SHA256":       tls.TLS_AES_128_GCM_SHA256,
	"TLS_AES_256_GCM_SHA384":       tls.TLS_AES_256_GCM_SHA384,
	"TLS_CHACHA20_POLY1305_SHA256": tls.TLS_CHACHA20_POLY1305_SHA256,
}

var tlsCurveIDs = map[string]tls.CurveID{
	"X25519":    tls.X25519,
	"CurveP256": tls.CurveP256,
	"CurveP384": tls.CurveP384,
	"CurveP521": tls.CurveP521,
}

// BuildServerTLSConfig creates a TLS config for the server with optional mTLS.
func BuildServerTLSConfig(cfg *TLSConfig, clientCAFile string) (*tls.Config, error) {
	tlsConfig, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	if clientCAFile != "" {
		caCert, err := os.ReadFile(clientCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read client CA file: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, kverrors.New("failed to append client CA certs")
		}

		tlsConfig.ClientCAs = caCertPool
	}

	return tlsConfig, nil
}

// BuildServerTLSConfigWithClientAuth creates a TLS config for the server with optional client authentication.
func BuildServerTLSConfigWithClientAuth(cfg *TLSConfig, clientCAFile string) (*tls.Config, error) {
	tlsConfig, err := BuildServerTLSConfig(cfg, clientCAFile)
	if err != nil {
		return nil, err
	}

	if clientCAFile != "" {
		clientAuth, err := parseClientAuthType(cfg.ClientAuth)
		if err != nil {
			return nil, err
		}
		tlsConfig.ClientAuth = clientAuth
	}

	return tlsConfig, nil
}

// BuildUpstreamTLSConfig creates a TLS config for upstream connections with optional client certificate.
func BuildUpstreamTLSConfig(cfg *TLSConfig, upstreamCAFile, upstreamCertFile, upstreamKeyFile string) (*tls.Config, error) {
	tlsConfig, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, err
	}

	// Load upstream CA for server verification
	if upstreamCAFile != "" {
		caCert, err := os.ReadFile(upstreamCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read upstream CA file: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, kverrors.New("failed to append upstream CA certs")
		}
		tlsConfig.RootCAs = caCertPool
	}

	// Load client certificate for upstream mTLS
	if upstreamCertFile != "" && upstreamKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(upstreamCertFile, upstreamKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load upstream client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

func parseClientAuthType(authType string) (tls.ClientAuthType, error) {
	if authType == "" {
		return tls.RequireAndVerifyClientCert, nil
	}
	auth, ok := tlsClientAuthTypes[authType]
	if !ok {
		return 0, kverrors.New("unknown client auth type", "type", authType)
	}
	return auth, nil
}

func buildTLSConfig(cfg *TLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{}

	tlsConfig.MinVersion = tls.VersionTLS12
	if cfg.MinVersion != "" {
		minVersion, err := parseTLSVersion(cfg.MinVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid min TLS version: %w", err)
		}
		tlsConfig.MinVersion = minVersion
	}

	if cfg.MaxVersion != "" {
		maxVersion, err := parseTLSVersion(cfg.MaxVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid max TLS version: %w", err)
		}
		tlsConfig.MaxVersion = maxVersion
	}

	if len(cfg.CipherSuites) > 0 {
		cipherSuites, err := parseCipherSuites(cfg.CipherSuites)
		if err != nil {
			return nil, fmt.Errorf("invalid cipher suites: %w", err)
		}
		tlsConfig.CipherSuites = cipherSuites
	}

	if len(cfg.CurvePrefs) > 0 {
		curvePrefs, err := parseCurvePreferences(cfg.CurvePrefs)
		if err != nil {
			return nil, fmt.Errorf("invalid curve preferences: %w", err)
		}
		tlsConfig.CurvePreferences = curvePrefs
	}

	return tlsConfig, nil
}

func parseTLSVersion(version string) (uint16, error) {
	if version == "" {
		return 0, nil
	}
	v, ok := tlsVersions[version]
	if !ok {
		return 0, kverrors.New("unknown TLS version", "version", version)
	}
	return v, nil
}

func parseCipherSuites(ciphers []string) ([]uint16, error) {
	if len(ciphers) == 0 {
		return nil, nil
	}
	result := make([]uint16, 0, len(ciphers))
	for _, name := range ciphers {
		id, ok := tlsCipherSuites[strings.TrimSpace(name)]
		if !ok {
			return nil, kverrors.New("unknown cipher suite", "cipher", name)
		}
		result = append(result, id)
	}
	return result, nil
}

func parseCurvePreferences(curves []string) ([]tls.CurveID, error) {
	if len(curves) == 0 {
		return nil, nil
	}
	result := make([]tls.CurveID, 0, len(curves))
	for _, name := range curves {
		id, ok := tlsCurveIDs[strings.TrimSpace(name)]
		if !ok {
			return nil, kverrors.New("unknown curve", "curve", name)
		}
		result = append(result, id)
	}
	return result, nil
}
