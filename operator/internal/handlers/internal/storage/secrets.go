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
	"net/url"
	"sort"
	"strings"

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

	errSecretUnknownCredentialMode     = errors.New("unknown credential mode")
	errSecretUnsupportedCredentialMode = errors.New("combination of storage type and credential mode not supported")

	errAzureManagedIdentityNoOverride = errors.New("when in managed mode, storage secret can not contain credentials")
	errAzureInvalidEnvironment        = errors.New("azure environment invalid (valid values: AzureGlobal, AzureChinaCloud, AzureGermanCloud, AzureUSGovernment)")
	errAzureInvalidAccountKey         = errors.New("azure account key is not valid base64")

	errS3EndpointUnparseable       = errors.New("can not parse S3 endpoint as URL")
	errS3EndpointNoURL             = errors.New("endpoint for S3 must be an HTTP or HTTPS URL")
	errS3EndpointUnsupportedScheme = errors.New("scheme of S3 endpoint URL is unsupported")
	errS3EndpointAWSInvalid        = errors.New("endpoint for AWS S3 must include correct region")

	errGCPParseCredentialsFile      = errors.New("gcp storage secret cannot be parsed from JSON content")
	errGCPWrongCredentialSourceFile = errors.New("credential source in secret needs to point to token file")

	azureValidEnvironments = map[string]bool{
		"AzureGlobal":       true,
		"AzureChinaCloud":   true,
		"AzureGermanCloud":  true,
		"AzureUSGovernment": true,
	}
)

const (
	awsEndpointSuffix      = ".amazonaws.com"
	gcpAccountTypeExternal = "external_account"
)

