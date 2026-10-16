package client

import (
	"os"
	"testing"

	"github.com/alecthomas/kingpin/v2"
	"github.com/stretchr/testify/require"
)

// settingEnvs are all environment variables that influence the configuration.
var settingEnvs = []string{
	"LOKI_ADDR", "LOKI_ADDRESS",
	"LOKI_ORG_ID", "LOKI_TENANT_ID",
	"LOKI_USERNAME", "LOKI_API_USER",
	"LOKI_PASSWORD", "LOKI_API_KEY",
	"LOKI_BEARER_TOKEN", "LOKI_AUTH_TOKEN", "LOKI_BEARER_TOKEN_FILE",
	"LOKI_CA_CERT_PATH", "LOKI_TLS_CA_PATH",
	"LOKI_CLIENT_CERT_PATH", "LOKI_TLS_CERT_PATH",
	"LOKI_CLIENT_KEY_PATH", "LOKI_TLS_KEY_PATH",
	"LOKI_TLS_SKIP_VERIFY", "LOKI_AUTH_HEADER", "LOKI_QUERY_TAGS", "LOKI_NO_CACHE",
	"LOKI_CLIENT_RETRIES", "LOKI_CLIENT_MIN_BACKOFF", "LOKI_CLIENT_MAX_BACKOFF",
	"LOKI_HTTP_PROXY_URL", "LOKI_ENV_PROXY", "LOKI_HTTP_COMPRESSION",
}

// TestMain removes the settings from the environment of the test runner, because the client reads them.
func TestMain(m *testing.M) {
	for _, name := range settingEnvs {
		os.Unsetenv(name)
	}
	os.Exit(m.Run())
}

// parseConfig parses the arguments with the environment variables set in env and resolves the configuration.
func parseConfig(t *testing.T, env map[string]string, args ...string) Config {
	t.Helper()

	for _, name := range settingEnvs {
		t.Setenv(name, "")
	}
	for name, value := range env {
		t.Setenv(name, value)
	}

	var cfg Config
	app := kingpin.New("test", "")
	cfg.Register(app)
	_, err := app.Parse(args)
	require.NoError(t, err)

	cfg.Resolve()
	return cfg
}

func TestConfigDefaults(t *testing.T) {
	cfg := parseConfig(t, nil)

	require.Equal(t, "http://localhost:3100", cfg.Address)
	require.Empty(t, cfg.OrgID)
	require.Empty(t, cfg.Username)
	require.Empty(t, cfg.Password)
	require.Empty(t, cfg.BearerToken)
	require.Equal(t, "Authorization", cfg.AuthHeader)
	require.True(t, cfg.EnvProxy)
	require.False(t, cfg.Compress)
	require.Zero(t, cfg.Retries)
	require.NoError(t, cfg.Validate())
}

func TestConfigFlagAliases(t *testing.T) {
	for _, tc := range []struct {
		flags []string
		get   func(Config) string
		want  string
	}{
		{[]string{"--address=a"}, func(c Config) string { return c.Address }, "a"},
		{[]string{"--addr=a"}, func(c Config) string { return c.Address }, "a"},
		{[]string{"--org-id=t"}, func(c Config) string { return c.OrgID }, "t"},
		{[]string{"--id=t"}, func(c Config) string { return c.OrgID }, "t"},
		{[]string{"--username=u"}, func(c Config) string { return c.Username }, "u"},
		{[]string{"--user=u"}, func(c Config) string { return c.Username }, "u"},
		{[]string{"--password=p"}, func(c Config) string { return c.Password }, "p"},
		{[]string{"--pass=p"}, func(c Config) string { return c.Password }, "p"},
		{[]string{"--bearer-token=b"}, func(c Config) string { return c.BearerToken }, "b"},
		{[]string{"--authToken=b"}, func(c Config) string { return c.BearerToken }, "b"},
		{[]string{"--tls-ca-path=ca"}, func(c Config) string { return c.TLS.CAFile }, "ca"},
		{[]string{"--ca-cert=ca"}, func(c Config) string { return c.TLS.CAFile }, "ca"},
		{[]string{"--ca-cert-path=ca"}, func(c Config) string { return c.TLS.CAFile }, "ca"},
		{[]string{"--tls-cert-path=c"}, func(c Config) string { return c.TLS.CertFile }, "c"},
		{[]string{"--cert=c"}, func(c Config) string { return c.TLS.CertFile }, "c"},
		{[]string{"--cert-path=c"}, func(c Config) string { return c.TLS.CertFile }, "c"},
		{[]string{"--tls-key-path=k"}, func(c Config) string { return c.TLS.KeyFile }, "k"},
		{[]string{"--key=k"}, func(c Config) string { return c.TLS.KeyFile }, "k"},
		{[]string{"--key-path=k"}, func(c Config) string { return c.TLS.KeyFile }, "k"},
	} {
		t.Run(tc.flags[0], func(t *testing.T) {
			require.Equal(t, tc.want, tc.get(parseConfig(t, nil, tc.flags...)))
		})
	}
}

