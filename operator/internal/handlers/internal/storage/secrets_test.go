package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestHashSecretData(t *testing.T) {
	tt := []struct {
		desc     string
		data     map[string][]byte
		wantHash string
	}{
		{
			desc:     "nil",
			data:     nil,
			wantHash: "da39a3ee5e6b4b0d3255bfef95601890afd80709",
		},
		{
			desc:     "empty",
			data:     map[string][]byte{},
			wantHash: "da39a3ee5e6b4b0d3255bfef95601890afd80709",
		},
		{
			desc: "single entry",
			data: map[string][]byte{
				"key": []byte("value"),
			},
			wantHash: "a8973b2094d3af1e43931132dee228909bf2b02a",
		},
		{
			desc: "multiple entries",
			data: map[string][]byte{
				"key":  []byte("value"),
				"key3": []byte("value3"),
				"key2": []byte("value2"),
			},
			wantHash: "a3341093891ad4df9f07db586029be48e9e6e884",
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			s := &corev1.Secret{
				Data: tc.data,
			}

			hash, err := hashSecretData(s)
			require.NoError(t, err)
			require.Equal(t, tc.wantHash, hash)
		})
	}
}

func TestUnknownType(t *testing.T) {
	wantError := "unknown secret type: test-unknown-type"

	spec := lokiv1.ObjectStorageSecretSpec{
		Type: "test-unknown-type",
	}
	_, err := extractSecrets(spec, &corev1.Secret{}, nil, configv1.FeatureGates{})
	require.EqualError(t, err, wantError)
}

func TestAzureExtract(t *testing.T) {
	type test struct {
		name               string
		secret             *corev1.Secret
		managedSecret      *corev1.Secret
		featureGates       configv1.FeatureGates
		wantError          string
		wantCredentialMode lokiv1.CredentialMode
	}
	table := []test{
		{
			name:      "missing environment",
			secret:    &corev1.Secret{},
			wantError: "missing secret field: environment",
		},
		{
			name: "invalid environment",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"environment": []byte("invalid-environment"),
				},
			},
			wantError: "azure environment invalid (valid values: AzureGlobal, AzureChinaCloud, AzureGermanCloud, AzureUSGovernment): invalid-environment",
		},
		{
			name: "missing account_name",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"environment": []byte("AzureGlobal"),
				},
			},
			wantError: "missing secret field: account_name",
		},
		{
			name: "missing container",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"environment":  []byte("AzureGlobal"),
					"account_name": []byte("id"),
				},
			},
			wantError: "missing secret field: container",
		},
		{
			name: "missing tenant_id",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":  []byte("AzureGlobal"),
					"container":    []byte("this,that"),
					"account_name": []byte("test-account-name"),
					"client_id":    []byte("test-client-id"),
				},
			},
			wantError: "missing secret field: tenant_id",
		},
		{
			name: "missing subscription_id",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":  []byte("AzureGlobal"),
					"container":    []byte("this,that"),
					"account_name": []byte("test-account-name"),
					"client_id":    []byte("test-client-id"),
					"tenant_id":    []byte("test-tenant-id"),
				},
			},
			wantError: "missing secret field: subscription_id",
		},
		{
			name: "token cco auth - no auth override",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":  []byte("AzureGlobal"),
					"account_name": []byte("test-account-name"),
					"container":    []byte("this,that"),
					"region":       []byte("test-region"),
					"account_key":  []byte("test-account-key"),
				},
			},
			managedSecret: &corev1.Secret{
				Data: map[string][]byte{},
			},
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled:         true,
					TokenCCOAuthEnv: true,
				},
			},
			wantError: errAzureManagedIdentityNoOverride.Error(),
		},
		{
			name: "audience used with static authentication",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":  []byte("AzureGlobal"),
					"container":    []byte("this,that"),
					"account_name": []byte("id"),
					"account_key":  []byte("dGVzdC1hY2NvdW50LWtleQ=="), // test-account-key
					"audience":     []byte("test-audience"),
				},
			},
			wantError: "secret field not allowed: audience",
		},
		{
			name: "mandatory for normal authentication set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":  []byte("AzureGlobal"),
					"container":    []byte("this,that"),
					"account_name": []byte("id"),
					"account_key":  []byte("dGVzdC1hY2NvdW50LWtleQ=="), // test-account-key
				},
			},
			wantCredentialMode: lokiv1.CredentialModeStatic,
		},
		{
			name: "mandatory for workload-identity set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":     []byte("AzureGlobal"),
					"container":       []byte("this,that"),
					"account_name":    []byte("test-account-name"),
					"client_id":       []byte("test-client-id"),
					"tenant_id":       []byte("test-tenant-id"),
					"subscription_id": []byte("test-subscription-id"),
					"region":          []byte("test-region"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeToken,
		},
		{
			name: "mandatory for managed workload-identity set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":  []byte("AzureGlobal"),
					"account_name": []byte("test-account-name"),
					"container":    []byte("this,that"),
					"region":       []byte("test-region"),
				},
			},
			managedSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-secret",
				},
				Data: map[string][]byte{
					"azure_client_id":       []byte("test-client-id"),
					"azure_tenant_id":       []byte("test-tenant-id"),
					"azure_subscription_id": []byte("test-subscription-id"),
				},
			},
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled:         true,
					TokenCCOAuthEnv: true,
				},
			},
			wantCredentialMode: lokiv1.CredentialModeTokenCCO,
		},
		{
			name: "all set including optional",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":     []byte("AzureGlobal"),
					"container":       []byte("this,that"),
					"account_name":    []byte("id"),
					"account_key":     []byte("dGVzdC1hY2NvdW50LWtleQ=="), // test-account-key
					"endpoint_suffix": []byte("suffix"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeStatic,
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			spec := lokiv1.ObjectStorageSecretSpec{
				Type: lokiv1.ObjectStorageSecretAzure,
			}

			opts, err := extractSecrets(spec, tst.secret, tst.managedSecret, tst.featureGates)
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, lokiv1.ObjectStorageSecretAzure, opts.SharedStore)
				require.Equal(t, tst.wantCredentialMode, opts.CredentialMode)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}