func getSecrets(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack, fg configv1.FeatureGates) (*corev1.Secret, *corev1.Secret, error) {
	var (
		storageSecret      corev1.Secret
		tokenCCOAuthSecret corev1.Secret
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

	if fg.OpenShift.TokenCCOAuthEnv {
		secretName := storage.ManagedCredentialsSecretName(stack.Name)
		tokenCCOAuthCredsKey := client.ObjectKey{Name: secretName, Namespace: stack.Namespace}
		if err := k.Get(ctx, tokenCCOAuthCredsKey, &tokenCCOAuthSecret); err != nil {
			if apierrors.IsNotFound(err) {
				// We don't know if this is an error yet, need to wait for evaluation of CredentialMode
				// For now we go with empty "managed secret", the eventual DegradedError will be returned later.
				return &storageSecret, nil, nil
			}
			return nil, nil, fmt.Errorf("failed to lookup OpenShift CCO token authentication credentials secret: %w", err)
		}

		return &storageSecret, &tokenCCOAuthSecret, nil
	}

	return &storageSecret, nil, nil
}

// extractSecrets reads the k8s obj storage secret into a manifest object storage struct if valid.
// The token cco auth is also read into the manifest object under the right circumstances.
func extractSecrets(secretSpec lokiv1.ObjectStorageSecretSpec, objStore, tokenCCOAuth *corev1.Secret, fg configv1.FeatureGates) (storage.Options, error) {
	hash, err := hashSecretData(objStore)
	if err != nil {
		return storage.Options{}, errSecretHashError
	}

	openShiftOpts := storage.OpenShiftOptions{
		Enabled: fg.OpenShift.Enabled,
	}
	if tokenCCOAuth != nil {
		var tokenCCOAuthHash string
		tokenCCOAuthHash, err = hashSecretData(tokenCCOAuth)
		if err != nil {
			return storage.Options{}, errSecretHashError
		}

		openShiftOpts.CloudCredentials = storage.CloudCredentials{
			SecretName: tokenCCOAuth.Name,
			SHA1:       tokenCCOAuthHash,
		}
	}

	storageOpts := storage.Options{
		SecretName:  objStore.Name,
		SecretSHA1:  hash,
		SharedStore: secretSpec.Type,
		OpenShift:   openShiftOpts,
	}

	credentialMode, err := determineCredentialMode(secretSpec, objStore, fg)
	if err != nil {
		return storage.Options{}, err
	}
	storageOpts.CredentialMode = credentialMode

	switch secretSpec.Type {
	case lokiv1.ObjectStorageSecretAzure:
		storageOpts.Azure, err = extractAzureConfigSecret(objStore, credentialMode)
	case lokiv1.ObjectStorageSecretGCS:
		storageOpts.GCS, err = extractGCSConfigSecret(objStore, credentialMode)
	case lokiv1.ObjectStorageSecretS3:
		storageOpts.S3, err = extractS3ConfigSecret(objStore, credentialMode)
	case lokiv1.ObjectStorageSecretSwift:
		storageOpts.Swift, err = extractSwiftConfigSecret(objStore)
	case lokiv1.ObjectStorageSecretAlibabaCloud:
		storageOpts.AlibabaCloud, err = extractAlibabaCloudConfigSecret(objStore)
	default:
		return storage.Options{}, fmt.Errorf("%w: %s", errSecretUnknownType, secretSpec.Type)
	}

	if err != nil {
		return storage.Options{}, err
	}

	return storageOpts, nil
}

func keyPresent(secret *corev1.Secret, key string) bool {
	data, ok := secret.Data[key]
	if !ok {
		return false
	}

	return len(data) > 0
}

func determineCredentialMode(spec lokiv1.ObjectStorageSecretSpec, secret *corev1.Secret, fg configv1.FeatureGates) (lokiv1.CredentialMode, error) {
	if spec.CredentialMode != "" {
		// Return user-defined credential mode if defined
		return spec.CredentialMode, nil
	}

	if fg.OpenShift.TokenCCOAuthEnv {
		// Default to token cco credential mode on a token-cco-auth installation
		return lokiv1.CredentialModeTokenCCO, nil
	}

	switch spec.Type {
	case lokiv1.ObjectStorageSecretAzure:
		if keyPresent(secret, storage.KeyAzureStorageClientID) {
			return lokiv1.CredentialModeToken, nil
		}
	case lokiv1.ObjectStorageSecretGCS:
		_, credentialType, err := extractGoogleCredentialSource(secret)
		if err != nil {
			return "", err
		}

		if credentialType == gcpAccountTypeExternal {
			return lokiv1.CredentialModeToken, nil
		}
	case lokiv1.ObjectStorageSecretS3:
		if keyPresent(secret, storage.KeyAWSRoleArn) {
			return lokiv1.CredentialModeToken, nil
		}
	case lokiv1.ObjectStorageSecretSwift:
		// does only support static mode
	case lokiv1.ObjectStorageSecretAlibabaCloud:
		// does only support static mode
	default:
		return "", fmt.Errorf("%w: %s", errSecretUnknownType, spec.Type)
	}

	return lokiv1.CredentialModeStatic, nil
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

func extractAzureConfigSecret(s *corev1.Secret, credentialMode lokiv1.CredentialMode) (*storage.AzureStorageConfig, error) {
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

	workloadIdentity, err := validateAzureCredentials(s, credentialMode)
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

func validateAzureCredentials(s *corev1.Secret, credentialMode lokiv1.CredentialMode) (workloadIdentity bool, err error) {
	accountKey := s.Data[storage.KeyAzureStorageAccountKey]
	clientID := s.Data[storage.KeyAzureStorageClientID]
	tenantID := s.Data[storage.KeyAzureStorageTenantID]
	subscriptionID := s.Data[storage.KeyAzureStorageSubscriptionID]

	switch credentialMode {
	case lokiv1.CredentialModeStatic:
		if len(accountKey) == 0 {
			return false, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureStorageAccountKey)
		}

		if err := validateBase64(accountKey); err != nil {
			return false, errAzureInvalidAccountKey
		}

		return false, nil
	case lokiv1.CredentialModeToken:
		if len(clientID) == 0 {
			return false, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureStorageClientID)
		}

		if len(tenantID) == 0 {
			return false, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureStorageTenantID)
		}

		if len(subscriptionID) == 0 {
			return false, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAzureStorageSubscriptionID)
		}

		return true, nil
	case lokiv1.CredentialModeTokenCCO:
		if len(accountKey) > 0 || len(clientID) > 0 || len(tenantID) > 0 || len(subscriptionID) > 0 {
			return false, errAzureManagedIdentityNoOverride
		}

		return true, nil
	}

	return false, fmt.Errorf("%w: %s", errSecretUnknownCredentialMode, credentialMode)
}

func validateBase64(data []byte) error {
	buf := bytes.NewBuffer(data)
	reader := base64.NewDecoder(base64.StdEncoding, buf)
	_, err := io.ReadAll(reader)
	return err
}

