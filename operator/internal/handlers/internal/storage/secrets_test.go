package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

func TestAzureExtract(t *testing.T) {
	type test struct {
		name    string
		secret  *corev1.Secret
		wantErr bool
	}
	table := []test{
		{
			name:    "missing environment",
			secret:  &corev1.Secret{},
			wantErr: true,
		},
		{
			name: "missing container",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"environment": []byte("here"),
				},
			},
			wantErr: true,
		},
		{
			name: "missing account_name",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"environment": []byte("here"),
					"container":   []byte("this,that"),
				},
			},
			wantErr: true,
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
			wantErr: true,
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

			opts, err := ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretAzure)
			if !tst.wantErr {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretAzure)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}

func TestGCSExtract(t *testing.T) {
	type test struct {
		name    string
		secret  *corev1.Secret
		wantErr bool
	}
	table := []test{
		{
			name:    "missing bucketname",
			secret:  &corev1.Secret{},
			wantErr: true,
		},
		{
			name: "missing key.json",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"bucketname": []byte("here"),
				},
			},
			wantErr: true,
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

			_, err := ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretGCS)
			if !tst.wantErr {
				require.NoError(t, err)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}

func TestS3Extract(t *testing.T) {
	type test struct {
		name    string
		secret  *corev1.Secret
		wantErr bool
	}
	table := []test{
		{
			name:    "missing endpoint",
			secret:  &corev1.Secret{},
			wantErr: true,
		},
		{
			name: "missing bucketnames",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint": []byte("here"),
				},
			},
			wantErr: true,
		},
		{
			name: "missing access_key_id",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint":    []byte("here"),
					"bucketnames": []byte("this,that"),
				},
			},
			wantErr: true,
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
			wantErr: true,
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
			wantErr: true,
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
			wantErr: true,
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
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			opts, err := ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretS3)
			if !tst.wantErr {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretS3)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}

func TestSwiftExtract(t *testing.T) {
	type test struct {
		name    string
		secret  *corev1.Secret
		wantErr bool
	}
	table := []test{
		{
			name:    "missing auth_url",
			secret:  &corev1.Secret{},
			wantErr: true,
		},
		{
			name: "missing username",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url": []byte("here"),
				},
			},
			wantErr: true,
		},
		{
			name: "missing user_domain_name",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"auth_url": []byte("here"),
					"username": []byte("this,that"),
				},
			},
			wantErr: true,
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
			wantErr: true,
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
			wantErr: true,
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
			wantErr: true,
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
			wantErr: true,
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
			wantErr: true,
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
			wantErr: true,
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

			opts, err := ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretSwift)
			if !tst.wantErr {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretSwift)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}

func TestAlibabaCloudExtract(t *testing.T) {
	type test struct {
		name    string
		secret  *corev1.Secret
		wantErr bool
	}
	table := []test{
		{
			name:    "missing endpoint",
			secret:  &corev1.Secret{},
			wantErr: true,
		},
		{
			name: "missing bucketnames",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint": []byte("here"),
				},
			},
			wantErr: true,
		},
		{
			name: "missing access_key_id",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint": []byte("here"),
					"bucket":   []byte("this,that"),
				},
			},
			wantErr: true,
		},
		{
			name: "missing access_key_secret",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"endpoint":      []byte("here"),
					"bucket":        []byte("this,that"),
					"access_key_id": []byte("id"),
				},
			},
			wantErr: true,
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

			opts, err := ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretAlibabaCloud)
			if !tst.wantErr {
				require.NoError(t, err)
				require.NotEmpty(t, opts.SecretName)
				require.NotEmpty(t, opts.SecretSHA1)
				require.Equal(t, opts.SharedStore, lokiv1.ObjectStorageSecretAlibabaCloud)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}
