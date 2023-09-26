package storage

import (
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

// Options is used to configure Loki to integrate with
// supported object storages.
type Options struct {
	Schemas     []lokiv1.ObjectStorageSchema
	SharedStore lokiv1.ObjectStorageSecretType

	Azure        *AzureStorageConfig
	GCS          *GCSStorageConfig
	S3           *S3StorageConfig
	Swift        *SwiftStorageConfig
	AlibabaCloud *AlibabaCloudStorageConfig

	SecretName string
	TLS        *TLSConfig
}

// AzureStorageConfig for Azure storage config
type AzureStorageConfig struct {
	Env         string
	Container   string
	AccountName string
	AccountKey  string
}

// GCSStorageConfig for GCS storage config
type GCSStorageConfig struct {
	Bucket string
}

// S3StorageConfig for S3 storage config
type S3StorageConfig struct {
	Endpoint        string
	Region          string
	Buckets         string
	AccessKeyID     string
	AccessKeySecret string
}

// SwiftStorageConfig for Swift storage config
type SwiftStorageConfig struct {
	AuthURL           string
	Username          string
	UserDomainName    string
	UserDomainID      string
	UserID            string
	Password          string
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
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

// TLSConfig for object storage endpoints. Currently supported only by:
// - S3
type TLSConfig struct {
	CA  string
	Key string
}
