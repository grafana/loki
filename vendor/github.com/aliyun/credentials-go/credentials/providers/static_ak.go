package providers

import (
	"errors"
	"os"
)

type StaticAKCredentialsProvider struct {
	accessKeyId     string
	accessKeySecret string
}

type StaticAKCredentialsProviderBuilder struct {
	provider *StaticAKCredentialsProvider
}

func NewStaticAKCredentialsProviderBuilder() *StaticAKCredentialsProviderBuilder {
	return &StaticAKCredentialsProviderBuilder{
		provider: &StaticAKCredentialsProvider{},
	}
}

func (builder *StaticAKCredentialsProviderBuilder) WithAccessKeyId(accessKeyId string) *StaticAKCredentialsProviderBuilder {
	builder.provider.accessKeyId = accessKeyId
	return builder
}

func (builder *StaticAKCredentialsProviderBuilder) WithAccessKeySecret(accessKeySecret string) *StaticAKCredentialsProviderBuilder {
	builder.provider.accessKeySecret = accessKeySecret
	return builder
}

func (builder *StaticAKCredentialsProviderBuilder) Build() (provider *StaticAKCredentialsProvider, err error) {
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

	provider = builder.provider
	return
}

func (provider *StaticAKCredentialsProvider) GetCredentials() (cc *Credentials, err error) {
	cc = &Credentials{
		AccessKeyId:     provider.accessKeyId,
		AccessKeySecret: provider.accessKeySecret,
		ProviderName:    provider.GetProviderName(),
	}
	return
}

func (provider *StaticAKCredentialsProvider) GetProviderName() string {
	return "static_ak"
}
