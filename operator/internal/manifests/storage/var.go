package storage

const (
	// EnvAlibabaCloudAccessKeyID is the environment variable to specify the AlibabaCloud client id to access S3.
	EnvAlibabaCloudAccessKeyID = "ALIBABA_CLOUD_ACCESS_KEY_ID"
	// EnvAlibabaCloudAccessKeySecret is the environment variable to specify the AlibabaCloud client secret to access S3.
	EnvAlibabaCloudAccessKeySecret = "ALIBABA_CLOUD_ACCESS_KEY_SECRET"
	// EnvAWSAccessKeyID is the environment variable to specify the AWS client id to access S3.
	EnvAWSAccessKeyID = "AWS_ACCESS_KEY_ID"
	// EnvAWSAccessKeySecret is the environment variable to specify the AWS client secret to access S3.
	EnvAWSAccessKeySecret = "AWS_ACCESS_KEY_SECRET"
	// EnvAWSSseKmsEncryptionContext is the environment variable to specity the AWS KMS encryption context when using type SSE-KMS.
	EnvAWSSseKmsEncryptionContext = "AWS_SSE_KMS_ENCRYPTION_CONTEXT"
	// EnvAzureStorageAccountName is the environment variable to specify the Azure storage account name to access the container.
	EnvAzureStorageAccountName = "AZURE_STORAGE_ACCOUNT_NAME"
	// EnvAzureStorageAccountKey is the environment variable to specify the Azure storage account key to access the container.
	EnvAzureStorageAccountKey = "AZURE_STORAGE_ACCOUNT_KEY"
	// EnvGoogleApplicationCredentials is the environment variable to specify path to key.json
	EnvGoogleApplicationCredentials = "GOOGLE_APPLICATION_CREDENTIALS"
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
	KeyAWSAccessKeySecret = "access_key_secret"
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

	// KeyAzureStorageAccountKey is the secret data key for the Azure storage account key.
	KeyAzureStorageAccountKey = "account_key"
	// KeyAzureStorageAccountName is the secret data key for the Azure storage account name.
	KeyAzureStorageAccountName = "account_name"
	// KeyAzureStorageContainerName is the secret data key for the Azure storage container name.
	KeyAzureStorageContainerName = "container"
	// KeyAzureStorageEndpointSuffix is the secret data key for the Azure storage endpoint URL suffix.
	KeyAzureStorageEndpointSuffix = "endpoint_suffix"
	// KeyAzureEnvironmentName is the secret data key for the Azure cloud environment name.
	KeyAzureEnvironmentName = "environment"

	// KeyGCPStorageBucketName is the secret data key for the GCS bucket name.
	KeyGCPStorageBucketName = "bucketname"
	// KeyGCPServiceAccountKeyFilename is the service account key filename containing the Google authentication credentials.
	KeyGCPServiceAccountKeyFilename = "key.json"

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
	// KeySwiftUserDomainID is the secret data key for the OpenStack Swift user domain name.
	KeySwiftUserDomainName = "user_domain_name"
	// KeySwiftUserID is the secret data key for the OpenStack Swift user id.
	KeySwiftUserID = "user_id"
	// KeySwiftPassword is the secret data key for the OpenStack Swift password.
	KeySwiftUsername = "username"

	secretDirectory  = "/etc/storage/secrets"
	storageTLSVolume = "storage-tls"
	caDirectory      = "/etc/storage/ca"
)
