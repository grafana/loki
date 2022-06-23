package rules_test

import (
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"github.com/grafana/loki/operator/internal/handlers/internal/rules"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestExtractRulerSecret(t *testing.T) {
	type test struct {
		name       string
		authType   lokiv1beta1.RemoteWriteAuthType
		secret     *corev1.Secret
		wantSecret *manifests.RulerSecret
		wantErr    bool
	}
	table := []test{
		{
			name:     "missing username",
			authType: lokiv1beta1.BasicAuthorization,
			secret:   &corev1.Secret{},
			wantErr:  true,
		},
		{
			name:     "missing password",
			authType: lokiv1beta1.BasicAuthorization,
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"username": []byte("dasd"),
				},
			},
			wantErr: true,
		},
		{
			name:     "missing bearer token",
			authType: lokiv1beta1.BearerAuthorization,
			secret:   &corev1.Secret{},
			wantErr:  true,
		},
		{
			name:     "valid basic auth",
			authType: lokiv1beta1.BasicAuthorization,
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"username": []byte("hello"),
					"password": []byte("world"),
				},
			},
			wantSecret: &manifests.RulerSecret{
				Username: "hello",
				Password: "world",
			},
		},
		{
			name:     "valid header auth",
			authType: lokiv1beta1.BearerAuthorization,
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"bearer_token": []byte("hello world"),
				},
			},
			wantSecret: &manifests.RulerSecret{
				BearerToken: "hello world",
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			s, err := rules.ExtractRulerSecret(tst.secret, tst.authType)
			if !tst.wantErr {
				require.NoError(t, err)
				require.Equal(t, tst.wantSecret, s)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}
