package storage

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/storage"

	corev1 "k8s.io/api/core/v1"
)

// ExtractSecret reads a k8s secret into a manifest object storage struct if valid.
func ExtractSecret(s *corev1.Secret, secretType lokiv1beta1.ObjectStorageSecretType) (*storage.Options, error) {
	var err error
	storageOpts := storage.Options{
		SecretName:  s.Name,
		SharedStore: secretType,
	}

	switch secretType {
	case lokiv1beta1.ObjectStorageSecretAzure:
		storageOpts.Azure, err = extractAzureConfigSecret(s)
	case lokiv1beta1.ObjectStorageSecretGCS:
		storageOpts.GCS, err = extractGCSConfigSecret(s)
	case lokiv1beta1.ObjectStorageSecretS3:
		storageOpts.S3, err = extractS3ConfigSecret(s)
	case lokiv1beta1.ObjectStorageSecretSwift:
		storageOpts.Swift, err = extractSwiftConfigSecret(s)
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
	env := s.Data["environment"]
	if env == nil {
		return nil, kverrors.New("missing secret field", "field", "environment")
	}
	container := s.Data["container"]
	if container == nil {
		return nil, kverrors.New("missing secret field", "field", "container")
	}
	name := s.Data["account_name"]
	if name == nil {
		return nil, kverrors.New("missing secret field", "field", "account_name")
	}
	key := s.Data["account_key"]
	if key == nil {
		return nil, kverrors.New("missing secret field", "field", "account_key")
	}

	return &storage.AzureStorageConfig{
		Env:         string(env),
		Container:   string(container),
		AccountName: string(name),
		AccountKey:  string(key),
	}, nil
}

func extractGCSConfigSecret(s *corev1.Secret) (*storage.GCSStorageConfig, error) {
	// Extract and validate mandatory fields
	bucket := s.Data["bucketname"]
	if bucket == nil {
		return nil, kverrors.New("missing secret field", "field", "bucketname")
	}

	// Check if google authentication credentials is provided
	keyJSON := s.Data["key.json"]
	if keyJSON == nil {
		return nil, kverrors.New("missing google authentication credentials", "field", "key.json")
	}

	return &storage.GCSStorageConfig{
		Bucket: string(bucket),
	}, nil
}

func extractS3ConfigSecret(s *corev1.Secret) (*storage.S3StorageConfig, error) {
	// Extract and validate mandatory fields
	endpoint := s.Data["endpoint"]
	if endpoint == nil {
		return nil, kverrors.New("missing secret field", "field", "endpoint")
	}
	buckets := s.Data["bucketnames"]
	if buckets == nil {
		return nil, kverrors.New("missing secret field", "field", "bucketnames")
	}
	// TODO buckets are comma-separated list
	id := s.Data["access_key_id"]
	if id == nil {
		return nil, kverrors.New("missing secret field", "field", "access_key_id")
	}
	secret := s.Data["access_key_secret"]
	if secret == nil {
		return nil, kverrors.New("missing secret field", "field", "access_key_secret")
	}

	// Extract and validate optional fields
	region := s.Data["region"]

	return &storage.S3StorageConfig{
		Endpoint:        string(endpoint),
		Buckets:         string(buckets),
		AccessKeyID:     string(id),
		AccessKeySecret: string(secret),
		Region:          string(region),
	}, nil
}

func extractSwiftConfigSecret(s *corev1.Secret) (*storage.SwiftStorageConfig, error) {
	// Extract and validate mandatory fields
	url := s.Data["auth_url"]
	if url == nil {
		return nil, kverrors.New("missing secret field", "field", "auth_url")
	}
	username := s.Data["username"]
	if username == nil {
		return nil, kverrors.New("missing secret field", "field", "username")
	}
	userDomainName := s.Data["user_domain_name"]
	if userDomainName == nil {
		return nil, kverrors.New("missing secret field", "field", "user_domain_name")
	}
	userDomainID := s.Data["user_domain_id"]
	if userDomainID == nil {
		return nil, kverrors.New("missing secret field", "field", "user_domain_id")
	}
	userID := s.Data["user_id"]
	if userID == nil {
		return nil, kverrors.New("missing secret field", "field", "user_id")
	}
	password := s.Data["password"]
	if password == nil {
		return nil, kverrors.New("missing secret field", "field", "password")
	}
	domainID := s.Data["domain_id"]
	if domainID == nil {
		return nil, kverrors.New("missing secret field", "field", "domain_id")
	}
	domainName := s.Data["domain_name"]
	if domainName == nil {
		return nil, kverrors.New("missing secret field", "field", "domain_name")
	}
	containerName := s.Data["container_name"]
	if containerName == nil {
		return nil, kverrors.New("missing secret field", "field", "container_name")
	}

	// Extract and validate optional fields
	projectID := s.Data["project_id"]
	projectName := s.Data["project_name"]
	projectDomainID := s.Data["project_domain_id"]
	projectDomainName := s.Data["project_domain_name"]
	region := s.Data["region"]

	return &storage.SwiftStorageConfig{
		AuthURL:           string(url),
		Username:          string(username),
		UserDomainName:    string(userDomainName),
		UserDomainID:      string(userDomainID),
		UserID:            string(userID),
		Password:          string(password),
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
