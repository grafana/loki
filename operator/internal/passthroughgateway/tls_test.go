package passthroughgateway

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildTLSConfig(t *testing.T) {
	tt := []struct {
		desc        string
		cfg         *TLSConfig
		wantErr     bool
		errContains string
		validate    func(t *testing.T, cfg *tls.Config)
	}{
		{
			desc: "default config with TLS 1.2 min version",
			cfg:  &TLSConfig{},
			validate: func(t *testing.T, cfg *tls.Config) {
				require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
			},
		},
		{
			desc: "custom min and max version",
			cfg: &TLSConfig{
				MinVersion: "VersionTLS12",
				MaxVersion: "VersionTLS13",
			},
			validate: func(t *testing.T, cfg *tls.Config) {
				require.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
				require.Equal(t, uint16(tls.VersionTLS13), cfg.MaxVersion)
			},
		},
		{
			desc: "invalid min version",
			cfg: &TLSConfig{
				MinVersion: "TLS99",
			},
			wantErr:     true,
			errContains: "invalid min TLS version",
		},
		{
			desc: "invalid max version",
			cfg: &TLSConfig{
				MaxVersion: "invalid",
			},
			wantErr:     true,
			errContains: "invalid max TLS version",
		},
		{
			desc: "valid cipher suites",
			cfg: &TLSConfig{
				CipherSuites: []string{
					"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
					"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
				},
			},
			validate: func(t *testing.T, cfg *tls.Config) {
				require.Len(t, cfg.CipherSuites, 2)
				require.Contains(t, cfg.CipherSuites, uint16(tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256))
				require.Contains(t, cfg.CipherSuites, uint16(tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384))
			},
		},
		{
			desc: "invalid cipher suite",
			cfg: &TLSConfig{
				CipherSuites: []string{"INVALID_CIPHER"},
			},
			wantErr:     true,
			errContains: "invalid cipher suites",
		},
		{
			desc: "valid curve preferences",
			cfg: &TLSConfig{
				CurvePrefs: []string{"X25519", "CurveP256"},
			},
			validate: func(t *testing.T, cfg *tls.Config) {
				require.Len(t, cfg.CurvePreferences, 2)
				require.Contains(t, cfg.CurvePreferences, tls.X25519)
				require.Contains(t, cfg.CurvePreferences, tls.CurveP256)
			},
		},
		{
			desc: "invalid curve preference",
			cfg: &TLSConfig{
				CurvePrefs: []string{"INVALID_CURVE"},
			},
			wantErr:     true,
			errContains: "invalid curve preferences",
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			cfg, err := buildTLSConfig(tc.cfg)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
				return
			}
			require.NoError(t, err)
			if tc.validate != nil {
				tc.validate(t, cfg)
			}
		})
	}
}

func TestParseTLSVersion(t *testing.T) {
	tt := []struct {
		desc    string
		version string
		want    uint16
		wantErr bool
	}{
		{
			desc:    "empty version returns zero",
			version: "",
			want:    0,
		},
		{
			desc:    "TLS 1.0",
			version: "VersionTLS10",
			want:    tls.VersionTLS10,
		},
		{
			desc:    "TLS 1.2",
			version: "VersionTLS12",
			want:    tls.VersionTLS12,
		},
		{
			desc:    "TLS 1.3",
			version: "VersionTLS13",
			want:    tls.VersionTLS13,
		},
		{
			desc:    "unknown version",
			version: "TLS99",
			wantErr: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := parseTLSVersion(tc.version)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseClientAuthType(t *testing.T) {
	tt := []struct {
		desc     string
		authType string
		want     tls.ClientAuthType
		wantErr  bool
	}{
		{
			desc:     "empty defaults to RequireAndVerifyClientCert",
			authType: "",
			want:     tls.RequireAndVerifyClientCert,
		},
		{
			desc:     "NoClientCert",
			authType: "NoClientCert",
			want:     tls.NoClientCert,
		},
		{
			desc:     "RequestClientCert",
			authType: "RequestClientCert",
			want:     tls.RequestClientCert,
		},
		{
			desc:     "RequireAnyClientCert",
			authType: "RequireAnyClientCert",
			want:     tls.RequireAnyClientCert,
		},
		{
			desc:     "VerifyClientCertIfGiven",
			authType: "VerifyClientCertIfGiven",
			want:     tls.VerifyClientCertIfGiven,
		},
		{
			desc:     "RequireAndVerifyClientCert",
			authType: "RequireAndVerifyClientCert",
			want:     tls.RequireAndVerifyClientCert,
		},
		{
			desc:     "unknown auth type",
			authType: "InvalidAuthType",
			wantErr:  true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := parseClientAuthType(tc.authType)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseCipherSuites(t *testing.T) {
	tt := []struct {
		desc    string
		ciphers []string
		want    []uint16
		wantErr bool
	}{
		{
			desc:    "empty returns nil",
			ciphers: []string{},
			want:    nil,
		},
		{
			desc:    "single cipher",
			ciphers: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"},
			want:    []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		},
		{
			desc:    "multiple ciphers",
			ciphers: []string{"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256", "TLS_AES_128_GCM_SHA256"},
			want:    []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256, tls.TLS_AES_128_GCM_SHA256},
		},
		{
			desc:    "cipher with whitespace",
			ciphers: []string{" TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 "},
			want:    []uint16{tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
		},
		{
			desc:    "unknown cipher",
			ciphers: []string{"UNKNOWN_CIPHER"},
			wantErr: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := parseCipherSuites(tc.ciphers)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestParseCurvePreferences(t *testing.T) {
	tt := []struct {
		desc    string
		curves  []string
		want    []tls.CurveID
		wantErr bool
	}{
		{
			desc:   "empty returns nil",
			curves: []string{},
			want:   nil,
		},
		{
			desc:   "X25519",
			curves: []string{"X25519"},
			want:   []tls.CurveID{tls.X25519},
		},
		{
			desc:   "multiple curves",
			curves: []string{"X25519", "CurveP256", "CurveP384"},
			want:   []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384},
		},
		{
			desc:   "curve with whitespace",
			curves: []string{" CurveP256 "},
			want:   []tls.CurveID{tls.CurveP256},
		},
		{
			desc:    "unknown curve",
			curves:  []string{"UNKNOWN_CURVE"},
			wantErr: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := parseCurvePreferences(tc.curves)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
