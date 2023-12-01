package storage

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// ExtractSecret reads a k8s secret into a manifest object storage struct if valid.
func ExtractSecret(s *corev1.Secret, secretType lokiv1.ObjectStorageSecretType) (*storage.Options, error) {
	var err error
	storageOpts := storage.Options{
		SecretName:  s.Name,
		SharedStore: secretType,
	}

	switch secretType {
	case lokiv1.ObjectStorageSecretAzure:
		storageOpts.Azure, err = extractAzureConfigSecret(s)
	case lokiv1.ObjectStorageSecretGCS:
		storageOpts.GCS, err = extractGCSConfigSecret(s)
	case lokiv1.ObjectStorageSecretS3:
		storageOpts.S3, err = extractS3ConfigSecret(s)
	case lokiv1.ObjectStorageSecretSwift:
		storageOpts.Swift, err = extractSwiftConfigSecret(s)
	case lokiv1.ObjectStorageSecretAlibabaCloud:
		storageOpts.AlibabaCloud, err = extractAlibabaCloudConfigSecret(s)
	default:
		return nil, kverrors.New("unknown secret type", "type", secretType)
	}

	if err != nil {
		return nil, err
	}
	return &storageOpts, nil
}

func extractAzureConfigSecret(s *corev1.Secret) (*storage.AzureStorageConfig, error) {
	// Extract and validate mandatory fields
	env := s.Data[storage.KeyAzureEnvironmentName]
	if len(env) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAzureEnvironmentName)
	}
	container := s.Data[storage.KeyAzureStorageContainerName]
	if len(container) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAzureStorageContainerName)
	}
	name := s.Data[storage.KeyAzureStorageAccountName]
	if len(name) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAzureStorageAccountName)
	}
	key := s.Data[storage.KeyAzureStorageAccountKey]
	if len(key) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAzureStorageAccountKey)
	}

	// Extract and validate optional fields
	endpointSuffix := s.Data[storage.KeyAzureStorageEndpointSuffix]

	return &storage.AzureStorageConfig{
		Env:            string(env),
		Container:      string(container),
		EndpointSuffix: string(endpointSuffix),
	}, nil
}

func extractGCSConfigSecret(s *corev1.Secret) (*storage.GCSStorageConfig, error) {
	// Extract and validate mandatory fields
	bucket := s.Data[storage.KeyGCPStorageBucketName]
	if len(bucket) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyGCPStorageBucketName)
	}

	// Check if google authentication credentials is provided
	keyJSON := s.Data[storage.KeyGCPServiceAccountKeyFilename]
	if len(keyJSON) == 0 {
		return nil, kverrors.New("missing google authentication credentials", "field", storage.KeyGCPServiceAccountKeyFilename)
	}

	return &storage.GCSStorageConfig{
		Bucket: string(bucket),
	}, nil
}

func extractS3ConfigSecret(s *corev1.Secret) (*storage.S3StorageConfig, error) {
	// Extract and validate mandatory fields
	endpoint := s.Data[storage.KeyAWSEndpoint]
	if len(endpoint) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAWSEndpoint)
	}
	buckets := s.Data[storage.KeyAWSBucketNames]
	if len(buckets) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAWSBucketNames)
	}
	id := s.Data[storage.KeyAWSAccessKeyID]
	if len(id) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAWSAccessKeyID)
	}
	secret := s.Data[storage.KeyAWSAccessKeySecret]
	if len(secret) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAWSAccessKeySecret)
	}

	// Extract and validate optional fields
	region := s.Data["region"]

	sseCfg, err := extractS3SSEConfig(s.Data)
	if err != nil {
		return nil, err
	}

	return &storage.S3StorageConfig{
		Endpoint: string(endpoint),
		Buckets:  string(buckets),
		Region:   string(region),
		SSE:      sseCfg,
	}, nil
}

