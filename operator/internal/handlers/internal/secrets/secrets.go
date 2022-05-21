package secrets

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/manifests/storage"

	corev1 "k8s.io/api/core/v1"
)

// ExtractGatewaySecret reads a k8s secret into a manifest tenant secret struct if valid.
func ExtractGatewaySecret(s *corev1.Secret, tenantName string) (*manifests.TenantSecrets, error) {
	// Extract and validate mandatory fields
	clientID, ok := s.Data["clientID"]
	if !ok {
		return nil, kverrors.New("missing clientID field", "field", "clientID")
	}
	clientSecret, ok := s.Data["clientSecret"]
	if !ok {
		return nil, kverrors.New("missing clientSecret field", "field", "clientSecret")
	}
	issuerCAPath, ok := s.Data["issuerCAPath"]
	if !ok {
		return nil, kverrors.New("missing issuerCAPath field", "field", "issuerCAPath")
	}

	return &manifests.TenantSecrets{
		TenantName:   tenantName,
		ClientID:     string(clientID),
		ClientSecret: string(clientSecret),
		IssuerCAPath: string(issuerCAPath),
	}, nil
}

// ExtractStorageSecret reads a k8s secret into a manifest object storage struct if valid.
func ExtractStorageSecret(s *corev1.Secret, secretType lokiv1beta1.ObjectStorageSecretType) (*storage.Options, error) {
	var err error
	storageOpts := storage.Options{SharedStore: secretType}

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
	env, ok := s.Data["environment"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "environment")
	}
	container, ok := s.Data["container"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "container")
	}
	name, ok := s.Data["account_name"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "account_name")
	}
	key, ok := s.Data["account_key"]
	if !ok {
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
	bucket, ok := s.Data["bucketname"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "bucketname")
	}

	// Check if google authentication credentials is provided
	_, ok = s.Data["key.json"]
	if !ok {
		return nil, kverrors.New("missing google authentication credentials", "field", "key.json")
	}

	return &storage.GCSStorageConfig{
		Bucket: string(bucket),
	}, nil
}

func extractS3ConfigSecret(s *corev1.Secret) (*storage.S3StorageConfig, error) {
	// Extract and validate mandatory fields
	endpoint, ok := s.Data["endpoint"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "endpoint")
	}
	buckets, ok := s.Data["bucketnames"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "bucketnames")
	}
	// TODO buckets are comma-separated list
	id, ok := s.Data["access_key_id"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "access_key_id")
	}
	secret, ok := s.Data["access_key_secret"]
	if !ok {
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
	url, ok := s.Data["auth_url"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "auth_url")
	}
	username, ok := s.Data["username"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "username")
	}
	userDomainName, ok := s.Data["user_domain_name"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "user_domain_name")
	}
	userDomainID, ok := s.Data["user_domain_id"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "user_domain_id")
	}
	userID, ok := s.Data["user_id"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "user_id")
	}
	password, ok := s.Data["password"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "password")
	}
	domainID, ok := s.Data["domain_id"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "domain_id")
	}
	domainName, ok := s.Data["domain_name"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "domain_name")
	}
	containerName, ok := s.Data["container_name"]
	if !ok {
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
