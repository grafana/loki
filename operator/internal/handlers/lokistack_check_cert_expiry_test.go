package handlers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/certrotation"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/handlers"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	rawTLSCert = `-----BEGIN CERTIFICATE-----
MIIDZTCCAk2gAwIBAgIITh/mvxH5jGMwDQYJKoZIhvcNAQELBQAwQDE+MDwGA1UE
Aww1b3BlbnNoaWZ0LWxvZ2dpbmdfbG9raXN0YWNrLWRldi1zaWduaW5nLWNhQDE2
NjI3MjQ2ODcwHhcNMjIwOTA5MTE1ODA2WhcNMjIxMDA5MTE1ODA3WjBAMT4wPAYD
VQQDDDVvcGVuc2hpZnQtbG9nZ2luZ19sb2tpc3RhY2stZGV2LXNpZ25pbmctY2FA
MTY2MjcyNDY4NzCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAJ6CSADy
QniPlguDCFlYcjMSrpPLcQrneCK0hgrZ9UjhoZFYBpjtZouGxLwEW+nfmB0TWtID
G4LYckPWx/LU3wYCSmCt3mVSIg9IAKw+vD/tlzP01hJghL1gS2QdYc8ajhGZQGPL
b1xvk/5LqTdSnEItgvMxvCDZDrqhTajQbK/vER2bJMbHi9H0MdRUYLqePv0Rob/Y
RhnyZ0pTd+VaZiXtXlQa0E2zRZH/zMVgxbVks9O3kY5hJKsphkey4CtVV3+M7sPm
kVuieS2KAllY5/LUD5rUnlSmOOkAspT3sP6e0NtK3amPX8D0Z5J1h4bhQCqBVpIK
H9u1SZ05tqnAxtECAwEAAaNjMGEwDgYDVR0PAQH/BAQDAgKkMA8GA1UdEwEB/wQF
MAMBAf8wHQYDVR0OBBYEFDDIuwTP4mD+1mrGYKSAAwFCNDgCMB8GA1UdIwQYMBaA
FDDIuwTP4mD+1mrGYKSAAwFCNDgCMA0GCSqGSIb3DQEBCwUAA4IBAQAKzoMO4bQW
+/An4uWJBSoMoxbS5dys25DWvdmW3kAiTMyCWrLfUyZqVlCjPDzDfrGLH95q3Am0
ieDouZps0Ot5IgtPgHTPP1Jp6us1ez7nGrNFYrS5/YQJ1oqWopmiG0QnybMOp0Th
SAO4g8hGf+eVn6oVzmSoDI1pxI7VdyXI9ZdPSCZvY4kagX7MbQ9k4K6HT787NrBp
WP9wnvMqHPpNJvVAORcnocgZGpsc8/rBnfOawFharyd7pIUgt1w0gjSM0HzrnbNt
mxXT/XVeksV98h0uSi0eXUM/gKkVUnivNLS/esa7D4O8XAJke88ZkE0OGpn4K/TH
WLUPhVWtGCfL
-----END CERTIFICATE-----`

	rawTLSKey = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAnoJIAPJCeI+WC4MIWVhyMxKuk8txCud4IrSGCtn1SOGhkVgG
