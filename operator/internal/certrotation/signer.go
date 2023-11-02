package certrotation

import (
	"bytes"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SigningCAExpired returns true if the signer certificate expired and the reason of expiry.
func SigningCAExpired(opts Options) error {
	// Skip as secret not created or loaded
	if opts.Signer.Secret == nil {
		return nil
	}

	reason := opts.Signer.Rotation.NeedNewCertificate(opts.Signer.Secret.Annotations, opts.Rotation.CACertRefresh)
	if reason != "" {
		return &CertExpiredError{Message: "signing CA certificate expired", Reasons: []string{reason}}
	}

	return nil
}

// buildSigningCASecret returns a k8s Secret holding the signing CA certificate
func buildSigningCASecret(opts *Options) (client.Object, error) {
	signingCertKeyPairSecret := newSigningCASecret(*opts)
	opts.Signer.Rotation.Issuer = fmt.Sprintf("%s_%s", signingCertKeyPairSecret.Namespace, signingCertKeyPairSecret.Name)

	if reason := opts.Signer.Rotation.NeedNewCertificate(signingCertKeyPairSecret.Annotations, opts.Rotation.CACertRefresh); reason != "" {
		if err := setSigningCertKeyPairSecret(signingCertKeyPairSecret, opts.Rotation.CACertValidity, opts.Signer.Rotation); err != nil {
			return nil, err
		}
	}

	var (
		cert = signingCertKeyPairSecret.Data[corev1.TLSCertKey]
		key  = signingCertKeyPairSecret.Data[corev1.TLSPrivateKeyKey]
	)

	rawCA, err := crypto.GetCAFromBytes(cert, key)
	if err != nil {
		return nil, err
	}

	opts.Signer.RawCA = rawCA

	return signingCertKeyPairSecret, nil
}

func newSigningCASecret(opts Options) *corev1.Secret {
	current := opts.Signer.Secret.DeepCopy()

	s := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SigningCASecretName(opts.StackName),
			Namespace: opts.StackNamespace,
		},
		Type: corev1.SecretTypeTLS,
	}

	if current != nil {
		s.Annotations = current.Annotations
		s.Labels = current.Labels
		s.Data = current.Data
	}

	return s
}

// setSigningCertKeyPairSecret creates a new signing cert/key pair and sets them in the secret
func setSigningCertKeyPairSecret(s *corev1.Secret, validity time.Duration, caCreator signerRotation) error {
	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	if s.Data == nil {
		s.Data = map[string][]byte{}
	}

	ca, err := caCreator.NewCertificate(validity)
	if err != nil {
		return err
	}

	certBytes := &bytes.Buffer{}
	keyBytes := &bytes.Buffer{}
	if err := ca.WriteCertConfig(certBytes, keyBytes); err != nil {
		return err
	}
	s.Data[corev1.TLSCertKey] = certBytes.Bytes()
	s.Data[corev1.TLSPrivateKeyKey] = keyBytes.Bytes()
	caCreator.SetAnnotations(ca, s.Annotations)

	return nil
}
