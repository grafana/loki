package certrotation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSigningCAExpired_EmptySecret(t *testing.T) {
	opts := Options{
		StackName:      "dev",
		StackNamespace: "ns",
	}

	err := SigningCAExpired(opts)
	require.NoError(t, err)
}

func TestSigningCAExpired_ExpiredSecret(t *testing.T) {
	var (
		stackName           = "dev"
		stackNamespace      = "ns"
		clock               = time.Now
		invalidNotAfter, _  = time.Parse(time.RFC3339, "")
		invalidNotBefore, _ = time.Parse(time.RFC3339, "")
	)

	opts := Options{
		StackName:      stackName,
		StackNamespace: stackNamespace,
		Signer: SigningCA{
			Rotation: signerRotation{
				Clock: clock,
			},
			Secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      SigningCASecretName(stackName),
					Namespace: stackNamespace,
					Annotations: map[string]string{
						CertificateIssuer:              "dev_ns@signing-ca@10000",
						CertificateNotAfterAnnotation:  invalidNotAfter.Format(time.RFC3339),
						CertificateNotBeforeAnnotation: invalidNotBefore.Format(time.RFC3339),
					},
				},
			},
		},
	}

	err := SigningCAExpired(opts)

	e := &CertExpiredError{}
	require.Error(t, err)
	require.ErrorAs(t, err, &e)
	require.Contains(t, err.(*CertExpiredError).Reasons, "already expired")
}

func TestBuildSigningCASecret_Create(t *testing.T) {
	opts := &Options{
		StackName:      "dev",
		StackNamespace: "ns",
	}

	obj, err := buildSigningCASecret(opts)
	require.NoError(t, err)
	require.NotNil(t, obj)
	require.NotNil(t, opts.Signer.RawCA)

	s := obj.(*corev1.Secret)
	// Require mandatory annotations for rotation
	require.Contains(t, s.Annotations, CertificateIssuer)
	require.Contains(t, s.Annotations, CertificateNotAfterAnnotation)
	require.Contains(t, s.Annotations, CertificateNotBeforeAnnotation)

	// Require cert-key-pair in data section
	require.NotEmpty(t, s.Data[corev1.TLSCertKey])
	require.NotEmpty(t, s.Data[corev1.TLSPrivateKeyKey])
}

func TestBuildSigningCASecret_Rotate(t *testing.T) {
	var (
		clock               = time.Now
		invalidNotAfter, _  = time.Parse(time.RFC3339, "")
		invalidNotBefore, _ = time.Parse(time.RFC3339, "")
	)

	opts := &Options{
		StackName:      "dev",
		StackNamespace: "ns",
		Signer: SigningCA{
			Rotation: signerRotation{
				Clock: clock,
			},
			Secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dev-signing-ca",
					Namespace: "ns",
					Annotations: map[string]string{
						CertificateIssuer:              "dev_ns@signing-ca@10000",
						CertificateNotAfterAnnotation:  invalidNotAfter.Format(time.RFC3339),
						CertificateNotBeforeAnnotation: invalidNotBefore.Format(time.RFC3339),
					},
				},
			},
		},
	}

	obj, err := buildSigningCASecret(opts)
	require.NoError(t, err)
	require.NotNil(t, obj)
	require.NotNil(t, opts.Signer.RawCA)

	s := obj.(*corev1.Secret)
	// Require mandatory annotations for rotation
	require.Contains(t, s.Annotations, CertificateIssuer)
	require.Contains(t, s.Annotations, CertificateNotAfterAnnotation)
	require.Contains(t, s.Annotations, CertificateNotBeforeAnnotation)

	// Require cert-key-pair in data section
	require.NotEmpty(t, s.Data[corev1.TLSCertKey])
	require.NotEmpty(t, s.Data[corev1.TLSPrivateKeyKey])

	// Require rotation
	require.NotEqual(t, s.Annotations[CertificateIssuer], opts.Signer.Secret.Annotations[CertificateIssuer])
	require.NotEqual(t, s.Annotations[CertificateNotAfterAnnotation], opts.Signer.Secret.Annotations[CertificateNotAfterAnnotation])
	require.NotEqual(t, s.Annotations[CertificateNotBeforeAnnotation], opts.Signer.Secret.Annotations[CertificateNotBeforeAnnotation])
	require.NotEqual(t, string(s.Data[corev1.TLSCertKey]), string(opts.Signer.Secret.Data[corev1.TLSCertKey]))
	require.NotEqual(t, string(s.Data[corev1.TLSPrivateKeyKey]), string(opts.Signer.Secret.Data[corev1.TLSPrivateKeyKey]))
}
