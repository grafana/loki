package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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
		tc := tc

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

	_, err := extractSecrets("test-unknown-type", &corev1.Secret{}, nil, configv1.FeatureGates{})
	require.EqualError(t, err, wantError)
}

func TestAzureExtract(t *testing.T) {
	type test struct {
		name      string
		secret    *corev1.Secret
		wantError string
	}
	table := []test{
		{
			name:      "missing environment",
			secret:    &corev1.Secret{},
			wantError: "missing secret field: environment",
		},
		{
			name: "missing container",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"environment": []byte("here"),
				},
			},
			wantError: "missing secret field: container",
		},
		{
			name: "missing account_name",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"environment": []byte("here"),
					"container":   []byte("this,that"),
				},
			},
			wantError: "missing secret field: account_name",
		},
		{
			name: "missing account_key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":  []byte("here"),
					"container":    []byte("this,that"),
					"account_name": []byte("id"),
				},
			},
			wantError: "missing secret field: account_key",
		},
		{
			name: "all mandatory set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":  []byte("here"),
					"container":    []byte("this,that"),
					"account_name": []byte("id"),
					"account_key":  []byte("secret"),
				},
			},
		},
		{
			name: "all set including optional",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"environment":     []byte("here"),
					"container":       []byte("this,that"),
					"account_name":    []byte("id"),
					"account_key":     []byte("secret"),
					"endpoint_suffix": []byte("suffix"),
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			opts, err := extractSecrets(lokiv1.ObjectStorageSecretAzure, tst.secret, nil, configv1.FeatureGates{})
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretAzure)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}

func TestGCSExtract(t *testing.T) {
	type test struct {
		name      string
		secret    *corev1.Secret
		wantError string
	}
	table := []test{
		{
			name:      "missing bucketname",
			secret:    &corev1.Secret{},
			wantError: "missing secret field: bucketname",
		},
		{
			name: "missing key.json",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"bucketname": []byte("here"),
				},
			},
			wantError: "missing secret field: key.json",
		},
		{
			name: "all set",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketname": []byte("here"),
					"key.json":   []byte("{\"type\": \"SA\"}"),
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			_, err := extractSecrets(lokiv1.ObjectStorageSecretGCS, tst.secret, nil, configv1.FeatureGates{})
			if tst.wantError == "" {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}

func TestS3Extract(t *testing.T) {
	type test struct {
		name      string
		secret    *corev1.Secret
		wantError string
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
					"endpoint":    []byte("here"),
					"bucketnames": []byte("this,that"),
				},
			},
			wantError: "missing secret field: access_key_id",
		},
		{
			name: "missing access_key_secret",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint":      []byte("here"),
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
					"endpoint":          []byte("here"),
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
					"endpoint":                   []byte("here"),
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
					"endpoint":          []byte("here"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
					"sse_type":          []byte("SSE-KMS"),
					"sse_kms_key_id":    []byte("kms-key-id"),
				},
			},
		},
		{
			name: "all set with SSE-KMS with encryption context",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":                   []byte("here"),
					"bucketnames":                []byte("this,that"),
					"access_key_id":              []byte("id"),
					"access_key_secret":          []byte("secret"),
					"sse_type":                   []byte("SSE-KMS"),
					"sse_kms_key_id":             []byte("kms-key-id"),
					"sse_kms_encryption_context": []byte("kms-encryption-ctx"),
				},
			},
		},
		{
			name: "all set with SSE-S3",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("here"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
					"sse_type":          []byte("SSE-S3"),
				},
			},
		},
		{
			name: "all set without SSE",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"endpoint":          []byte("here"),
					"bucketnames":       []byte("this,that"),
					"access_key_id":     []byte("id"),
					"access_key_secret": []byte("secret"),
				},
			},
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
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			opts, err := extractSecrets(lokiv1.ObjectStorageSecretS3, tst.secret, nil, configv1.FeatureGates{})
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretS3)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}

func TestS3Extract_WithOpenShiftManagedAuth(t *testing.T) {
	fg := configv1.FeatureGates{
		OpenShift: configv1.OpenShiftFeatureGates{
			Enabled:        true,
			ManagedAuthEnv: true,
		},
	}
	type test struct {
		name              string
		secret            *corev1.Secret
		managedAuthSecret *corev1.Secret
		wantError         string
	}
	table := []test{
		{
			name:              "missing bucketnames",
			secret:            &corev1.Secret{},
			managedAuthSecret: &corev1.Secret{},
			wantError:         "missing secret field: bucketnames",
		},
		{
			name: "missing region",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
				},
			},
			managedAuthSecret: &corev1.Secret{},
			wantError:         "missing secret field: region",
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
			managedAuthSecret: &corev1.Secret{},
			wantError:         "secret field not allowed: role_arn",
		},
		{
			name: "override audience not allowed",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Data: map[string][]byte{
					"bucketnames": []byte("this,that"),
					"audience":    []byte("test-audience"),
				},
			},
			managedAuthSecret: &corev1.Secret{},
			wantError:         "secret field not allowed: audience",
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
			managedAuthSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "managed-auth"},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			opts, err := extractSecrets(lokiv1.ObjectStorageSecretS3, tst.secret, tst.managedAuthSecret, fg)
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretS3)
				require.True(t, opts.S3.STS)
				require.Equal(t, opts.S3.Audience, "openshift")
				require.Equal(t, opts.OpenShift.CloudCredentials.SecretName, tst.managedAuthSecret.Name)
				require.NotEmpty(t, opts.OpenShift.CloudCredentials.SHA1)
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
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			opts, err := extractSecrets(lokiv1.ObjectStorageSecretSwift, tst.secret, nil, configv1.FeatureGates{})
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretSwift)
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
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			opts, err := extractSecrets(lokiv1.ObjectStorageSecretAlibabaCloud, tst.secret, nil, configv1.FeatureGates{})
			if tst.wantError == "" {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretAlibabaCloud)
			} else {
				require.EqualError(t, err, tst.wantError)
			}
		})
	}
}
