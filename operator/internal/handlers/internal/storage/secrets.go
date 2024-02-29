package storage

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/grafana/loki/operator/internal/status"
)

var (
	hashSeparator = []byte(",")

	errSecretUnknownType     = errors.New("unknown secret type")
	errSecretMissingField    = errors.New("missing secret field")
	errSecretFieldNotAllowed = errors.New("secret field not allowed")
	errSecretUnknownSSEType  = errors.New("unsupported SSE type (supported: SSE-KMS, SSE-S3)")
	errSecretHashError       = errors.New("error calculating hash for secret")

	errS3NoAuth = errors.New("missing secret fields for static or sts authentication")

	errAzureNoCredentials             = errors.New("azure storage secret does contain neither account_key or client_id")
	errAzureMixedCredentials          = errors.New("azure storage secret can not contain both account_key and client_id")
	errAzureManagedIdentityNoOverride = errors.New("when in managed mode, storage secret can not contain credentials")
	errAzureInvalidEnvironment        = errors.New("azure environment invalid (valid values: AzureGlobal, AzureChinaCloud, AzureGermanCloud, AzureUSGovernment)")
	errAzureInvalidAccountKey         = errors.New("azure account key is not valid base64")

	errGCPParseCredentialsFile      = errors.New("gcp storage secret cannot be parsed from JSON content")
	errGCPWrongCredentialSourceFile = errors.New("credential source in secret needs to point to token file")

	azureValidEnvironments = map[string]bool{
		"AzureGlobal":       true,
		"AzureChinaCloud":   true,
		"AzureGermanCloud":  true,
		"AzureUSGovernment": true,
	}
)

const gcpAccountTypeExternal = "external_account"

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
		return nil, nil, fmt.Errorf("failed to lookup lokistack storage secret: %w", err)
	}

	if fg.OpenShift.ManagedAuthEnv {
		secretName := storage.ManagedCredentialsSecretName(stack.Name)
		managedAuthCredsKey := client.ObjectKey{Name: secretName, Namespace: stack.Namespace}
		if err := k.Get(ctx, managedAuthCredsKey, &managedAuthSecret); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, &status.DegradedError{
					Message: "Missing OpenShift cloud credentials secret",
					Reason:  lokiv1.ReasonMissingManagedAuthSecret,
					Requeue: true,
				}
			}
			return nil, nil, fmt.Errorf("failed to lookup OpenShift CCO managed authentication credentials secret: %w", err)
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
		return storage.Options{}, errSecretHashError
	}

	storageOpts := storage.Options{
		SecretName:  objStore.Name,
		SecretSHA1:  hash,
		SharedStore: secretType,
	}

	if fg.OpenShift.ManagedAuthEnv {
		var managedAuthHash string
		managedAuthHash, err = hashSecretData(managedAuth)
		if err != nil {
			return storage.Options{}, errSecretHashError
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
		storageOpts.Azure, err = extractAzureConfigSecret(objStore, fg)
	case lokiv1.ObjectStorageSecretGCS:
		storageOpts.GCS, err = extractGCSConfigSecret(objStore)
	case lokiv1.ObjectStorageSecretS3:
		storageOpts.S3, err = extractS3ConfigSecret(objStore, fg)
	case lokiv1.ObjectStorageSecretSwift:
		storageOpts.Swift, err = extractSwiftConfigSecret(objStore)
	case lokiv1.ObjectStorageSecretAlibabaCloud:
		storageOpts.AlibabaCloud, err = extractAlibabaCloudConfigSecret(objStore)
	default:
		return storage.Options{}, fmt.Errorf("%w: %s", errSecretUnknownType, secretType)
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

func extractAzureConfigSecret(s *corev1.Secret, fg configv1.FeatureGates) (*storage.AzureStorageConfig, error) {
	// Extract and validate mandatory fields
	env := string(s.Data[storage.KeyAzureEnvironmentName])
	if env == "" {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureEnvironmentName)
	}

	if !azureValidEnvironments[env] {
		return nil, fmt.Errorf("%w: %s", errAzureInvalidEnvironment, env)
	}

	accountName := s.Data[storage.KeyAzureStorageAccountName]
	if len(accountName) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureStorageAccountName)
	}

	container := s.Data[storage.KeyAzureStorageContainerName]
	if len(container) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureStorageContainerName)
	}

	workloadIdentity, err := validateAzureCredentials(s, fg)
	if err != nil {
		return nil, err
	}

	// Extract and validate optional fields
	endpointSuffix := s.Data[storage.KeyAzureStorageEndpointSuffix]
	audience := s.Data[storage.KeyAzureAudience]

	if !workloadIdentity && len(audience) > 0 {
		return nil, fmt.Errorf("%w: %s", errSecretFieldNotAllowed, storage.KeyAzureAudience)
	}

	return &storage.AzureStorageConfig{
		Env:              env,
		Container:        string(container),
		EndpointSuffix:   string(endpointSuffix),
		Audience:         string(audience),
		WorkloadIdentity: workloadIdentity,
	}, nil
}

