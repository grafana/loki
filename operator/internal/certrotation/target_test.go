package certrotation

import (
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/cert"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
)

func TestCertificatesExpired(t *testing.T) {
	var (
		stackName           = "dev"
		stackNamespce       = "ns"
		invalidNotAfter, _  = time.Parse(time.RFC3339, "")
		invalidNotBefore, _ = time.Parse(time.RFC3339, "")
		rawCA, caBytes      = newTestCABundle(t, "dev-ca")
		cfg                 = configv1.BuiltInCertManagement{
			CACertValidity: "10m",
			CACertRefresh:  "5m",
			CertValidity:   "2m",
			CertRefresh:    "1m",
		}
	)

	certBytes, keyBytes, err := rawCA.Config.GetPEMBytes()
	require.NoError(t, err)

	opts := Options{
		StackName:      stackName,
		StackNamespace: stackNamespce,
		Signer: SigningCA{
			RawCA: rawCA,
			Secret: &corev1.Secret{
				Data: map[string][]byte{
					corev1.TLSCertKey:       certBytes,
					corev1.TLSPrivateKeyKey: keyBytes,
				},
			},
		},
		CABundle: &corev1.ConfigMap{
			Data: map[string]string{
				CAFile: string(caBytes),
			},
		},
		RawCACerts: rawCA.Config.Certs,
	}
	err = ApplyDefaultSettings(&opts, cfg)
	require.NoError(t, err)

	for _, name := range ComponentCertSecretNames(stackName) {
		cert := opts.Certificates[name]
		cert.Secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: stackNamespce,
				Annotations: map[string]string{
					CertificateIssuer:              "dev_ns@signing-ca@10000",
					CertificateNotAfterAnnotation:  invalidNotAfter.Format(time.RFC3339),
					CertificateNotBeforeAnnotation: invalidNotBefore.Format(time.RFC3339),
				},
			},
		}
		opts.Certificates[name] = cert
	}

	var expired *CertExpiredError
	err = CertificatesExpired(opts)

	require.Error(t, err)
	require.ErrorAs(t, err, &expired)
	require.Len(t, err.(*CertExpiredError).Reasons, 15)
}

func TestBuildTargetCertKeyPairSecrets_Create(t *testing.T) {
	var (
		rawCA, _ = newTestCABundle(t, "test-ca")
		cfg      = configv1.BuiltInCertManagement{
			CACertValidity: "10m",
			CACertRefresh:  "5m",
			CertValidity:   "2m",
			CertRefresh:    "1m",
		}
	)

	opts := Options{
		StackName:      "dev",
		StackNamespace: "ns",
		Signer: SigningCA{
			RawCA: rawCA,
		},
		RawCACerts: rawCA.Config.Certs,
	}

	err := ApplyDefaultSettings(&opts, cfg)
	require.NoError(t, err)

	objs, err := buildTargetCertKeyPairSecrets(opts)
	require.NoError(t, err)
	require.Len(t, objs, 15)
}

func TestBuildTargetCertKeyPairSecrets_Rotate(t *testing.T) {
	var (
		rawCA, _            = newTestCABundle(t, "test-ca")
		invalidNotAfter, _  = time.Parse(time.RFC3339, "")
		invalidNotBefore, _ = time.Parse(time.RFC3339, "")
		cfg                 = configv1.BuiltInCertManagement{
			CACertValidity: "10m",
			CACertRefresh:  "5m",
			CertValidity:   "2m",
			CertRefresh:    "1m",
		}
	)

	opts := Options{
		StackName:      "dev",
		StackNamespace: "ns",
		Signer: SigningCA{
			RawCA: rawCA,
		},
		RawCACerts: rawCA.Config.Certs,
		Certificates: map[string]SelfSignedCertKey{
			"dev-ingester-grpc": {
				Secret: &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "dev-ingester-grpc",
						Namespace: "ns",
						Annotations: map[string]string{
							CertificateIssuer:              "dev_ns@signing-ca@10000",
							CertificateNotAfterAnnotation:  invalidNotAfter.Format(time.RFC3339),
							CertificateNotBeforeAnnotation: invalidNotBefore.Format(time.RFC3339),
						},
					},
				},
			},
		},
	}
	err := ApplyDefaultSettings(&opts, cfg)
	require.NoError(t, err)

	objs, err := buildTargetCertKeyPairSecrets(opts)
	require.NoError(t, err)
	require.Len(t, objs, 15)

	// Check serving certificate rotation
	s := objs[7].(*corev1.Secret)
	ss := opts.Certificates["dev-ingester-grpc"]

	require.NotEqual(t, s.Annotations[CertificateIssuer], ss.Secret.Annotations[CertificateIssuer])
	require.NotEqual(t, s.Annotations[CertificateNotAfterAnnotation], ss.Secret.Annotations[CertificateNotAfterAnnotation])
	require.NotEqual(t, s.Annotations[CertificateNotBeforeAnnotation], ss.Secret.Annotations[CertificateNotBeforeAnnotation])
	require.NotEqual(t, s.Annotations[CertificateHostnames], ss.Secret.Annotations[CertificateHostnames])
	require.NotEqual(t, string(s.Data[corev1.TLSCertKey]), string(ss.Secret.Data[corev1.TLSCertKey]))
	require.NotEqual(t, string(s.Data[corev1.TLSPrivateKeyKey]), string(ss.Secret.Data[corev1.TLSPrivateKeyKey]))

	certs, err := cert.ParseCertsPEM(s.Data[corev1.TLSCertKey])
	require.NoError(t, err)
	require.Contains(t, certs[0].ExtKeyUsage, x509.ExtKeyUsageClientAuth)
	require.Contains(t, certs[0].ExtKeyUsage, x509.ExtKeyUsageServerAuth)
}