func extractS3SSEConfig(d map[string][]byte) (storage.S3SSEConfig, error) {
	var (
		sseType                    storage.S3SSEType
		kmsKeyId, kmsEncryptionCtx string
	)

	switch sseType = storage.S3SSEType(d[storage.KeyAWSSSEType]); sseType {
	case storage.SSEKMSType:
		kmsEncryptionCtx = string(d[storage.KeyAWSSSEKMSEncryptionContext])
		kmsKeyId = string(d[storage.KeyAWSSSEKMSKeyID])
		if kmsKeyId == "" {
			return storage.S3SSEConfig{}, kverrors.New("missing secret field", "field", storage.KeyAWSSSEKMSKeyID)
		}

	case storage.SSES3Type:
	case "":
		return storage.S3SSEConfig{}, nil

	default:
		return storage.S3SSEConfig{}, kverrors.New("unsupported secret field value (Supported: SSE-KMS, SSE-S3)", "field", "sse_type", "value", sseType)
	}

	return storage.S3SSEConfig{
		Type:                 sseType,
		KMSKeyID:             string(kmsKeyId),
		KMSEncryptionContext: string(kmsEncryptionCtx),
	}, nil
}

func extractSwiftConfigSecret(s *corev1.Secret) (*storage.SwiftStorageConfig, error) {
	// Extract and validate mandatory fields
	url := s.Data[storage.KeyOSSwiftAuthURL]
	if len(url) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftAuthURL)
	}
	username := s.Data[storage.KeyOSSwiftUsername]
	if len(username) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftUsername)
	}
	userDomainName := s.Data[storage.KeyOSSwiftUserDomainName]
	if len(userDomainName) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftUserDomainName)
	}
	userDomainID := s.Data[storage.KeyOSSwiftUserDomainID]
	if len(userDomainID) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftUserDomainID)
	}
	userID := s.Data[storage.KeyOSSwiftUserID]
	if len(userID) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftUserID)
	}
	password := s.Data[storage.KeyOSSwiftPassword]
	if len(password) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftPassword)
	}
	domainID := s.Data[storage.KeyOSSwiftDomainID]
	if len(domainID) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftDomainID)
	}
	domainName := s.Data[storage.KeyOSSwiftDomainName]
	if len(domainName) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftDomainName)
	}
	containerName := s.Data[storage.KeyOSSwiftContainerName]
	if len(containerName) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyOSSwiftContainerName)
	}

	// Extract and validate optional fields
	projectID := s.Data[storage.KeyOSSwiftProjectID]
	projectName := s.Data[storage.KeyOSSwiftProjectName]
	projectDomainID := s.Data[storage.KeyOSSwiftProjectDomainId]
	projectDomainName := s.Data[storage.KeyOSSwiftProjectDomainName]
	region := s.Data[storage.KeyOSSwiftRegion]

	return &storage.SwiftStorageConfig{
		AuthURL:           string(url),
		UserDomainName:    string(userDomainName),
		UserDomainID:      string(userDomainID),
		UserID:            string(userID),
		DomainID:          string(domainID),
		DomainName:        string(domainName),
		ProjectID:         string(projectID),
		ProjectName:       string(projectName),
		ProjectDomainID:   string(projectDomainID),
		ProjectDomainName: string(projectDomainName),
		Region:            string(region),
		Container:         string(containerName),
	}, nil
}

func extractAlibabaCloudConfigSecret(s *corev1.Secret) (*storage.AlibabaCloudStorageConfig, error) {
	// Extract and validate mandatory fields
	endpoint := s.Data[storage.KeyAlibabaCloudEndpoint]
	if len(endpoint) == 0 {
		return nil, kverrors.New("missing secret field", "field", "endpoint")
	}
	bucket := s.Data[storage.KeyAlibabaCloudBucket]
	if len(bucket) == 0 {
		return nil, kverrors.New("missing secret field", "field", "bucket")
	}
	// TODO buckets are comma-separated list
	id := s.Data[storage.KeyAlibabaCloudAccessKeyID]
	if len(id) == 0 {
		return nil, kverrors.New("missing secret field", "field", "access_key_id")
	}
	secret := s.Data[storage.KeyAlibabaCloudSecretAccessKey]
	if len(secret) == 0 {
		return nil, kverrors.New("missing secret field", "field", "secret_access_key")
	}

	return &storage.AlibabaCloudStorageConfig{
		Endpoint: string(endpoint),
		Bucket:   string(bucket),
	}, nil
}
