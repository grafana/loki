package ruler

import (
	"flag"
	"net/url"
	"reflect"
	"testing"

	commonconfig "github.com/prometheus/common/config"
	promconfig "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/storage/remote/azuread"
	"github.com/prometheus/sigv4"
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

func mustURL(t *testing.T, raw string) *commonconfig.URL {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	return &commonconfig.URL{URL: u}
}

var secretType = reflect.TypeOf(commonconfig.Secret(""))

// collectSecretPaths walks rt's type graph (structs, pointers, maps, slices) and records the dotted path of every commonconfig.Secret field it can reach.
func collectSecretPaths(rt reflect.Type, path string, depth int, out map[string]bool) {
	if depth > 30 {
		panic("collectSecretPaths: depth exceeded at " + path + " -- possible type cycle")
	}
	if rt == secretType {
		out[path] = true
		return
	}
	switch rt.Kind() { //nolint:exhaustive
	case reflect.Ptr:
		collectSecretPaths(rt.Elem(), path, depth+1, out)
	case reflect.Struct:
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.PkgPath != "" { // unexported
				continue
			}
			collectSecretPaths(f.Type, path+"."+f.Name, depth+1, out)
		}
	case reflect.Map:
		collectSecretPaths(rt.Elem(), path+"[]", depth+1, out)
	case reflect.Slice:
		collectSecretPaths(rt.Elem(), path+"[]", depth+1, out)
	}
}

// collectNonEmptySecretPaths walks a fixture's actual values in the same shape as collectSecretPaths, recording which paths hold a non-empty Secret, without mutating anything.
func collectNonEmptySecretPaths(v reflect.Value, path string, out map[string]bool) {
	if v.Type() == secretType {
		if v.Len() > 0 {
			out[path] = true
		}
		return
	}
	switch v.Kind() { //nolint:exhaustive
	case reflect.Ptr:
		if v.IsNil() {
			return
		}
		collectNonEmptySecretPaths(v.Elem(), path, out)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if f.PkgPath != "" {
				continue
			}
			collectNonEmptySecretPaths(v.Field(i), path+"."+f.Name, out)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			collectNonEmptySecretPaths(v.MapIndex(k), path+"[]", out)
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			collectNonEmptySecretPaths(v.Index(i), path+"[]", out)
		}
	}
}

// TestRemoteWriteConfigClone_RestoresSecrets reflects over RemoteWriteConfig's type graph to find every reachable Secret field, confirms the fixtures below set a value at each one, then asserts Clone preserves every one of them -- so a new Secret field added upstream fails this test immediately instead of passing silently, the way azuread.OAuthConfig.ClientSecret once did against a hand-picked field list.
func TestRemoteWriteConfigClone_RestoresSecrets(t *testing.T) {
	// DefaultRemoteWriteConfig is the baseline UnmarshalYAML applies, so orig and cloned start equal.
	newFixture := func(rawURL string) promconfig.RemoteWriteConfig {
		c := promconfig.DefaultRemoteWriteConfig
		c.URL = mustURL(t, rawURL)
		return c
	}

	basicAuth := newFixture("http://basic-auth.example.com")
	basicAuth.HTTPClientConfig.BasicAuth = &commonconfig.BasicAuth{Username: "user", Password: "secret-basic-auth-password"}
	basicAuth.HTTPClientConfig.TLSConfig = commonconfig.TLSConfig{Cert: "dummy-cert", Key: "secret-tls-client-key"}
	basicAuth.HTTPClientConfig.ProxyFromEnvironment = true
	basicAuth.HTTPClientConfig.ProxyConnectHeader = map[string][]commonconfig.Secret{"Authorization": {"secret-proxy-connect-header"}}
	basicAuth.HTTPClientConfig.HTTPHeaders = &commonconfig.Headers{Headers: map[string]commonconfig.Header{
		"X-Custom": {Secrets: []commonconfig.Secret{"secret-custom-header"}},
	}}

	authorization := newFixture("http://authorization.example.com")
	authorization.HTTPClientConfig.Authorization = &commonconfig.Authorization{Type: "Bearer", Credentials: "secret-authorization-credentials"}

	oauth2 := newFixture("http://oauth2.example.com")
	oauth2.HTTPClientConfig.OAuth2 = &commonconfig.OAuth2{
		ClientID:             "client-id",
		ClientSecret:         "secret-oauth2-client-secret",
		ClientCertificateKey: "secret-oauth2-client-certificate-key",
		GrantType:            "urn:ietf:params:oauth:grant-type:jwt-bearer",
		TokenURL:             "http://oauth2.example.com/token",
		TLSConfig:            commonconfig.TLSConfig{Cert: "dummy-cert", Key: "secret-oauth2-tls-key"},
		ProxyConfig: commonconfig.ProxyConfig{
			ProxyFromEnvironment: true,
			ProxyConnectHeader:   map[string][]commonconfig.Secret{"Authorization": {"secret-oauth2-proxy-connect-header"}},
		},
	}

	sigV4 := newFixture("http://sigv4.example.com")
	sigV4.SigV4Config = &sigv4.SigV4Config{AccessKey: "access-key", SecretKey: "secret-sigv4-secret-key"}

	azureADOAuth := newFixture("http://azuread-oauth.example.com")
	azureADOAuth.AzureADConfig = &azuread.AzureADConfig{
		Cloud: azuread.AzurePublic,
		OAuth: &azuread.OAuthConfig{
			ClientID:     "11111111-1111-1111-1111-111111111111",
			ClientSecret: "secret-azuread-oauth-client-secret",
			TenantID:     "tenant-id",
		},
	}

	azureADCert := newFixture("http://azuread-cert.example.com")
	azureADCert.AzureADConfig = &azuread.AzureADConfig{
		Cloud: azuread.AzurePublic,
		Certificate: &azuread.CertificateConfig{
			ClientID:            "11111111-1111-1111-1111-111111111111",
			TenantID:            "tenant-id",
			CertificatePath:     "dummy-cert-path",
			CertificatePassword: "secret-azuread-certificate-password",
		},
	}

	fixtures := map[string]promconfig.RemoteWriteConfig{
		"basic-auth":    basicAuth,
		"authorization": authorization,
		"oauth2":        oauth2,
		"sigv4":         sigV4,
		"azuread-oauth": azureADOAuth,
		"azuread-cert":  azureADCert,
	}

	// bearer_token migrates fields on clone (see TestRemoteWriteConfigClone_RestoresMigratedBearerToken), so it's covered for the manifest below but skipped from the deep-equality loop.
	bearerToken := newFixture("http://bearer-token.example.com")
	bearerToken.HTTPClientConfig.BearerToken = "secret-bearer-token"

	manifest := map[string]bool{}
	collectSecretPaths(reflect.TypeOf(promconfig.RemoteWriteConfig{}), "RemoteWriteConfig", 0, manifest)

	covered := map[string]bool{}
	for _, f := range fixtures {
		collectNonEmptySecretPaths(reflect.ValueOf(f), "RemoteWriteConfig", covered)
	}
	collectNonEmptySecretPaths(reflect.ValueOf(bearerToken), "RemoteWriteConfig", covered)

	for path := range manifest {
		assert.True(t, covered[path], "no fixture sets a value for reflection-discovered secret path %s; add one", path)
	}

	orig := &RemoteWriteConfig{Clients: fixtures}

	cloned, err := orig.Clone()
	require.NoError(t, err)

	for id := range fixtures {
		assert.Equal(t, orig.Clients[id], cloned.Clients[id], "Clients[%q]", id)
	}
}

