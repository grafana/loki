package certrotation

import (
	"fmt"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/openshift/library-go/pkg/crypto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CertificatesExpired returns an error if any certificates expired and the list of expiry reasons.
func CertificatesExpired(opts Options) error {
	if opts.Signer.Secret == nil || opts.CABundle == nil {
		return nil
	}

	for _, cert := range opts.Certificates {
		if cert.Secret == nil {
			return nil
		}
	}

	rawCA, err := crypto.GetCAFromBytes(opts.Signer.Secret.Data[corev1.TLSCertKey], opts.Signer.Secret.Data[corev1.TLSPrivateKeyKey])
	if err != nil {
		return kverrors.Wrap(err, "failed to get signing CA from secret")
	}

	caBundle := opts.CABundle.Data[CAFile]
	caCerts, err := crypto.CertsFromPEM([]byte(caBundle))
	if err != nil {
		return kverrors.Wrap(err, "failed to get ca bundle certificates from configmap")
	}

	var reasons []string
	for name, cert := range opts.Certificates {
		reason := cert.Rotation.NeedNewCertificate(cert.Secret.Annotations, rawCA, caCerts, opts.Rotation.TargetCertRefresh)
		if reason != "" {
			reasons = append(reasons, fmt.Sprintf("%s: %s", name, reason))
		}
	}

	if len(reasons) == 0 {
		return nil
	}

	return &CertExpiredError{Message: "certificates expired", Reasons: reasons}
}

// buildTargetCertKeyPairSecrets returns a slice of all rotated client and serving lokistack certificates.
func buildTargetCertKeyPairSecrets(opts Options) ([]client.Object, error) {
	var (
		res      = make([]client.Object, 0)
		ns       = opts.StackNamespace
		rawCA    = opts.Signer.RawCA
		caBundle = opts.RawCACerts
		validity = opts.Rotation.TargetCertValidity
		refresh  = opts.Rotation.TargetCertRefresh
	)

	for name, cert := range opts.Certificates {
		secret := newTargetCertificateSecret(name, ns, cert.Secret)
		reason := cert.Rotation.NeedNewCertificate(secret.Annotations, rawCA, caBundle, refresh)
		if len(reason) > 0 {
			if err := setTargetCertKeyPairSecret(secret, validity, rawCA, cert.Rotation); err != nil {
				return nil, err
			}
		}

		res = append(res, secret)
	}

	return res, nil
}

func newTargetCertificateSecret(name, ns string, s *corev1.Secret) *corev1.Secret {
	current := s.DeepCopy()

	ss := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Type: corev1.SecretTypeTLS,
	}

	if current != nil {
		ss.Annotations = current.Annotations
		ss.Labels = current.Labels
		ss.Data = current.Data
	}

	return ss
}

// setTargetCertKeyPairSecret creates a new cert/key pair and sets them in the secret.  Only one of client, serving, or signer rotation may be specified.
func setTargetCertKeyPairSecret(s *corev1.Secret, validity time.Duration, signer *crypto.CA, certCreator certificateRotation) error {
	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	if s.Data == nil {
		s.Data = map[string][]byte{}
	}

	// our annotation is based on our cert validity, so we want to make sure that we don't specify something past our signer
	targetValidity := validity
	remainingSignerValidity := time.Until(signer.Config.Certs[0].NotAfter)
	if remainingSignerValidity < validity {
		targetValidity = remainingSignerValidity
	}

	certKeyPair, err := certCreator.NewCertificate(signer, targetValidity)
	if err != nil {
		return err
	}

	s.Data[corev1.TLSCertKey], s.Data[corev1.TLSPrivateKeyKey], err = certKeyPair.GetPEMBytes()
	if err != nil {
		return err
	}
	certCreator.SetAnnotations(certKeyPair, s.Annotations)

	return nil
}
