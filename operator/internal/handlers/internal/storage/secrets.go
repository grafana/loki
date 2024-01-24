package storage

import (
	"context"
	"crypto/sha1"
	"fmt"
	"sort"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/grafana/loki/operator/internal/status"
)

var hashSeparator = []byte(",")

func getSecrets(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack, fg configv1.FeatureGates) (*corev1.Secret, *corev1.Secret, error) {
	var (
		storageSecret     corev1.Secret
		managedAuthSecret corev1.Secret
	)

	key := client.ObjectKey{Name: stack.Spec.Storage.Secret.Name, Namespace: stack.Namespace}
	if err := k.Get(ctx, key, &storageSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, &status.DegradedError{
				Message: "Missing object storage secret",
				Reason:  lokiv1.ReasonMissingObjectStorageSecret,
				Requeue: false,
			}
		}
		return nil, nil, kverrors.Wrap(err, "failed to lookup lokistack storage secret", "name", key)
	}

	if fg.OpenShift.ManagedAuthEnv {
		secretName, ok := stack.Annotations[storage.AnnotationCredentialsRequestsSecretRef]
		if !ok {
			return nil, nil, &status.DegradedError{
				Message: "Missing OpenShift cloud credentials request",
				Reason:  lokiv1.ReasonMissingCredentialsRequest,
				Requeue: true,
			}
		}

		managedAuthCredsKey := client.ObjectKey{Name: secretName, Namespace: stack.Namespace}
		if err := k.Get(ctx, managedAuthCredsKey, &managedAuthSecret); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, &status.DegradedError{
					Message: "Missing OpenShift cloud credentials secret",
					Reason:  lokiv1.ReasonMissingManagedAuthSecret,
					Requeue: true,
				}
			}
			return nil, nil, kverrors.Wrap(err, "failed to lookup OpenShift CCO managed authentication credentials secret", "name", stack)
		}

		return &storageSecret, &managedAuthSecret, nil
	}

	return &storageSecret, nil, nil
}

// extractSecrets reads the k8s obj storage secret into a manifest object storage struct if valid.
// The managed auth is also read into the manifest object under the right circumstances.
func extractSecrets(secretType lokiv1.ObjectStorageSecretType, objStore, managedAuth *corev1.Secret, fg configv1.FeatureGates) (storage.Options, error) {
	hash, err := hashSecretData(objStore)
	if err != nil {
		return storage.Options{}, kverrors.Wrap(err, "error calculating hash for secret", "type", secretType)
	}

	storageOpts := storage.Options{
		SecretName:  objStore.Name,
		SecretSHA1:  hash,
		SharedStore: secretType,
	}

	if fg.OpenShift.ManagedAuthEnabled() {
		var managedAuthHash string
		managedAuthHash, err = hashSecretData(managedAuth)
		if err != nil {
			return storage.Options{}, kverrors.Wrap(err, "error calculating hash for secret", "type", client.ObjectKeyFromObject(managedAuth))
		}

		storageOpts.OpenShift = storage.OpenShiftOptions{
			CloudCredentials: storage.CloudCredentials{
				SecretName: managedAuth.Name,
				SHA1:       managedAuthHash,
			},
		}
	}

	switch secretType {
	case lokiv1.ObjectStorageSecretAzure:
		storageOpts.Azure, err = extractAzureConfigSecret(objStore)
	case lokiv1.ObjectStorageSecretGCS:
		storageOpts.GCS, err = extractGCSConfigSecret(objStore)
	case lokiv1.ObjectStorageSecretS3:
		storageOpts.S3, err = extractS3ConfigSecret(objStore, fg)
	case lokiv1.ObjectStorageSecretSwift:
		storageOpts.Swift, err = extractSwiftConfigSecret(objStore)
	case lokiv1.ObjectStorageSecretAlibabaCloud:
		storageOpts.AlibabaCloud, err = extractAlibabaCloudConfigSecret(objStore)
	default:
		return storage.Options{}, kverrors.New("unknown secret type", "type", secretType)
	}

	if err != nil {
		return storage.Options{}, err
	}
	return storageOpts, nil
}

