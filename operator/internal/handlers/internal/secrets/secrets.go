package secrets

import (
	"github.com/ViaQ/logerr/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests"

	corev1 "k8s.io/api/core/v1"
)

// Extract reads a k8s secret into a manifest object storage struct if valid.
func Extract(s *corev1.Secret, t lokiv1beta1.ObjectStorageSecretType) (*manifests.ObjectStorage, error) {
	var err error
	storage := manifests.ObjectStorage{}

	switch t {
	case lokiv1beta1.ObjectStorageSecretAzure:
		storage.Azure, err = extractAzureConfigSecret(s)
	case lokiv1beta1.ObjectStorageSecretGCS:
		storage.GCS, err = extractGCSConfigSecret(s)
	case lokiv1beta1.ObjectStorageSecretS3:
		storage.S3, err = extractS3ConfigSecret(s)
	case lokiv1beta1.ObjectStorageSecretSwift:
		storage.Swift, err = extractSwiftConfigSecret(s)
	default:
		return nil, kverrors.New("unknown secret type", "type", t)
	}

	if err != nil {
		return nil, err
	}
	return &storage, nil
}

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

func extractAzureConfigSecret(s *corev1.Secret) (*manifests.AzureConfig, error) {
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

	return &manifests.AzureConfig{
		Env:         string(env),
		Container:   string(container),
		AccountName: string(name),
		AccountKey:  string(key),
	}, nil
}

func extractGCSConfigSecret(s *corev1.Secret) (*manifests.GCSConfig, error) {
	// Extract and validate mandatory fields
	bucket, ok := s.Data["bucketname"]
	if !ok {
		return nil, kverrors.New("missing secret field", "field", "bucketname")
	}

	return &manifests.GCSConfig{
		Bucket: string(bucket),
	}, nil
}

func extractS3ConfigSecret(s *corev1.Secret) (*manifests.S3Config, error) {
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
	region, ok := s.Data["region"]
	if !ok {
		region = []byte("")
	}

	return &manifests.S3Config{
		Endpoint:        string(endpoint),
		Buckets:         string(buckets),
		AccessKeyID:     string(id),
		AccessKeySecret: string(secret),
		Region:          string(region),
	}, nil
}

func extractSwiftConfigSecret(s *corev1.Secret) (*manifests.SwiftConfig, error) {
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
	projectID, ok := s.Data["project_id"]
	if !ok {
		projectID = []byte("")
	}
	projectName, ok := s.Data["project_name"]
	if !ok {
		projectName = []byte("")
	}
	projectDomainID, ok := s.Data["project_domain_id"]
	if !ok {
		projectDomainID = []byte("")
	}
	projectDomainName, ok := s.Data["project_domain_name"]
	if !ok {
		projectDomainName = []byte("")
	}
	region, ok := s.Data["region"]
	if !ok {
		region = []byte("")
	}

	return &manifests.SwiftConfig{
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
