package certrotation

import (
	"bytes"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/crypto"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SigningCAExpired returns true if the singer certificate expired and the reason of expiry.
func SigningCAExpired(opts Options) error {
	// Skip as secret not created or loaded
	if opts.Signer.Secret == nil {
		return nil
	}

	expired, reason := needNewSigningCertKeyPair(opts.Signer.Secret.Annotations, opts.CACertRefresh)
	if !expired {
		return nil
	}

	return &CertExpiredError{Message: "signing CA certificate expired", Reasons: []string{reason}}
}

// buildSigningCASecret returns a k8s Secret holding the signing CA certificate
func buildSigningCASecret(opts *Options) (client.Object, error) {
	signingCertKeyPairSecret := newSigningCASecret(*opts)

	if needed, _ := needNewSigningCertKeyPair(signingCertKeyPairSecret.Annotations, opts.CACertRefresh); needed {
		if err := setSigningCertKeyPairSecret(signingCertKeyPairSecret, opts.CACertValidity); err != nil {
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

func needNewSigningCertKeyPair(annotations map[string]string, refresh time.Duration) (bool, string) {
	notBefore, notAfter, reason := getValidityFromAnnotations(annotations)
	if len(reason) > 0 {
		return true, reason
	}

	if time.Now().After(notAfter) {
		return true, "already expired"
	}

	// Refresh only when expired
	validity := notAfter.Sub(notBefore)
	if validity == refresh {
		return false, ""
	}

	at80Percent := notAfter.Add(-validity / 5)
	if time.Now().After(at80Percent) {
		return true, fmt.Sprintf("past its latest possible time %v", at80Percent)
	}

	developerSpecifiedRefresh := notBefore.Add(refresh)
	if time.Now().After(developerSpecifiedRefresh) {
		return true, fmt.Sprintf("past its refresh time %v", developerSpecifiedRefresh)
	}

	return false, ""
}

func getValidityFromAnnotations(annotations map[string]string) (notBefore time.Time, notAfter time.Time, reason string) {
	notAfterString := annotations[CertificateNotAfterAnnotation]
	if len(notAfterString) == 0 {
		return notBefore, notAfter, "missing notAfter"
	}
	notAfter, err := time.Parse(time.RFC3339, notAfterString)
	if err != nil {
		return notBefore, notAfter, fmt.Sprintf("bad expiry: %q", notAfterString)
	}
	notBeforeString := annotations[CertificateNotBeforeAnnotation]
	if len(notAfterString) == 0 {
		return notBefore, notAfter, "missing notBefore"
	}
	notBefore, err = time.Parse(time.RFC3339, notBeforeString)
	if err != nil {
		return notBefore, notAfter, fmt.Sprintf("bad expiry: %q", notBeforeString)
	}

	return notBefore, notAfter, ""
}

// setSigningCertKeyPairSecret creates a new signing cert/key pair and sets them in the secret
func setSigningCertKeyPairSecret(signingCertKeyPairSecret *corev1.Secret, validity time.Duration) error {
	signerName := fmt.Sprintf("%s_%s@%d", signingCertKeyPairSecret.Namespace, signingCertKeyPairSecret.Name, time.Now().Unix())
	ca, err := crypto.MakeSelfSignedCAConfigForDuration(signerName, validity)
	if err != nil {
		return err
	}

	certBytes := &bytes.Buffer{}
	keyBytes := &bytes.Buffer{}
	if err := ca.WriteCertConfig(certBytes, keyBytes); err != nil {
		return err
	}

	if signingCertKeyPairSecret.Annotations == nil {
		signingCertKeyPairSecret.Annotations = map[string]string{}
	}
	if signingCertKeyPairSecret.Data == nil {
		signingCertKeyPairSecret.Data = map[string][]byte{}
	}
	signingCertKeyPairSecret.Data[corev1.TLSCertKey] = certBytes.Bytes()
	signingCertKeyPairSecret.Data[corev1.TLSPrivateKeyKey] = keyBytes.Bytes()
	signingCertKeyPairSecret.Annotations[CertificateNotAfterAnnotation] = ca.Certs[0].NotAfter.Format(time.RFC3339)
	signingCertKeyPairSecret.Annotations[CertificateNotBeforeAnnotation] = ca.Certs[0].NotBefore.Format(time.RFC3339)
	signingCertKeyPairSecret.Annotations[CertificateIssuer] = ca.Certs[0].Issuer.CommonName

	return nil
}