func TestGCSExtract(t *testing.T) {
	type test struct {
		name               string
		secret             *corev1.Secret
		tokenAuth          *corev1.Secret
		featureGates       configv1.FeatureGates
		wantError          string
		wantCredentialMode lokiv1.CredentialMode
	}
	table := []test{
		{
			name: "missing key.json",
			secret: &corev1.Secret{
				Data: map[string][]byte{},
			},
			wantError: "missing secret field: key.json",
		},
		{
			name: "missing bucketname",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"key.json": []byte("{\"type\": \"service_account\"}"),
				},
			},
			wantError: "missing secret field: bucketname",
		},
		{
			name: "missing audience",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketname": []byte("here"),
					"key.json":   []byte("{\"type\": \"external_account\"}"),
				},
			},
			wantError: "missing secret field: audience",
		},
		{
			name: "credential_source file no override",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketname": []byte("here"),
					"audience":   []byte("test"),
					"key.json":   []byte("{\"type\": \"external_account\", \"credential_source\": {\"file\": \"/custom/path/to/secret/storage/serviceaccount/token\"}}"),
				},
			},
			wantError: "credential source in secret needs to point to token file: /var/run/secrets/storage/serviceaccount/token",
		},
		{
			name: "all set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketname": []byte("here"),
					"key.json":   []byte("{\"type\": \"service_account\"}"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeStatic,
		},
		{
			name: "mandatory for workload-identity set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketname": []byte("here"),
					"audience":   []byte("test"),
					"key.json":   []byte("{\"type\": \"external_account\", \"credential_source\": {\"file\": \"/var/run/secrets/storage/serviceaccount/token\"}}"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeToken,
		},
		{
			name: "invalid for token CCO",
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled:         true,
					TokenCCOAuthEnv: true,
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketname": []byte("here"),
					"key.json":   []byte("{\"type\": \"external_account\", \"audience\": \"\", \"service_account_id\": \"\"}"),
				},
			},
			wantError: "gcp credentials file contains invalid fields: key.json must not be set for CredentialModeTokenCCO",
		},
		{
			name: "valid for token CCO",
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled:         true,
					TokenCCOAuthEnv: true,
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketname": []byte("here"),
				},
			},
			tokenAuth: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "token-auth-config"},
				Data: map[string][]byte{
					"service_account.json": []byte("{\"type\": \"external_account\", \"audience\": \"test\", \"service_account_id\": \"\"}"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeTokenCCO,
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			spec := lokiv1.ObjectStorageSecretSpec{
				Type: lokiv1.ObjectStorageSecretGCS,
			}

			opts, err := extractSecrets(spec, tst.secret, tst.tokenAuth, tst.featureGates)
			if tst.wantError == "" {
				require.NoError(t, err)
				require.Equal(t, tst.wantCredentialMode, opts.CredentialMode)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}

func TestS3Extract(t *testing.T) {
	type test struct {
		name               string
		secret             *corev1.Secret
		wantError          string
		wantCredentialMode lokiv1.CredentialMode
	}
	table := []test{
		{
			name:      "missing bucketnames",
			secret:    &corev1.Secret{},
			wantError: "missing secret field: bucketnames",
		},
		{
			name: "missing endpoint",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
				},
			},
			wantError: "missing secret field: endpoint",
		},
		{
			name: "missing access_key_id",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint":    []byte("https://s3.test-region.amazonaws.com"),
					"region":      []byte("test-region"),
					"bucketnames": []byte("this,that"),
				},
			},
			wantError: "missing secret field: access_key_id",
		},
		{
			name: "missing access_key_secret",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint":      []byte("https://s3.test-region.amazonaws.com"),
					"region":        []byte("test-region"),
					"bucketnames":   []byte("this,that"),
					"access_key_id": []byte("id"),
				},
			},
			wantError: "missing secret field: access_key_secret",
		},
		{
			name: "unsupported SSE type",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint":          []byte("https://s3.REGION.amazonaws.com"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
					"sse_type":          []byte("unsupported"),
				},
			},
			wantError: "unsupported SSE type (supported: SSE-KMS, SSE-S3): unsupported",
		},
		{
			name: "missing SSE-KMS kms_key_id",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint":                   []byte("https://s3.test-region.amazonaws.com"),
					"region":                     []byte("test-region"),
					"bucketnames":                []byte("this,that"),
					"access_key_id":              []byte("id"),
					"access_key_secret":          []byte("secret"),
					"sse_type":                   []byte("SSE-KMS"),
					"sse_kms_encryption_context": []byte("kms-encryption-ctx"),
				},
			},
			wantError: "missing secret field: sse_kms_key_id",
		},
		{
			name: "all set with SSE-KMS",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("https://s3.test-region.amazonaws.com"),
					"region":            []byte("test-region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
					"sse_type":          []byte("SSE-KMS"),
					"sse_kms_key_id":    []byte("kms-key-id"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeStatic,
		},
		{
			name: "all set with SSE-KMS with encryption context",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":                   []byte("https://s3.test-region.amazonaws.com"),
					"region":                     []byte("test-region"),
					"bucketnames":                []byte("this,that"),
					"access_key_id":              []byte("id"),
					"access_key_secret":          []byte("secret"),
					"sse_type":                   []byte("SSE-KMS"),
					"sse_kms_key_id":             []byte("kms-key-id"),
					"sse_kms_encryption_context": []byte("kms-encryption-ctx"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeStatic,
		},
		{
			name: "all set with SSE-S3",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("https://s3.test-region.amazonaws.com"),
					"region":            []byte("test-region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
					"sse_type":          []byte("SSE-S3"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeStatic,
		},
		{
			name: "all set without SSE",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("https://s3.test-region.amazonaws.com"),
					"region":            []byte("test-region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeStatic,
		},
		{
			name: "STS missing region",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
					"role_arn":    []byte("role"),
				},
			},
			wantError: "missing secret field: region",
		},
		{
			name: "STS with region",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
					"role_arn":    []byte("role"),
					"region":      []byte("here"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeToken,
		},
		{
			name: "STS all set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
					"role_arn":    []byte("role"),
					"region":      []byte("here"),
					"audience":    []byte("audience"),
				},
			},
			wantCredentialMode: lokiv1.CredentialModeToken,
		},
		{
			name: "endpoint is just hostname",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("hostname.example.com"),
					"region":            []byte("region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
				},
			},
			wantError: "endpoint for S3 must be an HTTP or HTTPS URL",
		},
		{
			name: "endpoint unsupported scheme",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("invalid://hostname"),
					"region":            []byte("region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
				},
			},
			wantError: "scheme of S3 endpoint URL is unsupported: invalid",
		},
		{
			name: "s3 region used in endpoint URL is incorrect",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("https://s3.wrong.amazonaws.com"),
					"region":            []byte("region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
				},
			},
			wantError: "endpoint for AWS S3 must include correct region: https://s3.region.amazonaws.com",
		},
		{
			name: "s3 endpoint format is not a valid s3 URL",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("http://region.amazonaws.com"),
					"region":            []byte("region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
				},
			},
			wantError: "endpoint for AWS S3 must include correct region: https://s3.region.amazonaws.com",
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			spec := lokiv1.ObjectStorageSecretSpec{
				Type: lokiv1.ObjectStorageSecretS3,
			}

			opts, err := extractSecrets(spec, tst.secret, nil, configv1.FeatureGates{})
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, lokiv1.ObjectStorageSecretS3, opts.SharedStore)
				require.Equal(t, tst.wantCredentialMode, opts.CredentialMode)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}

