package certrotation

import (
	"bytes"
	"crypto/x509"
	"testing"
	"time"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/cert"
)

func TestBuildTargetCertKeyPairSecrets(t *testing.T) {
	// A default test CA
	testCA, err := crypto.MakeSelfSignedCAConfigForDuration("lokistack-dev-ca-bundle", 30*24*time.Hour)
	require.NoError(t, err)

	certBytes := &bytes.Buffer{}
	keyBytes := &bytes.Buffer{}
	err = testCA.WriteCertConfig(certBytes, keyBytes)
	require.NoError(t, err)

	rawCA, err := crypto.GetCAFromBytes(certBytes.Bytes(), keyBytes.Bytes())
	require.NoError(t, err)

	cfg := configv1.BuiltInCertManagement{
		CACertValidity: "10m",
		CACertRefresh:  "5m",
		CertValidity:   "2m",
		CertRefresh:    "1m",
	}

	createOpts := Options{
		StackName:      "dev",
		StackNamespace: "ns",
		Signer: SigningCA{
			RawCA: rawCA,
		},
		RawCACerts: rawCA.Config.Certs,
	}
	err = ApplyDefaultSettings(&createOpts, cfg)
	require.NoError(t, err)

	invalidNotAfter, _ := time.Parse(time.RFC3339, "")
	invalidNotBefore, _ := time.Parse(time.RFC3339, "")

	rotateOpts := Options{
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
	err = ApplyDefaultSettings(&rotateOpts, cfg)
	require.NoError(t, err)

	tt := []struct {
		desc string
		opts Options
	}{
		{
			desc: "create",
			opts: createOpts,
		},
		{
			desc: "rotate",
			opts: rotateOpts,
		},
	}

	for _, test := range tt {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			objs, err := buildTargetCertKeyPairSecrets(test.opts)
			require.NoError(t, err)
			require.Len(t, objs, 15)

			// Check serving certificate rotation
			s := objs[7].(*corev1.Secret)
			ss := test.opts.Certificates["dev-ingester-grpc"]
			if ss.Secret != nil {
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
		})
	}
}
