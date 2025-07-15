package gateway

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests"
)

func TestGetTenantSecrets(t *testing.T) {
	for _, mode := range []lokiv1.ModeType{lokiv1.Static, lokiv1.Dynamic} {
		for _, tc := range []struct {
			name      string
			authNSpec []lokiv1.AuthenticationSpec
			object    client.Object
			expected  []*manifests.TenantSecrets
		}{
			{
				name: "oidc",
				authNSpec: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "test",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: "test",
							},
						},
					},
				},
				object: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "some-ns",
					},
					Data: map[string][]byte{
						"clientID":     []byte("test"),
						"clientSecret": []byte("test"),
					},
				},
				expected: []*manifests.TenantSecrets{
					{
						TenantName: "test",
						OIDCSecret: &manifests.OIDCSecret{
							ClientID:     "test",
							ClientSecret: "test",
						},
					},
				},
			},
			{
				name: "mTLS",
				authNSpec: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "test",
						MTLS: &lokiv1.MTLSSpec{
							CA: &lokiv1.CASpec{
								CA:    "test",
								CAKey: "special-ca.crt",
							},
						},
					},
				},
				object: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "some-ns",
					},
					Data: map[string]string{
						"special-ca.crt": "my-specila-ca",
					},
				},
				expected: []*manifests.TenantSecrets{
					{
						TenantName: "test",
						MTLSSecret: &manifests.MTLSSecret{
							CAPath: "/var/run/tenants-ca/test/special-ca.crt",
						},
					},
				},
			},
		} {
			t.Run(strings.Join([]string{string(mode), tc.name}, "_"), func(t *testing.T) {
				k := &k8sfakes.FakeClient{}
				s := &lokiv1.LokiStack{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mystack",
						Namespace: "some-ns",
					},
					Spec: lokiv1.LokiStackSpec{
						Tenants: &lokiv1.TenantsSpec{
							Mode:           mode,
							Authentication: tc.authNSpec,
						},
					},
				}

				k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
					if name.Name == "test" && name.Namespace == "some-ns" {
						k.SetClientObject(object, tc.object)
					}
					return nil
				}
				ts, err := getTenantSecrets(context.TODO(), k, s)
				require.NoError(t, err)
				require.ElementsMatch(t, ts, tc.expected)
			})
		}
	}
}

func TestExtractOIDCSecret(t *testing.T) {
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
			name:       "all set",
			tenantName: "tenant-a",
			secret: &corev1.Secret{
				Data: map[string][]byte{
					"clientID":     []byte("test"),
					"clientSecret": []byte("test"),
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			_, err := extractOIDCSecret(tst.secret)
			if !tst.wantErr {
				require.NoError(t, err)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}

func TestCheckKeyIsPresent(t *testing.T) {
	type test struct {
		name       string
		tenantName string
		configMap  *corev1.ConfigMap
		wantErr    bool
	}
	table := []test{
		{
			name:       "missing key",
			tenantName: "tenant-a",
			configMap:  &corev1.ConfigMap{},
			wantErr:    true,
		},
		{
			name:       "all set",
			tenantName: "tenant-a",
			configMap: &corev1.ConfigMap{
				Data: map[string]string{
					"test": "test",
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			err := checkKeyIsPresent(tst.configMap, "test")
			if !tst.wantErr {
				require.NoError(t, err)
			}
			if tst.wantErr {
				require.NotNil(t, err)
			}
		})
	}
}
