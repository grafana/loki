package storage

const (
	// EnvAlibabaCloudAccessKeyID is the environment variable to specify the AlibabaCloud client id to access S3.
	EnvAlibabaCloudAccessKeyID = "ALIBABA_CLOUD_ACCESS_KEY_ID"
	// EnvAlibabaCloudAccessKeySecret is the environment variable to specify the AlibabaCloud client secret to access S3.
	EnvAlibabaCloudAccessKeySecret = "ALIBABA_CLOUD_ACCESS_KEY_SECRET"
	// EnvAWSAccessKeyID is the environment variable to specify the AWS client id to access S3.
	EnvAWSAccessKeyID = "AWS_ACCESS_KEY_ID"
	// EnvAWSAccessKeySecre is the environment variable to specify the AWS client secret to access S3.
	EnvAWSAccessKeySecret = "AWS_ACCESS_KEY_SECRET"
	// EnvAzureStorageAccountName is the environment variable to specify the Azure storage account name to access the container.
	EnvAzureStorageAccountName = "AZURE_STORAGE_ACCOUNT_NAME"
	// EnvAzureStorageAccountKey is the environment variable to specify the Azure storage account key to access the container.
	EnvAzureStorageAccountKey = "AZURE_STORAGE_ACCOUNT_KEY"
	// EnvGoogleApplicationCredentials is the environment variable to specify path to key.json
	EnvGoogleApplicationCredentials = "GOOGLE_APPLICATION_CREDENTIALS"

	EnvOpenStackSwiftUsername = "OS_SWIFT_USERNAME"
	EnvOpenStackSwiftPassword = "OS_SWIFT_PASSWORD"

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
	// KeyAWSSSEKMSEncryptionContext is the secret data key for the AWS SSE KMS encryption context.
	KeyAWSSSEKMSEncryptionContext = "sse_kms_encryption_context"
	// KeyAWSSSEKMSKeyID is the secret data key for the AWS SSE KMS key id.
	KeyAWSSSEKMSKeyID = "sse_kms_key_id"

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

	// KeyOSSwiftAuthURL is the secret data key for the OpenStack Swift authentication URL.
	KeyOSSwiftAuthURL = "auth_url"
	// KeyOSSwiftContainerName is the secret data key for the OpenStack Swift container name.
	KeyOSSwiftContainerName = "container_name"
	// KeyOSSwiftDomainID is the secret data key for the OpenStack domain ID.
	KeyOSSwiftDomainID = "domain_id"
	// KeyOSSwiftDomainName is the secret data key for the OpenStack domain name.
	KeyOSSwiftDomainName = "domain_name"
	// KeyOSSwiftPassword is the secret data key for the OpenStack Swift password.
	KeyOSSwiftPassword = "password"
	// KeyOSSwiftProjectDomainId is the secret data key for the OpenStack project's domain id.
	KeyOSSwiftProjectDomainId = "project_domain_id"
	// KeyOSSwiftProjectDomainName is the secret data key for the OpenStack project's domain name.
	KeyOSSwiftProjectDomainName = "project_domain_name"
	// KeyOSSwiftProjectID is the secret data key for the OpenStack project id.
	KeyOSSwiftProjectID = "project_id"
	// KeyOSSwiftProjectName is the secret data key for the OpenStack project name.
	KeyOSSwiftProjectName = "project_name"
	// KeyOSSwiftRegion is the secret data key for the OpenStack Swift region.
	KeyOSSwiftRegion = "region"
	// KeyOSSwiftUserDomainID is the secret data key for the OpenStack Swift user domain id.
	KeyOSSwiftUserDomainID = "user_domain_id"
	// KeyOSSwiftUserDomainID is the secret data key for the OpenStack Swift user domain name.
	KeyOSSwiftUserDomainName = "user_domain_name"
	// KeyOSSwiftUserID is the secret data key for the OpenStack Swift user id.
	KeyOSSwiftUserID = "user_id"
	// KeyOSSwiftPassword is the secret data key for the OpenStack Swift password.
	KeyOSSwiftUsername = "username"

	secretDirectory  = "/etc/storage/secrets"
	storageTLSVolume = "storage-tls"
	caDirectory      = "/etc/storage/ca"
)