func extractGoogleCredentialSource(secret *corev1.Secret) (sourceFile, sourceType string, err error) {
	keyJSON := secret.Data[storage.KeyGCPServiceAccountKeyFilename]
	if len(keyJSON) == 0 {
		return "", "", fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyGCPServiceAccountKeyFilename)
	}

	credentialsFile := struct {
		CredentialsType   string `json:"type"`
		CredentialsSource struct {
			File string `json:"file"`
		} `json:"credential_source"`
	}{}

	err = json.Unmarshal(keyJSON, &credentialsFile)
	if err != nil {
		return "", "", errGCPParseCredentialsFile
	}

	return credentialsFile.CredentialsSource.File, credentialsFile.CredentialsType, nil
}

func extractGCSConfigSecret(s *corev1.Secret, credentialMode lokiv1.CredentialMode) (*storage.GCSStorageConfig, error) {
	// Extract and validate mandatory fields
	bucket := s.Data[storage.KeyGCPStorageBucketName]
	if len(bucket) == 0 {
		return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyGCPStorageBucketName)
	}

	switch credentialMode {
	case lokiv1.CredentialModeStatic:
		return &storage.GCSStorageConfig{
			Bucket: string(bucket),
		}, nil
	case lokiv1.CredentialModeToken:
		audience := string(s.Data[storage.KeyGCPWorkloadIdentityProviderAudience])
		if audience == "" {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyGCPWorkloadIdentityProviderAudience)
		}

		// Check if correct credential source is used
		credentialSource, _, err := extractGoogleCredentialSource(s)
		if err != nil {
			return nil, err
		}

		if credentialSource != storage.ServiceAccountTokenFilePath {
			return nil, fmt.Errorf("%w: %s", errGCPWrongCredentialSourceFile, storage.ServiceAccountTokenFilePath)
		}

		return &storage.GCSStorageConfig{
			Bucket:           string(bucket),
			WorkloadIdentity: true,
			Audience:         audience,
		}, nil
	case lokiv1.CredentialModeTokenCCO:
		return nil, fmt.Errorf("%w: type: %s credentialMode: %s", errSecretUnsupportedCredentialMode, lokiv1.ObjectStorageSecretGCS, credentialMode)
	default:
	}

	return nil, fmt.Errorf("%w: %s", errSecretUnknownCredentialMode, credentialMode)
}

func extractS3ConfigSecret(s *corev1.Secret, credentialMode lokiv1.CredentialMode) (*storage.S3StorageConfig, error) {
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
		region         = s.Data[storage.KeyAWSRegion]
		forcePathStyle = !strings.HasSuffix(string(endpoint), awsEndpointSuffix)
	)

	sseCfg, err := extractS3SSEConfig(s.Data)
	if err != nil {
		return nil, err
	}

	cfg := &storage.S3StorageConfig{
		Buckets:        string(buckets),
		Region:         string(region),
		SSE:            sseCfg,
		ForcePathStyle: forcePathStyle,
	}

	switch credentialMode {
	case lokiv1.CredentialModeTokenCCO:
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
	case lokiv1.CredentialModeStatic:
		cfg.Endpoint = string(endpoint)

		if err := validateS3Endpoint(string(endpoint), string(region)); err != nil {
			return nil, err
		}
		if len(id) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSAccessKeyID)
		}
		if len(secret) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSAccessKeySecret)
		}

		return cfg, nil
	case lokiv1.CredentialModeToken: // Extract STS from user provided values
		cfg.STS = true
		cfg.Audience = string(audience)

		// In the STS case region is not an optional field
		if len(region) == 0 {
			return nil, fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSRegion)
		}
		return cfg, nil
	default:
		return nil, fmt.Errorf("%w: %s", errSecretUnknownCredentialMode, credentialMode)
	}
}

func validateS3Endpoint(endpoint string, region string) error {
	if len(endpoint) == 0 {
		return fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSEndpoint)
	}

	parsedURL, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("%w: %w", errS3EndpointUnparseable, err)
	}

	if parsedURL.Scheme == "" {
		// Assume "just a hostname" when scheme is empty and produce a clearer error message
		return errS3EndpointNoURL
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%w: %s", errS3EndpointUnsupportedScheme, parsedURL.Scheme)
	}

	if strings.HasSuffix(endpoint, awsEndpointSuffix) {
		if len(region) == 0 {
			return fmt.Errorf("%w: %s", errSecretMissingField, storage.KeyAWSRegion)
		}

		validEndpoint := fmt.Sprintf("https://s3.%s%s", region, awsEndpointSuffix)
		if endpoint != validEndpoint {
			return fmt.Errorf("%w: %s", errS3EndpointAWSInvalid, validEndpoint)
		}
	}
	return nil
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