func TestS3Extract_S3ForcePathStyle(t *testing.T) {
	tt := []struct {
		desc        string
		secret      *corev1.Secret
		wantOptions *storage.S3StorageConfig
	}{
		{
			desc: "aws s3 endpoint",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("https://s3.region.amazonaws.com"),
					"region":            []byte("region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
				},
			},
			wantOptions: &storage.S3StorageConfig{
				Endpoint: "https://s3.region.amazonaws.com",
				Region:   "region",
				Buckets:  "this,that",
			},
		},
		{
			desc: "non-aws s3 endpoint",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("https://test.default.svc.cluster.local:9000"),
					"region":            []byte("region"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
				},
			},
			wantOptions: &storage.S3StorageConfig{
				Endpoint:       "https://test.default.svc.cluster.local:9000",
				Region:         "region",
				Buckets:        "this,that",
				ForcePathStyle: true,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			options, err := extractS3ConfigSecret(tc.secret, lokiv1.CredentialModeStatic)
			require.NoError(t, err)
			require.Equal(t, tc.wantOptions, options)
		})
	}
}

func TestS3Extract_WithOpenShiftTokenCCOAuth(t *testing.T) {
	fg := configv1.FeatureGates{
		OpenShift: configv1.OpenShiftFeatureGates{
			Enabled:         true,
			TokenCCOAuthEnv: true,
		},
	}
	type test struct {
		name               string
		secret             *corev1.Secret
		tokenCCOAuthSecret *corev1.Secret
		wantError          string
	}
	table := []test{
		{
			name:               "missing bucketnames",
			secret:             &corev1.Secret{},
			tokenCCOAuthSecret: &corev1.Secret{},
			wantError:          "missing secret field: bucketnames",
		},
		{
			name: "missing region",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
				},
			},
			tokenCCOAuthSecret: &corev1.Secret{},
			wantError:          "missing secret field: region",
		},
		{
			name: "override role_arn not allowed",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
					"role_arn":    []byte("role-arn"),
				},
			},
			tokenCCOAuthSecret: &corev1.Secret{},
			wantError:          "secret field not allowed: role_arn",
		},
		{
			name: "STS all set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
					"region":      []byte("a-region"),
				},
			},
			tokenCCOAuthSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "token-cco-auth"},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			spec := lokiv1.ObjectStorageSecretSpec{
				Type: lokiv1.ObjectStorageSecretS3,
			}

			opts, err := extractSecrets(spec, tst.secret, tst.tokenCCOAuthSecret, fg)
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, lokiv1.ObjectStorageSecretS3, opts.SharedStore)
				require.True(t, opts.S3.STS)
				require.Equal(t, tst.tokenCCOAuthSecret.Name, opts.OpenShift.CloudCredentials.SecretName)
				require.NotEmpty(t, opts.OpenShift.CloudCredentials.SHA1)
				require.Equal(t, lokiv1.CredentialModeTokenCCO, opts.CredentialMode)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}

