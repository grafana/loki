package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/status"
)

func TestValidateTLSConfig(t *testing.T) {
	const (
		stackName      = "my-stack"
		stackNamespace = "some-ns"
	)

	validCAConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-ca-configmap",
			Namespace: stackNamespace,
		},
		Data: map[string]string{
			"ca.crt": "test",
		},
	}

	validCertConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-cert-configmap",
			Namespace: stackNamespace,
		},
		Data: map[string]string{
			"tls.crt": "test",
		},
	}

	validCertSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-cert-secret",
			Namespace: stackNamespace,
		},
		Data: map[string][]byte{
			"tls.crt": []byte("test"),
		},
	}

	validKeySecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-key-secret",
			Namespace: stackNamespace,
		},
		Data: map[string][]byte{
			"tls.key": []byte("test"),
		},
	}

	invalidKeySecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-key-secret-invalid",
			Namespace: stackNamespace,
		},
		Data: map[string][]byte{},
	}

	invalidCertConfigMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "tls-cert-configmap-invalid",
			Namespace: stackNamespace,
		},
		Data: map[string]string{},
	}

	for _, tc := range []struct {
		name     string
		tlsSpec  *lokiv1.TLSSpec
		expError error
	}{
		{
			name:     "nil TLS spec returns no error",
			tlsSpec:  nil,
			expError: nil,
		},
		{
			name: "valid cert from ConfigMap and key from Secret",
			tlsSpec: &lokiv1.TLSSpec{
				CA: &lokiv1.ValueReference{
					Key:           "ca.crt",
					ConfigMapName: validCAConfigMap.Name,
				},
				Certificate: &lokiv1.ValueReference{
					Key:           "tls.crt",
					ConfigMapName: validCertConfigMap.Name,
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: validKeySecret.Name,
				},
			},
			expError: nil,
		},
		{
			name: "valid cert from Secret and key from Secret",
			tlsSpec: &lokiv1.TLSSpec{
				Certificate: &lokiv1.ValueReference{
					Key:        "tls.crt",
					SecretName: validCertSecret.Name,
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: validKeySecret.Name,
				},
			},
			expError: nil,
		},
		{
			name: "missing CA from ConfigMap",
			tlsSpec: &lokiv1.TLSSpec{
				CA: &lokiv1.ValueReference{
					Key:           "ca.crt",
					ConfigMapName: "non-existent-configmap",
				},
				Certificate: &lokiv1.ValueReference{
					Key:           "tls.crt",
					ConfigMapName: validCertConfigMap.Name,
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: validKeySecret.Name,
				},
			},
			expError: &status.DegradedError{
				Message: `Missing configmap for field "ca" in gateway TLS configuration: non-existent-configmap`,
				Reason:  lokiv1.ReasonMissingGatewayTLSConfig,
				Requeue: false,
			},
		},
		{
			name: "missing certificate from config",
			tlsSpec: &lokiv1.TLSSpec{
				CA: &lokiv1.ValueReference{
					Key:           "ca.crt",
					ConfigMapName: validCAConfigMap.Name,
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: validKeySecret.Name,
				},
			},
			expError: &status.DegradedError{
				Message: "Missing certificate or key in gateway TLS configuration. Please provide both certificate and key.",
				Reason:  lokiv1.ReasonInvalidGatewayTLSConfig,
				Requeue: false,
			},
		},
		{
			name: "missing cert ConfigMap",
			tlsSpec: &lokiv1.TLSSpec{
				Certificate: &lokiv1.ValueReference{
					Key:           "tls.crt",
					ConfigMapName: "non-existent-configmap",
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: validKeySecret.Name,
				},
			},
			expError: &status.DegradedError{
				Message: `Missing configmap for field "certificate" in gateway TLS configuration: non-existent-configmap`,
				Reason:  lokiv1.ReasonMissingGatewayTLSConfig,
				Requeue: false,
			},
		},
		{
			name: "missing cert Secret",
			tlsSpec: &lokiv1.TLSSpec{
				Certificate: &lokiv1.ValueReference{
					Key:        "tls.crt",
					SecretName: "non-existent-secret",
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: validKeySecret.Name,
				},
			},
			expError: &status.DegradedError{
				Message: `Missing secret for field "certificate" in gateway TLS configuration: non-existent-secret`,
				Reason:  lokiv1.ReasonMissingGatewayTLSConfig,
				Requeue: false,
			},
		},
		{
			name: "missing key Secret",
			tlsSpec: &lokiv1.TLSSpec{
				Certificate: &lokiv1.ValueReference{
					Key:           "tls.crt",
					ConfigMapName: validCertConfigMap.Name,
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: "non-existent-key-secret",
				},
			},
			expError: &status.DegradedError{
				Message: `Missing secret for field "privateKey" in gateway TLS configuration: non-existent-key-secret`,
				Reason:  lokiv1.ReasonMissingGatewayTLSConfig,
				Requeue: false,
			},
		},
		{
			name: "ConfigMap missing key",
			tlsSpec: &lokiv1.TLSSpec{
				Certificate: &lokiv1.ValueReference{
					Key:           "tls.crt",
					ConfigMapName: invalidCertConfigMap.Name,
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: validKeySecret.Name,
				},
			},
			expError: &status.DegradedError{
				Message: `Invalid configmap tls-cert-configmap-invalid for field "certificate" in gateway TLS configuration, missing key: tls.crt`,
				Reason:  lokiv1.ReasonInvalidGatewayTLSConfig,
				Requeue: false,
			},
		},
		{
			name: "Secret missing key data",
			tlsSpec: &lokiv1.TLSSpec{
				Certificate: &lokiv1.ValueReference{
					Key:           "tls.crt",
					ConfigMapName: validCertConfigMap.Name,
				},
				PrivateKey: &lokiv1.SecretReference{
					Key:        "tls.key",
					SecretName: invalidKeySecret.Name,
				},
			},
			expError: &status.DegradedError{
				Message: `Invalid secret tls-key-secret-invalid for field "privateKey" in gateway TLS configuration, missing key: tls.key`,
				Reason:  lokiv1.ReasonInvalidGatewayTLSConfig,
				Requeue: false,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := &k8sfakes.FakeClient{}

			stack := &lokiv1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stackName,
					Namespace: stackNamespace,
				},
				Spec: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
						Gateway: &lokiv1.GatewaySpec{
							TLS: tc.tlsSpec,
						},
					},
				},
			}

			k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
				switch obj := object.(type) {
				case *corev1.ConfigMap:
					switch name.Name {
					case validCAConfigMap.Name:
						k.SetClientObject(obj, &validCAConfigMap)
						return nil
					case validCertConfigMap.Name:
						k.SetClientObject(obj, &validCertConfigMap)
						return nil
					case invalidCertConfigMap.Name:
						k.SetClientObject(obj, &invalidCertConfigMap)
						return nil
					}
				case *corev1.Secret:
					switch name.Name {
					case validCertSecret.Name:
						k.SetClientObject(obj, &validCertSecret)
						return nil
					case validKeySecret.Name:
						k.SetClientObject(obj, &validKeySecret)
						return nil
					case invalidKeySecret.Name:
						k.SetClientObject(obj, &invalidKeySecret)
						return nil
					}
				}
				return apierrors.NewNotFound(schema.GroupResource{}, name.Name)
			}

			err := validateTLSConfig(context.Background(), k, stack)

			if tc.expError != nil {
				require.Equal(t, tc.expError, err)
				return
			}

			require.NoError(t, err)
		})
	}
}