func TestConfigEnvVars(t *testing.T) {
	for _, tc := range []struct {
		env  []string
		get  func(Config) string
		want string
	}{
		{[]string{"LOKI_ADDR", "LOKI_ADDRESS"}, func(c Config) string { return c.Address }, "a"},
		{[]string{"LOKI_ORG_ID", "LOKI_TENANT_ID"}, func(c Config) string { return c.OrgID }, "t"},
		{[]string{"LOKI_USERNAME", "LOKI_API_USER"}, func(c Config) string { return c.Username }, "u"},
		{[]string{"LOKI_PASSWORD", "LOKI_API_KEY"}, func(c Config) string { return c.Password }, "p"},
		{[]string{"LOKI_BEARER_TOKEN", "LOKI_AUTH_TOKEN"}, func(c Config) string { return c.BearerToken }, "b"},
		{[]string{"LOKI_CA_CERT_PATH", "LOKI_TLS_CA_PATH"}, func(c Config) string { return c.TLS.CAFile }, "ca"},
		{[]string{"LOKI_CLIENT_CERT_PATH", "LOKI_TLS_CERT_PATH"}, func(c Config) string { return c.TLS.CertFile }, "c"},
		{[]string{"LOKI_CLIENT_KEY_PATH", "LOKI_TLS_KEY_PATH"}, func(c Config) string { return c.TLS.KeyFile }, "k"},
	} {
		for _, name := range tc.env {
			t.Run(name, func(t *testing.T) {
				cfg := parseConfig(t, map[string]string{name: tc.want})
				require.Equal(t, tc.want, tc.get(cfg))
			})
		}

		t.Run(tc.env[0]+" before "+tc.env[1], func(t *testing.T) {
			cfg := parseConfig(t, map[string]string{tc.env[0]: tc.want, tc.env[1]: "other"})
			require.Equal(t, tc.want, tc.get(cfg))
		})
	}
}

func TestConfigPrecedence(t *testing.T) {
	env := map[string]string{"LOKI_ADDR": "env-addr", "LOKI_ADDRESS": "env-address"}

	t.Run("canonical flag over env", func(t *testing.T) {
		require.Equal(t, "flag", parseConfig(t, env, "--address=flag").Address)
	})
	t.Run("alias flag over env", func(t *testing.T) {
		require.Equal(t, "flag", parseConfig(t, env, "--addr=flag").Address)
	})
	t.Run("last flag wins", func(t *testing.T) {
		require.Equal(t, "second", parseConfig(t, nil, "--addr=first", "--address=second").Address)
		require.Equal(t, "second", parseConfig(t, nil, "--address=first", "--addr=second").Address)
	})
	t.Run("resolve is idempotent", func(t *testing.T) {
		cfg := parseConfig(t, env)
		cfg.Resolve()
		require.Equal(t, "env-addr", cfg.Address)
	})
}

func TestConfigEnvProxy(t *testing.T) {
	require.True(t, parseConfig(t, nil).EnvProxy)
	require.False(t, parseConfig(t, nil, "--no-envproxy").EnvProxy)
	require.True(t, parseConfig(t, nil, "--envproxy").EnvProxy)
	require.False(t, parseConfig(t, map[string]string{"LOKI_ENV_PROXY": "false"}).EnvProxy)
}

func TestConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{name: "empty", cfg: Config{}},
		{name: "basic auth", cfg: Config{Username: "u", Password: "p"}},
		{name: "username only", cfg: Config{Username: "u"}},
		{name: "password with org id", cfg: Config{Password: "p", OrgID: "t"}},
		{name: "bearer token", cfg: Config{BearerToken: "b"}},
		{name: "bearer token file", cfg: Config{BearerTokenFile: "f"}},
		{name: "password without username and org id", cfg: Config{Password: "p"}, wantErr: "password requires a username or org-id"},
		{name: "basic auth and bearer token", cfg: Config{Username: "u", Password: "p", BearerToken: "b"}, wantErr: "at most one of HTTP basic auth"},
		{name: "basic auth and bearer token file", cfg: Config{Username: "u", BearerTokenFile: "f"}, wantErr: "at most one of HTTP basic auth"},
		{name: "bearer token and file", cfg: Config{BearerToken: "b", BearerTokenFile: "f"}, wantErr: "at most one of the options"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}
