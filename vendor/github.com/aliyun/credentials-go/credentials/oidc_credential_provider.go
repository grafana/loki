package credentials

import (
	"os"

	"github.com/alibabacloud-go/tea/tea"
)

type oidcCredentialsProvider struct{}

var providerOIDC = new(oidcCredentialsProvider)

func newOidcCredentialsProvider() Provider {
	return &oidcCredentialsProvider{}
}

func (p *oidcCredentialsProvider) resolve() (*Config, error) {
	roleArn, ok1 := os.LookupEnv(ENVRoleArn)
	oidcProviderArn, ok2 := os.LookupEnv(ENVOIDCProviderArn)
	oidcToken, okToken := os.LookupEnv(ENVOIDCToken)
	oidcTokenFilePath, okFile := os.LookupEnv(ENVOIDCTokenFile)
	if !ok1 || !ok2 || (!okToken && !okFile) {
		return nil, nil
	}

	config := &Config{
		Type:            tea.String("oidc_role_arn"),
		RoleArn:         tea.String(roleArn),
		OIDCProviderArn: tea.String(oidcProviderArn),
		RoleSessionName: tea.String("defaultSessionName"),
	}
	if oidcToken != "" {
		config.OIDCToken = tea.String(oidcToken)
	} else {
		config.OIDCTokenFilePath = tea.String(oidcTokenFilePath)
	}
	roleSessionName, ok := os.LookupEnv(ENVRoleSessionName)
	if ok {
		config.RoleSessionName = tea.String(roleSessionName)
	}
	return config, nil
}
