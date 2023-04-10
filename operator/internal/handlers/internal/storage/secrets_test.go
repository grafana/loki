package storage_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/handlers/internal/storage"
)

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
				Data: map[string][]byte{
					"environment":  []byte("here"),
					"container":    []byte("this,that"),
					"account_name": []byte("id"),
				},
			},
			wantErr: true,
		},
		{
			name: "all set",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"environment":  []byte("here"),
					"container":    []byte("this,that"),
					"account_name": []byte("id"),
					"account_key":  []byte("secret"),
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			_, err := storage.ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretAzure)
			if !tst.wantErr {
				require.NoError(t, err)
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

			_, err := storage.ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretGCS)
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
			name: "all set",
			secret: &corev1.Secret{
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

			_, err := storage.ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretS3)
			if !tst.wantErr {
				require.NoError(t, err)
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

			_, err := storage.ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretSwift)
			if !tst.wantErr {
				require.NoError(t, err)
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

			_, err := storage.ExtractSecret(tst.secret, lokiv1.ObjectStorageSecretAlibabaCloud)
			if !tst.wantErr {
				require.NoError(t, err)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}
