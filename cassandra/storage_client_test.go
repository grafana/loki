package cassandra

import (
	"testing"

	"github.com/gocql/gocql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cortexproject/cortex/pkg/util/flagext"
)

func TestConfig_setClusterConfig_noAuth(t *testing.T) {
	cqlCfg := gocql.NewCluster()
	cfg := Config{
		Auth: false,
	}
	require.NoError(t, cfg.Validate())

	err := cfg.setClusterConfig(cqlCfg)
	require.NoError(t, err)
	assert.Nil(t, cqlCfg.Authenticator)
}

func TestConfig_setClusterConfig_authWithPassword(t *testing.T) {
	cqlCfg := gocql.NewCluster()
	cfg := Config{
		Auth:     true,
		Username: "user",
		Password: flagext.Secret{Value: "pass"},
	}
	require.NoError(t, cfg.Validate())

	err := cfg.setClusterConfig(cqlCfg)
	require.NoError(t, err)
	assert.NotNil(t, cqlCfg.Authenticator)
	assert.Equal(t, "user", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Username)
	assert.Equal(t, "pass", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Password)
}

func TestConfig_setClusterConfig_authWithPasswordFile_withoutTrailingNewline(t *testing.T) {
	cqlCfg := gocql.NewCluster()
	cfg := Config{
		Auth:         true,
		Username:     "user",
		PasswordFile: "testdata/password-without-trailing-newline.txt",
	}
	require.NoError(t, cfg.Validate())

	err := cfg.setClusterConfig(cqlCfg)
	require.NoError(t, err)
	assert.NotNil(t, cqlCfg.Authenticator)
	assert.Equal(t, "user", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Username)
	assert.Equal(t, "pass", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Password)
}

func TestConfig_setClusterConfig_authWithPasswordFile_withTrailingNewline(t *testing.T) {
	cqlCfg := gocql.NewCluster()
	cfg := Config{
		Auth:         true,
		Username:     "user",
		PasswordFile: "testdata/password-with-trailing-newline.txt",
	}
	require.NoError(t, cfg.Validate())

	err := cfg.setClusterConfig(cqlCfg)
	require.NoError(t, err)
	assert.NotNil(t, cqlCfg.Authenticator)
	assert.Equal(t, "user", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Username)
	assert.Equal(t, "pass", cqlCfg.Authenticator.(gocql.PasswordAuthenticator).Password)
}

func TestConfig_setClusterConfig_authWithPasswordAndPasswordFile(t *testing.T) {
	cfg := Config{
		Auth:         true,
		Username:     "user",
		Password:     flagext.Secret{Value: "pass"},
		PasswordFile: "testdata/password-with-trailing-newline.txt",
	}
	assert.Error(t, cfg.Validate())
}
