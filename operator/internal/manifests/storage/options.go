package storage

import (
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

// Options is used to configure Loki to integrate with
// supported object storages.
type Options struct {
	Schemas                 []lokiv1.ObjectStorageSchema
	SharedStore             lokiv1.ObjectStorageSecretType
	CredentialMode          lokiv1.CredentialMode
	AllowStructuredMetadata bool

	Azure        *AzureStorageConfig
	GCS          *GCSStorageConfig
	S3           *S3StorageConfig
	Swift        *SwiftStorageConfig
	AlibabaCloud *AlibabaCloudStorageConfig

	SecretName string
	SecretSHA1 string
	TLS        *TLSConfig

	OpenShift OpenShiftOptions
}

// AzureStorageConfig for Azure storage config
type AzureStorageConfig struct {
	Env              string
	Container        string
	EndpointSuffix   string
	Audience         string
	WorkloadIdentity bool
}

// GCSStorageConfig for GCS storage config
type GCSStorageConfig struct {
	Bucket           string
	Audience         string
	WorkloadIdentity bool
}

// S3StorageConfig for S3 storage config
type S3StorageConfig struct {
	Endpoint       string
	Region         string
	Buckets        string
	Audience       string
	STS            bool
	SSE            S3SSEConfig
	ForcePathStyle bool
}

type S3SSEType string

const (
	SSEKMSType S3SSEType = "SSE-KMS"
	SSES3Type  S3SSEType = "SSE-S3"
)

type S3SSEConfig struct {
	Type                 S3SSEType
	KMSKeyID             string
	KMSEncryptionContext string
}

// SwiftStorageConfig for Swift storage config
type SwiftStorageConfig struct {
	AuthURL           string
	UserDomainName    string
	UserDomainID      string
	UserID            string
	DomainID          string
	DomainName        string
	ProjectID         string
	ProjectName       string
	ProjectDomainID   string
	ProjectDomainName string
	Region            string
	Container         string
}

// AlibabaCloudStorageConfig for AlibabaCloud storage config
type AlibabaCloudStorageConfig struct {
	Endpoint string
	Bucket   string
}

// TLSConfig for object storage endpoints. Currently supported only by:
// - S3
type TLSConfig struct {
	CA  string
	Key string
}

type OpenShiftOptions struct {
	Enabled          bool
	CloudCredentials CloudCredentials
}

type CloudCredentials struct {
	SecretName string
	SHA1       string
}

func (o OpenShiftOptions) TokenCCOAuthEnabled() bool {
	return o.CloudCredentials.SecretName != "" && o.CloudCredentials.SHA1 != ""
}