func hashSecretData(s *corev1.Secret) (string, error) {
	keys := make([]string, 0, len(s.Data))
	for k := range s.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	h := sha1.New()
	for _, k := range keys {
		if _, err := h.Write([]byte(k)); err != nil {
			return "", err
		}

		if _, err := h.Write(hashSeparator); err != nil {
			return "", err
		}

		if _, err := h.Write(s.Data[k]); err != nil {
			return "", err
		}

		if _, err := h.Write(hashSeparator); err != nil {
			return "", err
		}
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
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

func extractS3ConfigSecret(s *corev1.Secret, fg configv1.FeatureGates) (*storage.S3StorageConfig, error) {
	// Extract and validate mandatory fields
	buckets := s.Data[storage.KeyAWSBucketNames]
	if len(buckets) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAWSBucketNames)
	}

	var (
		// Fields related with static authentication
		endpoint = s.Data[storage.KeyAWSEndpoint]
		id       = s.Data[storage.KeyAWSAccessKeyID]
		secret   = s.Data[storage.KeyAWSAccessKeySecret]
		// Fields related with STS authentication
		roleArn  = s.Data[storage.KeyAWSRoleArn]
		audience = s.Data[storage.KeyAWSAudience]
		// Optional fields
		region = s.Data[storage.KeyAWSRegion]
	)

	sseCfg, err := extractS3SSEConfig(s.Data)
	if err != nil {
		return nil, err
	}

	cfg := &storage.S3StorageConfig{
		Buckets: string(buckets),
		Region:  string(region),
		SSE:     sseCfg,
	}

	var (
		isManagedAuthEnv = len(roleArn) != 0
		isStaticAuthEnv  = !isManagedAuthEnv
	)

	switch {
	case fg.OpenShift.ManagedAuthEnabled():
		cfg.STS = true
		cfg.Audience = storage.AWSOpenShiftAudience
		// Do not allow users overriding the role arn provided on Loki Operator installation
		if len(roleArn) != 0 {
			return nil, kverrors.New("extra secret field set", "field", storage.KeyAWSRoleArn)
		}
		if len(audience) != 0 {
			return nil, kverrors.New("extra secret field set", "field", storage.KeyAWSAudience)
		}
		// In the STS case region is not an optional field
		if len(region) == 0 {
			return nil, kverrors.New("missing secret field", "field", storage.KeyAWSRegion)
		}

		return cfg, nil
	case isStaticAuthEnv:
		cfg.Endpoint = string(endpoint)

		if len(endpoint) == 0 {
			return nil, kverrors.New("missing secret field", "field", storage.KeyAWSEndpoint)
		}
		if len(id) == 0 {
			return nil, kverrors.New("missing secret field", "field", storage.KeyAWSAccessKeyID)
		}
		if len(secret) == 0 {
			return nil, kverrors.New("missing secret field", "field", storage.KeyAWSAccessKeySecret)
		}

		return cfg, nil
	case isManagedAuthEnv: // Extract STS from user provided values
		cfg.STS = true
		cfg.Audience = string(audience)

		// In the STS case region is not an optional field
		if len(region) == 0 {
			return nil, kverrors.New("missing secret field", "field", storage.KeyAWSRegion)
		}
		return cfg, nil
	default:
		return nil, kverrors.New("missing secret fields for static or sts authentication")
	}
}

func extractS3SSEConfig(d map[string][]byte) (storage.S3SSEConfig, error) {
	var (
		sseType                    storage.S3SSEType
		kmsKeyId, kmsEncryptionCtx string
	)

	switch sseType = storage.S3SSEType(d[storage.KeyAWSSSEType]); sseType {
	case storage.SSEKMSType:
		kmsEncryptionCtx = string(d[storage.KeyAWSSseKmsEncryptionContext])
		kmsKeyId = string(d[storage.KeyAWSSseKmsKeyID])
		if kmsKeyId == "" {
			return storage.S3SSEConfig{}, kverrors.New("missing secret field", "field", storage.KeyAWSSseKmsKeyID)
		}

	case storage.SSES3Type:
	case "":
		return storage.S3SSEConfig{}, nil

	default:
		return storage.S3SSEConfig{}, kverrors.New("unsupported secret field value (Supported: SSE-KMS, SSE-S3)", "field", storage.KeyAWSSSEType, "value", sseType)
	}

	return storage.S3SSEConfig{
		Type:                 sseType,
		KMSKeyID:             string(kmsKeyId),
		KMSEncryptionContext: string(kmsEncryptionCtx),
	}, nil
}

func extractSwiftConfigSecret(s *corev1.Secret) (*storage.SwiftStorageConfig, error) {
	// Extract and validate mandatory fields
	url := s.Data[storage.KeySwiftAuthURL]
	if len(url) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftAuthURL)
	}
	username := s.Data[storage.KeySwiftUsername]
	if len(username) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftUsername)
	}
	userDomainName := s.Data[storage.KeySwiftUserDomainName]
	if len(userDomainName) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftUserDomainName)
	}
	userDomainID := s.Data[storage.KeySwiftUserDomainID]
	if len(userDomainID) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftUserDomainID)
	}
	userID := s.Data[storage.KeySwiftUserID]
	if len(userID) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftUserID)
	}
	password := s.Data[storage.KeySwiftPassword]
	if len(password) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftPassword)
	}
	domainID := s.Data[storage.KeySwiftDomainID]
	if len(domainID) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftDomainID)
	}
	domainName := s.Data[storage.KeySwiftDomainName]
	if len(domainName) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftDomainName)
	}
	containerName := s.Data[storage.KeySwiftContainerName]
	if len(containerName) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeySwiftContainerName)
	}

	// Extract and validate optional fields
	projectID := s.Data[storage.KeySwiftProjectID]
	projectName := s.Data[storage.KeySwiftProjectName]
	projectDomainID := s.Data[storage.KeySwiftProjectDomainId]
	projectDomainName := s.Data[storage.KeySwiftProjectDomainName]
	region := s.Data[storage.KeySwiftRegion]

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
		return nil, kverrors.New("missing secret field", "field", storage.KeyAlibabaCloudEndpoint)
	}
	bucket := s.Data[storage.KeyAlibabaCloudBucket]
	if len(bucket) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAlibabaCloudBucket)
	}
	id := s.Data[storage.KeyAlibabaCloudAccessKeyID]
	if len(id) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAlibabaCloudAccessKeyID)
	}
	secret := s.Data[storage.KeyAlibabaCloudSecretAccessKey]
	if len(secret) == 0 {
		return nil, kverrors.New("missing secret field", "field", storage.KeyAlibabaCloudSecretAccessKey)
	}

	return &storage.AlibabaCloudStorageConfig{
		Endpoint: string(endpoint),
		Bucket:   string(bucket),
	}, nil
}
