package storage

import "fmt"

const (
	// EnvAlibabaCloudAccessKeyID is the environment variable to specify the AlibabaCloud client id to access S3.
	EnvAlibabaCloudAccessKeyID = "ALIBABA_CLOUD_ACCESS_KEY_ID"
	// EnvAlibabaCloudAccessKeySecret is the environment variable to specify the AlibabaCloud client secret to access S3.
	EnvAlibabaCloudAccessKeySecret = "ALIBABA_CLOUD_ACCESS_KEY_SECRET"
	// EnvAWSAccessKeyID is the environment variable to specify the AWS client id to access S3.
	EnvAWSAccessKeyID = "AWS_ACCESS_KEY_ID"
	// EnvAWSAccessKeySecret is the environment variable to specify the AWS client secret to access S3.
	EnvAWSAccessKeySecret = "AWS_ACCESS_KEY_SECRET" //#nosec G101 -- False positive
	// EnvAWSSseKmsEncryptionContext is the environment variable to specify the AWS KMS encryption context when using type SSE-KMS.
	EnvAWSSseKmsEncryptionContext = "AWS_SSE_KMS_ENCRYPTION_CONTEXT"
	// EnvAWSRoleArn is the environment variable to specify the AWS role ARN secret for the federated identity workflow.
	EnvAWSRoleArn = "AWS_ROLE_ARN"
	// EnvAWSWebIdentityTokenFile is the environment variable to specify the path to the web identity token file used in the federated identity workflow.
	EnvAWSWebIdentityTokenFile = "AWS_WEB_IDENTITY_TOKEN_FILE" //#nosec G101 -- False positive
	// EnvAWSCredentialsFile is the environment variable to specify the path to the shared credentials file
	EnvAWSCredentialsFile = "AWS_SHARED_CREDENTIALS_FILE" //#nosec G101 -- False positive
	// EnvAWSSdkLoadConfig is the environment that enabled the AWS SDK to enable the shared credentials file to be loaded
	EnvAWSSdkLoadConfig = "AWS_SDK_LOAD_CONFIG"
	// EnvAzureStorageAccountName is the environment variable to specify the Azure storage account name to access the container.
	EnvAzureStorageAccountName = "AZURE_STORAGE_ACCOUNT_NAME"
	// EnvAzureStorageAccountKey is the environment variable to specify the Azure storage account key to access the container.
	EnvAzureStorageAccountKey = "AZURE_STORAGE_ACCOUNT_KEY"
	// EnvAzureClientID is the environment variable used to pass the Managed Identity client-ID to the container.
	EnvAzureClientID = "AZURE_CLIENT_ID"
	// EnvAzureTenantID is the environment variable used to pass the Managed Identity tenant-ID to the container.
	EnvAzureTenantID = "AZURE_TENANT_ID"
	// EnvAzureSubscriptionID is the environment variable used to pass the Managed Identity subscription-ID to the container.
	EnvAzureSubscriptionID = "AZURE_SUBSCRIPTION_ID"
	// EnvAzureFederatedTokenFile is the environment variable used to store the path to the Managed Identity token.
	EnvAzureFederatedTokenFile = "AZURE_FEDERATED_TOKEN_FILE"
	// EnvGoogleApplicationCredentials is the environment variable to specify path to key.json
	EnvGoogleApplicationCredentials = "GOOGLE_APPLICATION_CREDENTIALS" //#nosec G101 -- False positive
	// EnvSwiftPassword is the environment variable to specify the OpenStack Swift password.
	EnvSwiftPassword = "SWIFT_PASSWORD"
	// EnvSwiftUsername is the environment variable to specify the OpenStack Swift username.
	EnvSwiftUsername = "SWIFT_USERNAME"

	// KeyAlibabaCloudAccessKeyID is the secret data key for the AlibabaCloud client id to access S3.
	KeyAlibabaCloudAccessKeyID = "access_key_id"
	// KeyAlibabaCloudSecretAccessKey is the secret data key for the AlibabaCloud client secret to access S3.
	KeyAlibabaCloudSecretAccessKey = "secret_access_key"
	// KeyAlibabaCloudBucket is the secret data key for the S3 bucket name.
	KeyAlibabaCloudBucket = "bucket"
	// KeyAlibabaCloudEndpoint is the secret data key for the S3 endpoint URL.
	KeyAlibabaCloudEndpoint = "endpoint"

	// KeyAWSAccessKeyID is the secret data key for the AWS client id to access S3.
	KeyAWSAccessKeyID = "access_key_id"
	// KeyAWSAccessKeySecret is the secret data key for the AWS client secret to access S3.
	KeyAWSAccessKeySecret = "access_key_secret" //#nosec G101 -- False positive
	// KeyAWSBucketNames is the secret data key for the AWS S3 bucket names.
	KeyAWSBucketNames = "bucketnames"
	// KeyAWSEndpoint is the secret data key for the AWS endpoint URL.
	KeyAWSEndpoint = "endpoint"
	// KeyAWSRegion is the secret data key for the AWS region.
	KeyAWSRegion = "region"
	// KeyAWSSSEType is the secret data key for the AWS server-side encryption type.
	KeyAWSSSEType = "sse_type"
	// KeyAWSSseKmsEncryptionContext is the secret data key for the AWS SSE KMS encryption context.
	KeyAWSSseKmsEncryptionContext = "sse_kms_encryption_context"
	// KeyAWSSseKmsKeyID is the secret data key for the AWS SSE KMS key id.
	KeyAWSSseKmsKeyID = "sse_kms_key_id"
	// KeyAWSRoleArn is the secret data key for the AWS STS role ARN.
	KeyAWSRoleArn = "role_arn"
	// KeyAWSAudience is the audience for the AWS STS workflow.
	KeyAWSAudience = "audience"
	// KeyAWSCredentialsFilename is the config filename containing the AWS authentication credentials.
	KeyAWSCredentialsFilename = "credentials"

	// KeyAzureStorageAccountKey is the secret data key for the Azure storage account key.
	KeyAzureStorageAccountKey = "account_key"
	// KeyAzureStorageAccountName is the secret data key for the Azure storage account name.
	KeyAzureStorageAccountName = "account_name"
	// KeyAzureStorageClientID contains the UUID of the Managed Identity accessing the storage.
	KeyAzureStorageClientID = "client_id"
	// KeyAzureStorageTenantID contains the UUID of the Tenant hosting the Managed Identity.
	KeyAzureStorageTenantID = "tenant_id"
	// KeyAzureStorageSubscriptionID contains the UUID of the subscription hosting the Managed Identity.
	KeyAzureStorageSubscriptionID = "subscription_id"
	// KeyAzureStorageContainerName is the secret data key for the Azure storage container name.
	KeyAzureStorageContainerName = "container"
	// KeyAzureStorageEndpointSuffix is the secret data key for the Azure storage endpoint URL suffix.
	KeyAzureStorageEndpointSuffix = "endpoint_suffix"
	// KeyAzureEnvironmentName is the secret data key for the Azure cloud environment name.
	KeyAzureEnvironmentName = "environment"
	// KeyAzureAudience is the secret data key for customizing the audience used for the ServiceAccount token.
	KeyAzureAudience = "audience"

	// KeyGCPWorkloadIdentityProviderAudience is the secret data key for the GCP Workload Identity Provider audience.
	KeyGCPWorkloadIdentityProviderAudience = "audience"
	// KeyGCPStorageBucketName is the secret data key for the GCS bucket name.
	KeyGCPStorageBucketName = "bucketname"
	// KeyGCPServiceAccountKeyFilename is the service account key filename containing the Google authentication credentials.
	KeyGCPServiceAccountKeyFilename = "key.json"
	// KeyGCPManagedServiceAccountKeyFilename is the service account key filename for the managed Google service account.
	KeyGCPManagedServiceAccountKeyFilename = "service_account.json"

	// KeySwiftAuthURL is the secret data key for the OpenStack Swift authentication URL.
	KeySwiftAuthURL = "auth_url"
	// KeySwiftContainerName is the secret data key for the OpenStack Swift container name.
	KeySwiftContainerName = "container_name"
	// KeySwiftDomainID is the secret data key for the OpenStack domain ID.
	KeySwiftDomainID = "domain_id"
	// KeySwiftDomainName is the secret data key for the OpenStack domain name.
	KeySwiftDomainName = "domain_name"
	// KeySwiftPassword is the secret data key for the OpenStack Swift password.
	KeySwiftPassword = "password"
	// KeySwiftProjectDomainId is the secret data key for the OpenStack project's domain id.
	KeySwiftProjectDomainId = "project_domain_id"
	// KeySwiftProjectDomainName is the secret data key for the OpenStack project's domain name.
	KeySwiftProjectDomainName = "project_domain_name"
	// KeySwiftProjectID is the secret data key for the OpenStack project id.
	KeySwiftProjectID = "project_id"
	// KeySwiftProjectName is the secret data key for the OpenStack project name.
	KeySwiftProjectName = "project_name"
	// KeySwiftRegion is the secret data key for the OpenStack Swift region.
	KeySwiftRegion = "region"
	// KeySwiftUserDomainID is the secret data key for the OpenStack Swift user domain id.
	KeySwiftUserDomainID = "user_domain_id"
	// KeySwiftUserDomainName is the secret data key for the OpenStack Swift user domain name.
	KeySwiftUserDomainName = "user_domain_name"
	// KeySwiftUserID is the secret data key for the OpenStack Swift user id.
	KeySwiftUserID = "user_id"
	// KeySwiftUsername is the secret data key for the OpenStack Swift password.
	KeySwiftUsername = "username"

	saTokenVolumeName            = "bound-sa-token"
	saTokenExpiration      int64 = 3600
	saTokenVolumeMountPath       = "/var/run/secrets/storage/serviceaccount" //#nosec G101 -- False positive

	ServiceAccountTokenFilePath = saTokenVolumeMountPath + "/token"

	secretDirectory  = "/etc/storage/secrets" //#nosec G101 -- False positive
	storageTLSVolume = "storage-tls"
	caDirectory      = "/etc/storage/ca"

	tokenAuthConfigVolumeName = "token-auth-config"       //#nosec G101 -- False positive
	tokenAuthConfigDirectory  = "/etc/storage/token-auth" //#nosec G101 -- False positive

	awsDefaultAudience   = "sts.amazonaws.com"
	azureDefaultAudience = "api://AzureADTokenExchange"
	gcpDefaultAudience   = "openshift"

	azureManagedCredentialKeyClientID       = "azure_client_id"
	azureManagedCredentialKeyTenantID       = "azure_tenant_id"
	azureManagedCredentialKeySubscriptionID = "azure_subscription_id"
)

// ManagedCredentialsSecretName returns the name of the secret holding the managed credentials.
func ManagedCredentialsSecretName(stackName string) string {
	return fmt.Sprintf("%s-managed-credentials", stackName)
}
