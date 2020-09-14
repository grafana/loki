package cassandra

import (
	"testing"

	"github.com/gocql/gocql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/util/flagext"
)

func TestConfig_setClusterConfig_noAuth(t *testing.T) {
	cfg := defaultConfig()
	cfg.Auth = false
	require.NoError(t, cfg.Validate())

	cqlCfg := gocql.NewCluster()
	err := cfg.setClusterConfig(cqlCfg)
	require.NoError(t, err)
	assert.Nil(t, cqlCfg.Authenticator)
}

func TestConfig_setClusterConfig_authWithPassword(t *testing.T) {
	cfg := defaultConfig()
	cfg.Auth = true
	cfg.Username = "user"
	cfg.Password = flagext.Secret{Value: "pass"}
	require.NoError(t, cfg.Validate())

	cqlCfg := gocql.NewCluster()
	err := cfg.setClusterConfig(cqlCfg)
	require.NoError(t, err)
	assert.NotNil(t, cqlCfg.Authenticator)
	assert.Equal(t, "user", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Username)
	assert.Equal(t, "pass", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Password)
}

func TestConfig_setClusterConfig_authWithPasswordFile_withoutTrailingNewline(t *testing.T) {
	cfg := defaultConfig()
	cfg.Auth = true
	cfg.Username = "user"
	cfg.PasswordFile = "testdata/password-without-trailing-newline.txt"
	require.NoError(t, cfg.Validate())

	cqlCfg := gocql.NewCluster()
	err := cfg.setClusterConfig(cqlCfg)
	require.NoError(t, err)
	assert.NotNil(t, cqlCfg.Authenticator)
	assert.Equal(t, "user", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Username)
	assert.Equal(t, "pass", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Password)
}

func TestConfig_setClusterConfig_authWithPasswordFile_withTrailingNewline(t *testing.T) {
	cfg := defaultConfig()
	cfg.Auth = true
	cfg.Username = "user"
	cfg.PasswordFile = "testdata/password-with-trailing-newline.txt"
	require.NoError(t, cfg.Validate())

	cqlCfg := gocql.NewCluster()
	err := cfg.setClusterConfig(cqlCfg)
	require.NoError(t, err)
	assert.NotNil(t, cqlCfg.Authenticator)
	assert.Equal(t, "user", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Username)
	assert.Equal(t, "pass", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Password)
}

func TestConfig_setClusterConfig_authWithPasswordAndPasswordFile(t *testing.T) {
	cfg := defaultConfig()
	cfg.Auth = true
	cfg.Username = "user"
	cfg.Password = flagext.Secret{Value: "pass"}
	cfg.PasswordFile = "testdata/password-with-trailing-newline.txt"
	assert.Error(t, cfg.Validate())
}

func TestConfig_setClusterConfig_consistency(t *testing.T) {
	tests := map[string]struct {
		cfg                 Config
		expectedConsistency string
	}{
		"default config should set default consistency": {
			cfg:                 defaultConfig(),
			expectedConsistency: "QUORUM",
		},
		"should honor configured consistency": {
			cfg: func() Config {
				cfg := defaultConfig()
				cfg.Consistency = "LOCAL_QUORUM"
				return cfg
			}(),
			expectedConsistency: "LOCAL_QUORUM",
		},
	}

	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			require.NoError(t, testData.cfg.Validate())

			cqlCfg := gocql.NewCluster()
			err := testData.cfg.setClusterConfig(cqlCfg)
			require.NoError(t, err)
			assert.Equal(t, testData.expectedConsistency, cqlCfg.Consistency.String())
		})
	}
}

func defaultConfig() Config {
	cfg := Config{}
	flagext.DefaultValues(&cfg)
	return cfg
}
