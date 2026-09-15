package providers

import (
	"fmt"
	"os"
	"strings"
)

type DefaultCredentialsProvider struct {
	providerChain    []CredentialsProvider
	lastUsedProvider CredentialsProvider
}

func NewDefaultCredentialsProvider() (provider *DefaultCredentialsProvider) {
	providers := []CredentialsProvider{}

	// Add static ak or sts credentials provider
	envProvider, err := NewEnvironmentVariableCredentialsProviderBuilder().Build()
	if err == nil {
		providers = append(providers, envProvider)
	}

	// oidc check
	oidcProvider, err := NewOIDCCredentialsProviderBuilder().Build()
	if err == nil {
		providers = append(providers, oidcProvider)
	}

	// cli credentials provider
	cliProfileProvider, err := NewCLIProfileCredentialsProviderBuilder().Build()
	if err == nil {
		providers = append(providers, cliProfileProvider)
	}

	// profile credentials provider
	profileProvider, err := NewProfileCredentialsProviderBuilder().Build()
	if err == nil {
		providers = append(providers, profileProvider)
	}

	// Add IMDS
	ecsRamRoleProvider, err := NewECSRAMRoleCredentialsProviderBuilder().Build()
	if err == nil {
		providers = append(providers, ecsRamRoleProvider)
	}

	// credentials uri
	if os.Getenv("ALIBABA_CLOUD_CREDENTIALS_URI") != "" {
		credentialsUriProvider, err := NewURLCredentialsProviderBuilder().Build()
		if err == nil {
			providers = append(providers, credentialsUriProvider)
		}
	}

	return &DefaultCredentialsProvider{
		providerChain: providers,
	}
}

func (provider *DefaultCredentialsProvider) GetCredentials() (cc *Credentials, err error) {
	if provider.lastUsedProvider != nil {
		inner, err1 := provider.lastUsedProvider.GetCredentials()
		if err1 != nil {
			err = err1
			return
		}

		providerName := inner.ProviderName
		if providerName == "" {
			providerName = provider.lastUsedProvider.GetProviderName()
		}

		cc = &Credentials{
			AccessKeyId:     inner.AccessKeyId,
			AccessKeySecret: inner.AccessKeySecret,
			SecurityToken:   inner.SecurityToken,
			ProviderName:    fmt.Sprintf("%s/%s", provider.GetProviderName(), providerName),
		}
		return
	}

	errors := []string{}
	for _, p := range provider.providerChain {
		provider.lastUsedProvider = p
		inner, errInLoop := p.GetCredentials()
		if errInLoop != nil {
			errors = append(errors, errInLoop.Error())
			// 如果有错误，进入下一个获取过程
			continue
		}

		if inner != nil {
			providerName := inner.ProviderName
			if providerName == "" {
				providerName = p.GetProviderName()
			}
			cc = &Credentials{
				AccessKeyId:     inner.AccessKeyId,
				AccessKeySecret: inner.AccessKeySecret,
				SecurityToken:   inner.SecurityToken,
				ProviderName:    fmt.Sprintf("%s/%s", provider.GetProviderName(), providerName),
			}
			return
		}
	}

	err = fmt.Errorf("unable to get credentials from any of the providers in the chain: %s", strings.Join(errors, ", "))
	return
}

func (provider *DefaultCredentialsProvider) GetProviderName() string {
	return "default"
}