func TestSwiftExtract(t *testing.T) {
	type test struct {
		name      string
		secret    *corev1.Secret
		wantError string
	}
	table := []test{
		{
			name:      "missing auth_url",
			secret:    &corev1.Secret{},
			wantError: "missing secret field: auth_url",
		},
		{
			name: "missing username",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url": []byte("here"),
				},
			},
			wantError: "missing secret field: username",
		},
		{
			name: "missing user_domain_name",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url": []byte("here"),
					"username": []byte("this,that"),
				},
			},
			wantError: "missing secret field: user_domain_name",
		},
		{
			name: "missing user_domain_id",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url":         []byte("here"),
					"username":         []byte("this,that"),
					"user_domain_name": []byte("id"),
				},
			},
			wantError: "missing secret field: user_domain_id",
		},
		{
			name: "missing user_id",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url":         []byte("here"),
					"username":         []byte("this,that"),
					"user_domain_name": []byte("id"),
					"user_domain_id":   []byte("secret"),
				},
			},
			wantError: "missing secret field: user_id",
		},
		{
			name: "missing password",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url":         []byte("here"),
					"username":         []byte("this,that"),
					"user_domain_name": []byte("id"),
					"user_domain_id":   []byte("secret"),
					"user_id":          []byte("there"),
				},
			},
			wantError: "missing secret field: password",
		},
		{
			name: "missing domain_id",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url":         []byte("here"),
					"username":         []byte("this,that"),
					"user_domain_name": []byte("id"),
					"user_domain_id":   []byte("secret"),
					"user_id":          []byte("there"),
					"password":         []byte("cred"),
				},
			},
			wantError: "missing secret field: domain_id",
		},
		{
			name: "missing domain_name",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url":         []byte("here"),
					"username":         []byte("this,that"),
					"user_domain_name": []byte("id"),
					"user_domain_id":   []byte("secret"),
					"user_id":          []byte("there"),
					"password":         []byte("cred"),
					"domain_id":        []byte("text"),
				},
			},
			wantError: "missing secret field: domain_name",
		},
		{
			name: "missing container_name",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url":         []byte("here"),
					"username":         []byte("this,that"),
					"user_domain_name": []byte("id"),
					"user_domain_id":   []byte("secret"),
					"user_id":          []byte("there"),
					"password":         []byte("cred"),
					"domain_id":        []byte("text"),
					"domain_name":      []byte("where"),
				},
			},
			wantError: "missing secret field: container_name",
		},
		{
			name: "all set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"auth_url":         []byte("here"),
					"username":         []byte("this,that"),
					"user_domain_name": []byte("id"),
					"user_domain_id":   []byte("secret"),
					"user_id":          []byte("there"),
					"password":         []byte("cred"),
					"domain_id":        []byte("text"),
					"domain_name":      []byte("where"),
					"container_name":   []byte("then"),
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			spec := lokiv1.ObjectStorageSecretSpec{
				Type: lokiv1.ObjectStorageSecretSwift,
			}

			opts, err := extractSecrets(spec, tst.secret, nil, configv1.FeatureGates{})
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, lokiv1.ObjectStorageSecretSwift, opts.SharedStore)
				require.Equal(t, lokiv1.CredentialModeStatic, opts.CredentialMode)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}

