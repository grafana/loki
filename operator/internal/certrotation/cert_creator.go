package certrotation

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/certs"
	"github.com/openshift/library-go/pkg/crypto"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apiserver/pkg/authentication/user"
)

var errMissingHostnames = errors.New("no hostnames set")

// TargetCertCreator defines the interface of cert creating facilities.
type TargetCertCreator interface {
	// NewCertificate creates a new key-cert pair with the given signer.
	NewCertificate(signer *crypto.CA, validity time.Duration) (*crypto.TLSCertificateConfig, error)
	// NeedNewTargetCertKeyPair decides whether a new cert-key pair is needed. It returns a non-empty reason if it is the case.
	NeedNewTargetCertKeyPair(currentSecretAnnotations map[string]string, signer *crypto.CA, caBundleCerts []*x509.Certificate, refresh time.Duration, refreshOnlyWhenExpired bool) string
	// SetAnnotations gives an option to override or set additional annotations
	SetAnnotations(cert *crypto.TLSCertificateConfig, annotations map[string]string) map[string]string
}

type clientCertCreator struct {
	UserInfo user.Info
}

func (r *clientCertCreator) NewCertificate(signer *crypto.CA, validity time.Duration) (*crypto.TLSCertificateConfig, error) {
	return signer.MakeClientCertificateForDuration(r.UserInfo, validity)
}

func (r *clientCertCreator) NeedNewTargetCertKeyPair(annotations map[string]string, signer *crypto.CA, caBundleCerts []*x509.Certificate, refresh time.Duration, refreshOnlyWhenExpired bool) string {
	return needNewTargetCertKeyPair(annotations, signer, caBundleCerts, refresh, refreshOnlyWhenExpired)
}

func (r *clientCertCreator) SetAnnotations(cert *crypto.TLSCertificateConfig, annotations map[string]string) map[string]string {
	return annotations
}

type servingCertCreator struct {
	UserInfo  user.Info
	Hostnames []string
}

func (r *servingCertCreator) NewCertificate(signer *crypto.CA, validity time.Duration) (*crypto.TLSCertificateConfig, error) {
	if len(r.Hostnames) == 0 {
		return nil, errMissingHostnames
	}

	addClientAuthUsage := func(cert *x509.Certificate) error {
		cert.ExtKeyUsage = append(cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth)
		return nil
	}

	addSubject := func(cert *x509.Certificate) error {
		cert.Subject = pkix.Name{
			CommonName:   r.UserInfo.GetName(),
			SerialNumber: r.UserInfo.GetUID(),
			Organization: r.UserInfo.GetGroups(),
		}
		return nil
	}

	return signer.MakeServerCertForDuration(sets.NewString(r.Hostnames...), validity, addClientAuthUsage, addSubject)
}

func (r *servingCertCreator) NeedNewTargetCertKeyPair(annotations map[string]string, signer *crypto.CA, caBundleCerts []*x509.Certificate, refresh time.Duration, refreshOnlyWhenExpired bool) string {
	reason := needNewTargetCertKeyPair(annotations, signer, caBundleCerts, refresh, refreshOnlyWhenExpired)
	if len(reason) > 0 {
		return reason
	}

	return r.missingHostnames(annotations)
}

func (r *servingCertCreator) missingHostnames(annotations map[string]string) string {
	existingHostnames := sets.NewString(strings.Split(annotations[CertificateHostnames], ",")...)
	requiredHostnames := sets.NewString(r.Hostnames...)
	if !existingHostnames.Equal(requiredHostnames) {
		existingNotRequired := existingHostnames.Difference(requiredHostnames)
		requiredNotExisting := requiredHostnames.Difference(existingHostnames)
		return fmt.Sprintf("%q are existing and not required, %q are required and not existing", strings.Join(existingNotRequired.List(), ","), strings.Join(requiredNotExisting.List(), ","))
	}

	return ""
}

func (r *servingCertCreator) SetAnnotations(cert *crypto.TLSCertificateConfig, annotations map[string]string) map[string]string {
	hostnames := sets.String{}
	for _, ip := range cert.Certs[0].IPAddresses {
		hostnames.Insert(ip.String())
	}
	for _, dnsName := range cert.Certs[0].DNSNames {
		hostnames.Insert(dnsName)
	}

	// List does a sort so that we have a consistent representation
	annotations[CertificateHostnames] = strings.Join(hostnames.List(), ",")
	return annotations
}

func needNewTargetCertKeyPair(annotations map[string]string, signer *crypto.CA, caBundleCerts []*x509.Certificate, refresh time.Duration, refreshOnlyWhenExpired bool) string {
	if reason := needNewTargetCertKeyPairForTime(annotations, signer, refresh, refreshOnlyWhenExpired); len(reason) > 0 {
		return reason
	}

	// check the signer common name against all the common names in our ca bundle so we don't refresh early
	signerCommonName := annotations[CertificateIssuer]
	if len(signerCommonName) == 0 {
		return "missing issuer name"
	}
	for _, caCert := range caBundleCerts {
		if signerCommonName == caCert.Subject.CommonName {
			return ""
		}
	}

	return fmt.Sprintf("issuer %q, not in ca bundle:\n%s", signerCommonName, certs.CertificateBundleToString(caBundleCerts))
}

// needNewTargetCertKeyPairForTime returns true when
//  1. when notAfter or notBefore is missing in the annotation
//  2. when notAfter or notBefore is malformed
//  3. when now is after the notAfter
//  4. when now is after notAfter+refresh AND the signer has been valid
//     for more than 5% of the "extra" time we renew the target
//
// in other words, we rotate if
//
// our old CA is gone from the bundle (then we are pretty late to the renewal party)
// or the cert expired (then we are also pretty late)
// or we are over the renewal percentage of the validity, but only if the new CA at least 10% into its age.
// Maybe worth a go doc.
//
// So in general we need to see a signing CA at least aged 10% within 1-percentage of the cert validity.
//
// Hence, if the CAs are rotated too fast (like CA percentage around 10% or smaller), we will not hit the time to make use of the CA. Or if the cert renewal percentage is at 90%, there is not much time either.
//
// So with a cert percentage of 75% and equally long CA and cert validities at the worst case we start at 85% of the cert to renew, trying again every minute.
func needNewTargetCertKeyPairForTime(annotations map[string]string, signer *crypto.CA, refresh time.Duration, refreshOnlyWhenExpired bool) string {
	notBefore, notAfter, reason := getValidityFromAnnotations(annotations)
	if len(reason) > 0 {
		return reason
	}

	// Is cert expired?
	if time.Now().After(notAfter) {
		return "already expired"
	}

	if refreshOnlyWhenExpired {
		return ""
	}

	// Are we at 80% of validity?
	validity := notAfter.Sub(notBefore)
	at80Percent := notAfter.Add(-validity / 5)
	if time.Now().After(at80Percent) {
		return fmt.Sprintf("past its latest possible time %v", at80Percent)
	}

	// If Certificate is past its refresh time, we may have action to take. We only do this if the signer is old enough.
	refreshTime := notBefore.Add(refresh)
	if time.Now().After(refreshTime) {
		// make sure the signer has been valid for more than 10% of the target's refresh time.
		timeToWaitForTrustRotation := refresh / 10
		if time.Now().After(signer.Config.Certs[0].NotBefore.Add(timeToWaitForTrustRotation)) {
			return fmt.Sprintf("past its refresh time %v", refreshTime)
		}
	}

	return ""
}