func validateAzureCredentials(s *corev1.Secret, fg configv1.FeatureGates) (workloadIdentity bool, err error) {
	accountKey := s.Data[storage.KeyAzureStorageAccountKey]
	clientID := s.Data[storage.KeyAzureStorageClientID]
	tenantID := s.Data[storage.KeyAzureStorageTenantID]
	subscriptionID := s.Data[storage.KeyAzureStorageSubscriptionID]

	if fg.OpenShift.ManagedAuthEnv {
		if len(accountKey) > 0 || len(clientID) > 0 || len(tenantID) > 0 || len(subscriptionID) > 0 {
			return false, errAzureManagedIdentityNoOverride
		}

		return true, nil
	}

	if len(accountKey) == 0 && len(clientID) == 0 {
		return false, errAzureNoCredentials
	}

	if len(accountKey) > 0 && len(clientID) > 0 {
		return false, errAzureMixedCredentials
	}

	if len(accountKey) > 0 {
		if err := validateBase64(accountKey); err != nil {
			return false, errAzureInvalidAccountKey
		}

		// have both account_name and account_key -> no workload identity federation
		return false, nil
	}

	// assume workload-identity from here on
	if len(tenantID) == 0 {
		return false, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureStorageTenantID)
	}

	if len(subscriptionID) == 0 {
		return false, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureStorageSubscriptionID)
	}

	return true, nil
}

func validateBase64(data []byte) error {
	buf := bytes.NewBuffer(data)
	reader := base64.NewDecoder(base64.StdEncoding, buf)
	_, err := io.ReadAll(reader)
	return err
}

func extractGCSConfigSecret(s *corev1.Secret) (*storage.GCSStorageConfig, error) {
	// Extract and validate mandatory fields
	bucket := s.Data[storage.KeyGCPStorageBucketName]
	if len(bucket) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyGCPStorageBucketName)
	}

	// Check if google authentication credentials is provided
	keyJSON := s.Data[storage.KeyGCPServiceAccountKeyFilename]
	if len(keyJSON) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyGCPServiceAccountKeyFilename)
	}

	credentialsFile := struct {
		CredentialsType   string `json:"type"`
		CredentialsSource struct {
			File string `json:"file"`
		} `json:"credential_source"`
	}{}

	err := json.Unmarshal(keyJSON, &credentialsFile)
	if err != nil {
		return nil, errGCPParseCredentialsFile
	}

	var (
		audience           = s.Data[storage.KeyGCPWorkloadIdentityProviderAudience]
		isWorkloadIdentity = credentialsFile.CredentialsType == gcpAccountTypeExternal
	)
	if isWorkloadIdentity {
		if len(audience) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyGCPWorkloadIdentityProviderAudience)
		}

		if credentialsFile.CredentialsSource.File != storage.ServiceAccountTokenFilePath {
			return nil, fmt.Errorf("%w: %s", errGCPWrongCredentialSourceFile, storage.ServiceAccountTokenFilePath)
		}
	}

	return &storage.GCSStorageConfig{
		Bucket:           string(bucket),
		WorkloadIdentity: isWorkloadIdentity,
		Audience:         string(audience),
	}, nil
}