mO1mi4bEvARb6d+YHRNa0gMbgthyQ9bH8tTfBgJKYK3eZVIiD0gArD68P+2XM/TW
EmCEvWBLZB1hzxqOEZlAY8tvXG+T/kupN1KcQi2C8zG8INkOuqFNqNBsr+8RHZsk
xseL0fQx1FRgup4+/RGhv9hGGfJnSlN35VpmJe1eVBrQTbNFkf/MxWDFtWSz07eR
jmEkqymGR7LgK1VXf4zuw+aRW6J5LYoCWVjn8tQPmtSeVKY46QCylPew/p7Q20rd
qY9fwPRnknWHhuFAKoFWkgof27VJnTm2qcDG0QIDAQABAoIBAHUsHYoFi7mPev1b
iYpyZUj34HGEjDXhUd9lz0iqQjX0BPlqNsZJh+pQX5IVLtS94rZrnlFs1qNs2Vro
pLoPPiY0/0JkhOglRORC96xcW9BuE73mmXDQRI+xZUnGpozwNmEwBnc+5T1RhfcP
ezFYMgaBmjGobEdj7Q1tO/k0yYNrbhao70lYAnP0+aQyHP8osuB8i86bnKoyl5Lt
PozPkjlQKIzlO1K0C0DcZ1sK8u6vLPhCgPNsaE1I25QTSQfsjgpaSLuu5mxzot7b
HvoDaec966gZlWnMt8+WvAA0HG28Qhmd6c7Rz+nUYS8+flPDXyD2LAzlqb7ke22i
DwhC55kCgYEA0RR/j0qHIF1xz4yY2C4O4KeMCQcU0hhs/60ex/24qPH63HGtGT2X
r1HjfzLu3WXlgwz25FSorCaHizLsxzAhS6YI3rn69PrurTD1QKMQLx0kUOJRI7QX
mHdbjSpaTmeNaRGXeiCcM9M9yJOHUufSnd41FkOYfWIxpEyk5BwyhAsCgYEAwhSB
dtiMvw1Hj9sXChnngmMt60QUga67Th1rCyRg8ury6OETs8l7Zp/HNF6CiCOwobQU
uKZ+oYkz59dzdPZ7PFU/8XF2KbPloXeTs2LCaGUHDpN0OIDnZLLhobUO+L9qLnGt
B5Xd1y6SDH9pvh2gcUIytmHcE65gYz13N3g1LhMCgYEAzWORm7Xe4FBriTPYwiUc
wFxXGFc4gNs12ES8xEHesThk80FIhk8XP0b2cPIb7Ko4uHB36P2xZMvEw12XdGU3
kBTfCc0xVo9bAA/kHUcSkvXRwxNQGf7EXyaBbT95zyOyqtB5OaPnTpHpU6x5d1v8
btDm3aQxnJplob0ZDm0UwtkCgYArZuqM6WCQWSfnw9cjKyfawNNECbWMSscYcPu/
QiNsL56i9bKyQhyWlqS10WzfhRu7DcqUgKdQ+J3i+wuW3Igytd3W4MjMCq8PrO4a
77sKHY22dMNI34rfuiE7SIJQnn3gZQuM5rb1qDSBFv1OxtFagrNUlg3hWN21U8mV
XgyGgQKBgBxrZdgsv3m5ga5Pc0siNYJIg31j2wethnxVG3JyVh9hwUWw0iHa34+m
sZt8goBfyDqcY7Pj7SnpuhEOc0tDNRI70/L4xBcMyaxtY9B3AaULZmhzKvhNejzJ
W3TUS27Ov6u2ZX7Mx5yfk81WJVuiNzf1zlKizOkUbzrGUJ72Ev5U
-----END RSA PRIVATE KEY-----`
)

func TestCheckCertExpiry_WhenGetReturnsNotFound_DoesNotError(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CheckCertExpiry(context.TODO(), logger, r, k, featureGates)
	require.NoError(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCheckCertExpiry_WhenGetReturnsAnErrorOtherThanNotFound_ReturnsTheError(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	badRequestErr := apierrors.NewBadRequest("you do not belong here")
	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return badRequestErr
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CheckCertExpiry(context.TODO(), logger, r, k, featureGates)

	require.Equal(t, badRequestErr, errors.Unwrap(err))

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCheckCertExpiry_WhenGetOptionsReturnsSignerNotFound_DoesNotError(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == r.Name && name.Namespace == r.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CheckCertExpiry(context.TODO(), logger, r, k, featureGates)
	require.NoError(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCheckCertExpiry_WhenGetOptionsReturnsCABUndleNotFound_DoesNotError(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	signer := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-signing-ca",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.grafana.com/v1",
					Kind:               "LokiStack",
					Name:               "my-stack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         pointer.Bool(true),
					BlockOwnerDeletion: pointer.Bool(true),
				},
			},
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == r.Name && name.Namespace == r.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		if name.Name == signer.Name && name.Namespace == signer.Namespace {
			k.SetClientObject(object, &signer)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CheckCertExpiry(context.TODO(), logger, r, k, featureGates)
	require.NoError(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCheckCertExpiry_WhenGetCAFromBytesError_ReturnError(t *testing.T) {
	validNotAfter := time.Now().Add(600 * 24 * time.Hour).UTC().Format(time.RFC3339)
	validNotBefore := time.Now().Add(600 * 24 * time.Hour).UTC().Format(time.RFC3339)

	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	signer := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-signing-ca",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			Annotations: map[string]string{
				certrotation.CertificateIssuer:              "dev_ns@signing-ca@10000",
				certrotation.CertificateNotAfterAnnotation:  validNotAfter,
				certrotation.CertificateNotBeforeAnnotation: validNotBefore,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.grafana.com/v1",
					Kind:               "LokiStack",
					Name:               "my-stack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
	}

	caBundle := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-ca-bundle",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.grafana.com/v1",
					Kind:               "LokiStack",
					Name:               "my-stack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == r.Name && name.Namespace == r.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		if name.Name == signer.Name && name.Namespace == signer.Namespace {
			k.SetClientObject(object, &signer)
			return nil
		}
		if name.Name == caBundle.Name && name.Namespace == caBundle.Namespace {
			k.SetClientObject(object, &caBundle)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CheckCertExpiry(context.TODO(), logger, r, k, featureGates)
	require.Error(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCheckCertExpiry_WhenGetCACertsFromPEMError_ReturnError(t *testing.T) {
	validNotAfter := time.Now().Add(600 * 24 * time.Hour).UTC().Format(time.RFC3339)
	validNotBefore := time.Now().Add(600 * 24 * time.Hour).UTC().Format(time.RFC3339)

	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	signer := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-signing-ca",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			Annotations: map[string]string{
				certrotation.CertificateIssuer:              "dev_ns@signing-ca@10000",
				certrotation.CertificateNotAfterAnnotation:  validNotAfter,
				certrotation.CertificateNotBeforeAnnotation: validNotBefore,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.grafana.com/v1",
					Kind:               "LokiStack",
					Name:               "my-stack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte(rawTLSCert),
			corev1.TLSPrivateKeyKey: []byte(rawTLSKey),
		},
	}

	caBundle := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-ca-bundle",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.grafana.com/v1",
					Kind:               "LokiStack",
					Name:               "my-stack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == r.Name && name.Namespace == r.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		if name.Name == signer.Name && name.Namespace == signer.Namespace {
			k.SetClientObject(object, &signer)
			return nil
		}
		if name.Name == caBundle.Name && name.Namespace == caBundle.Namespace {
			k.SetClientObject(object, &caBundle)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CheckCertExpiry(context.TODO(), logger, r, k, featureGates)
	require.Error(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}
