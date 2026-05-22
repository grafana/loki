package credentials

// Environmental virables that may be used by the provider
const (
	ENVCredentialFile          = "ALIBABA_CLOUD_CREDENTIALS_FILE"
	ENVEcsMetadata             = "ALIBABA_CLOUD_ECS_METADATA"
	ENVEcsMetadataIMDSv2Enable = "ALIBABA_CLOUD_ECS_IMDSV2_ENABLE"
	PATHCredentialFile         = "~/.alibabacloud/credentials"
	ENVRoleArn                 = "ALIBABA_CLOUD_ROLE_ARN"
	ENVOIDCProviderArn         = "ALIBABA_CLOUD_OIDC_PROVIDER_ARN"
	ENVOIDCTokenFile           = "ALIBABA_CLOUD_OIDC_TOKEN_FILE"
	ENVRoleSessionName         = "ALIBABA_CLOUD_ROLE_SESSION_NAME"
)

// Provider will be implemented When you want to customize the provider.
type Provider interface {
	resolve() (*Config, error)
}