func TestAlibabaCloudExtract(t *testing.T) {
	type test struct {
		name      string
		secret    *corev1.Secret
		wantError string
	}
	table := []test{
		{
			name:      "missing endpoint",
			secret:    &corev1.Secret{},
			wantError: "missing secret field: endpoint",
		},
		{
			name: "missing bucket",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint": []byte("here"),
				},
			},
			wantError: "missing secret field: bucket",
		},
		{
			name: "missing access_key_id",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint": []byte("here"),
					"bucket":   []byte("this,that"),
				},
			},
			wantError: "missing secret field: access_key_id",
		},
		{
			name: "missing secret_access_key",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint":      []byte("here"),
					"bucket":        []byte("this,that"),
					"access_key_id": []byte("id"),
				},
			},
			wantError: "missing secret field: secret_access_key",
		},
		{
			name: "all set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("here"),
					"bucket":            []byte("this,that"),
					"access_key_id":     []byte("id"),
					"secret_access_key": []byte("secret"),
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			spec := lokiv1.ObjectStorageSecretSpec{
				Type: lokiv1.ObjectStorageSecretAlibabaCloud,
			}

			opts, err := extractSecrets(spec, tst.secret, nil, configv1.FeatureGates{})
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, lokiv1.ObjectStorageSecretAlibabaCloud, opts.SharedStore)
				require.Equal(t, lokiv1.CredentialModeStatic, opts.CredentialMode)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}
