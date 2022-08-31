package certrotation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBuildSigningCASecret(t *testing.T) {
	invalidNotAfter, _ := time.Parse(time.RFC3339, "")
	invalidNotBefore, _ := time.Parse(time.RFC3339, "")

	tt := []struct {
		desc string
		opts *Options
	}{
		{
			desc: "create",
			opts: &Options{
				StackName:      "dev",
				StackNamespace: "ns",
			},
		},
		{
			desc: "rotate",
			opts: &Options{
				StackName:      "dev",
				StackNamespace: "ns",
				Signer: SigningCA{
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
			},
		},
	}
	for _, test := range tt {
		test := test
		t.Run(test.desc, func(t *testing.T) {
			t.Parallel()

			obj, err := buildSigningCASecret(test.opts)
			require.NoError(t, err)
			require.NotNil(t, obj)
			require.NotNil(t, test.opts.Signer.RawCA)

			s := obj.(*corev1.Secret)
			// Require mandatory annotations for rotation
			require.Contains(t, s.Annotations, CertificateIssuer)
			require.Contains(t, s.Annotations, CertificateNotAfterAnnotation)
			require.Contains(t, s.Annotations, CertificateNotBeforeAnnotation)

			// Require cert-key-pair in data section
			require.NotEmpty(t, s.Data[corev1.TLSCertKey])
			require.NotEmpty(t, s.Data[corev1.TLSPrivateKeyKey])

			// Require rotation
			ss := test.opts.Signer.Secret
			if ss != nil {
				require.NotEqual(t, s.Annotations[CertificateIssuer], ss.Annotations[CertificateIssuer])
				require.NotEqual(t, s.Annotations[CertificateNotAfterAnnotation], ss.Annotations[CertificateNotAfterAnnotation])
				require.NotEqual(t, s.Annotations[CertificateNotBeforeAnnotation], ss.Annotations[CertificateNotBeforeAnnotation])
				require.NotEqual(t, string(s.Data[corev1.TLSCertKey]), string(ss.Data[corev1.TLSCertKey]))
				require.NotEqual(t, string(s.Data[corev1.TLSPrivateKeyKey]), string(ss.Data[corev1.TLSPrivateKeyKey]))
			}
		})
	}
}