func extractS3ConfigSecret(s *corev1.Secret, fg configv1.FeatureGates) (*storage.S3StorageConfig, error) {
	// Extract and validate mandatory fields
	buckets := s.Data[storage.KeyAWSBucketNames]
	if len(buckets) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSBucketNames)
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
	case fg.OpenShift.ManagedAuthEnv:
		cfg.STS = true
		cfg.Audience = string(audience)
		// Do not allow users overriding the role arn provided on Loki Operator installation
		if len(roleArn) != 0 {
			return nil, fmt.Errorf("%w: %s", errSecretFieldNotAllowed, storage.KeyAWSRoleArn)
		}
		// In the STS case region is not an optional field
		if len(region) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSRegion)
		}

		return cfg, nil
	case isStaticAuthEnv:
		cfg.Endpoint = string(endpoint)

		if len(endpoint) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSEndpoint)
		}
		if len(id) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSAccessKeyID)
		}
		if len(secret) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSAccessKeySecret)
		}

		return cfg, nil
	case isManagedAuthEnv: // Extract STS from user provided values
		cfg.STS = true
		cfg.Audience = string(audience)

		// In the STS case region is not an optional field
		if len(region) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSRegion)
		}
		return cfg, nil
	default:
		return nil, errS3NoAuth
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
			return storage.S3SSEConfig{}, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSSseKmsKeyID)
		}

	case storage.SSES3Type:
	case "":
		return storage.S3SSEConfig{}, nil

	default:
		return storage.S3SSEConfig{}, fmt.Errorf("%w: %s", errSecretUnknownSSEType, sseType)
	}

	return storage.S3SSEConfig{
		Type:                 sseType,
		KMSKeyID:             kmsKeyId,
		KMSEncryptionContext: kmsEncryptionCtx,
	}, nil
}

func extractSwiftConfigSecret(s *corev1.Secret) (*storage.SwiftStorageConfig, error) {
	// Extract and validate mandatory fields
	url := s.Data[storage.KeySwiftAuthURL]
	if len(url) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftAuthURL)
	}
	username := s.Data[storage.KeySwiftUsername]
	if len(username) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftUsername)
	}
	userDomainName := s.Data[storage.KeySwiftUserDomainName]
	if len(userDomainName) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftUserDomainName)
	}
	userDomainID := s.Data[storage.KeySwiftUserDomainID]
	if len(userDomainID) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftUserDomainID)
	}
	userID := s.Data[storage.KeySwiftUserID]
	if len(userID) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftUserID)
	}
	password := s.Data[storage.KeySwiftPassword]
	if len(password) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftPassword)
	}
	domainID := s.Data[storage.KeySwiftDomainID]
	if len(domainID) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftDomainID)
	}
	domainName := s.Data[storage.KeySwiftDomainName]
	if len(domainName) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftDomainName)
	}
	containerName := s.Data[storage.KeySwiftContainerName]
	if len(containerName) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeySwiftContainerName)
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
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAlibabaCloudEndpoint)
	}
	bucket := s.Data[storage.KeyAlibabaCloudBucket]
	if len(bucket) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAlibabaCloudBucket)
	}
	id := s.Data[storage.KeyAlibabaCloudAccessKeyID]
	if len(id) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAlibabaCloudAccessKeyID)
	}
	secret := s.Data[storage.KeyAlibabaCloudSecretAccessKey]
	if len(secret) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAlibabaCloudSecretAccessKey)
	}

	return &storage.AlibabaCloudStorageConfig{
		Endpoint: string(endpoint),
		Bucket:   string(bucket),
	}, nil
}