// TestRemoteWriteConfigClone_DoesNotAliasContainers guards against a container-aliasing bug a plain "dst.X = src.X" map/slice restore would reintroduce, since getTenantRemoteWriteConfig later merges per-tenant overrides into a clone in place via mergo, which would otherwise mutate the shared base config.
func TestRemoteWriteConfigClone_DoesNotAliasContainers(t *testing.T) {
	orig := &RemoteWriteConfig{
		Clients: map[string]promconfig.RemoteWriteConfig{
			"default": {
				URL: mustURL(t, "http://alias.example.com"),
				HTTPClientConfig: commonconfig.HTTPClientConfig{
					ProxyConfig: commonconfig.ProxyConfig{
						ProxyFromEnvironment: true,
						ProxyConnectHeader:   map[string][]commonconfig.Secret{"Authorization": {"secret-proxy-header"}},
					},
					HTTPHeaders: &commonconfig.Headers{Headers: map[string]commonconfig.Header{
						"X-Custom": {Secrets: []commonconfig.Secret{"secret-custom-header"}},
					}},
				},
			},
		},
	}

	cloned, err := orig.Clone()
	require.NoError(t, err)

	clonedClient := cloned.Clients["default"]
	clonedClient.HTTPClientConfig.ProxyConnectHeader["Authorization"][0] = "mutated"
	clonedClient.HTTPClientConfig.ProxyConnectHeader["injected"] = []commonconfig.Secret{"injected"}
	header := clonedClient.HTTPClientConfig.HTTPHeaders.Headers["X-Custom"]
	header.Secrets[0] = "mutated"
	clonedClient.HTTPClientConfig.HTTPHeaders.Headers["X-Custom"] = header

	origClient := orig.Clients["default"]
	assert.Equal(t, commonconfig.Secret("secret-proxy-header"), origClient.HTTPClientConfig.ProxyConnectHeader["Authorization"][0])
	assert.NotContains(t, origClient.HTTPClientConfig.ProxyConnectHeader, "injected")
	assert.Equal(t, commonconfig.Secret("secret-custom-header"), origClient.HTTPClientConfig.HTTPHeaders.Headers["X-Custom"].Secrets[0])
}

// TestRemoteWriteConfigClone_RestoresMigratedBearerToken covers the one Secret field Clone can't restore in place: HTTPClientConfig.Validate migrates a bare bearer_token into authorization.credentials during Clone's YAML unmarshal, so the secret ends up in a different field than the one it started in.
func TestRemoteWriteConfigClone_RestoresMigratedBearerToken(t *testing.T) {
	bearerToken := promconfig.DefaultRemoteWriteConfig
	bearerToken.URL = mustURL(t, "http://bearer-token.example.com")
	bearerToken.HTTPClientConfig.BearerToken = "secret-bearer-token"

	orig := &RemoteWriteConfig{Clients: map[string]promconfig.RemoteWriteConfig{"bearer-token": bearerToken}}

	cloned, err := orig.Clone()
	require.NoError(t, err)

	clt := cloned.Clients["bearer-token"]
	require.NotNil(t, clt.HTTPClientConfig.Authorization)
	assert.Equal(t, commonconfig.Secret("secret-bearer-token"), clt.HTTPClientConfig.Authorization.Credentials)
}
