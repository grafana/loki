package secrets_test

import (
	"testing"

	"github.com/ViaQ/loki-operator/internal/handlers/internal/secrets"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestExtract(t *testing.T) {
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

			_, err := secrets.Extract(tst.secret)
			if !tst.wantErr {
				require.NoError(t, err)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}

func TestExtractGatewaySecret(t *testing.T) {
	type test struct {
		name       string
		tenantName string
		secret     *corev1.Secret
		wantErr    bool
	}
	table := []test{
		{
			name:       "missing clientID",
			tenantName: "tenant-a",
			secret:     &corev1.Secret{},
			wantErr:    true,
		},
		{
			name:       "missing clientSecret",
			tenantName: "tenant-a",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"clientID": []byte("test"),
				},
			},
			wantErr: true,
		},
		{
			name:       "missing issuerCAPath",
			tenantName: "tenant-a",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"clientID":     []byte("test"),
					"clientSecret": []byte("test"),
				},
			},
			wantErr: true,
		},
		{
			name:       "all set",
			tenantName: "tenant-a",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"clientID":     []byte("test"),
					"clientSecret": []byte("test"),
					"issuerCAPath": []byte("/tmp/test"),
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			_, err := secrets.ExtractGatewaySecret(tst.secret, tst.tenantName)
			if !tst.wantErr {
				require.NoError(t, err)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}
