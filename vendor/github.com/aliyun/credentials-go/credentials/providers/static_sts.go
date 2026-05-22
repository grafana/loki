package providers

import (
	"errors"
	"os"
)

type StaticSTSCredentialsProvider struct {
	accessKeyId     string
	accessKeySecret string
	securityToken   string
}

type StaticSTSCredentialsProviderBuilder struct {
	provider *StaticSTSCredentialsProvider
}

func NewStaticSTSCredentialsProviderBuilder() *StaticSTSCredentialsProviderBuilder {
	return &StaticSTSCredentialsProviderBuilder{
		provider: &StaticSTSCredentialsProvider{},
	}
}

func (builder *StaticSTSCredentialsProviderBuilder) WithAccessKeyId(accessKeyId string) *StaticSTSCredentialsProviderBuilder {
	builder.provider.accessKeyId = accessKeyId
	return builder
}

func (builder *StaticSTSCredentialsProviderBuilder) WithAccessKeySecret(accessKeySecret string) *StaticSTSCredentialsProviderBuilder {
	builder.provider.accessKeySecret = accessKeySecret
	return builder
}

func (builder *StaticSTSCredentialsProviderBuilder) WithSecurityToken(securityToken string) *StaticSTSCredentialsProviderBuilder {
	builder.provider.securityToken = securityToken
	return builder
}

func (builder *StaticSTSCredentialsProviderBuilder) Build() (provider *StaticSTSCredentialsProvider, err error) {
	if builder.provider.accessKeyId == "" {
		builder.provider.accessKeyId = os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_ID")
	}

	if builder.provider.accessKeyId == "" {
		err = errors.New("the access key id is empty")
		return
	}

	if builder.provider.accessKeySecret == "" {
		builder.provider.accessKeySecret = os.Getenv("ALIBABA_CLOUD_ACCESS_KEY_SECRET")
	}

	if builder.provider.accessKeySecret == "" {
		err = errors.New("the access key secret is empty")
		return
	}

	if builder.provider.securityToken == "" {
		builder.provider.securityToken = os.Getenv("ALIBABA_CLOUD_SECURITY_TOKEN")
	}

	if builder.provider.securityToken == "" {
		err = errors.New("the security token is empty")
		return
	}

	provider = builder.provider
	return
}

func (provider *StaticSTSCredentialsProvider) GetCredentials() (cc *Credentials, err error) {
	cc = &Credentials{
		AccessKeyId:     provider.accessKeyId,
		AccessKeySecret: provider.accessKeySecret,
		SecurityToken:   provider.securityToken,
		ProviderName:    provider.GetProviderName(),
	}
	return
}

func (provider *StaticSTSCredentialsProvider) GetProviderName() string {
	return "static_sts"
}
