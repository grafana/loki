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

var (
	errMissingIssuer    = errors.New("no issuer set")
	errMissingHostnames = errors.New("no hostnames set")
	errMissingUserInfo  = errors.New("no user info")
)

type clockFunc func() time.Time

type signerRotation struct {
	Issuer string
	Clock  clockFunc
}

func (r *signerRotation) NewCertificate(validity time.Duration) (*crypto.TLSCertificateConfig, error) {
	if r.Issuer == "" {
		return nil, errMissingIssuer
	}

	signerName := fmt.Sprintf("%s@%d", r.Issuer, time.Now().Unix())
	return crypto.MakeSelfSignedCAConfigForDuration(signerName, validity)
}

func (r *signerRotation) NeedNewCertificate(annotations map[string]string, refresh time.Duration) string {
	return needNewCertificate(annotations, r.Clock, refresh, nil)
}

func (r *signerRotation) SetAnnotations(ca *crypto.TLSCertificateConfig, annotations map[string]string) {
	annotations[CertificateNotAfterAnnotation] = ca.Certs[0].NotAfter.Format(time.RFC3339)
	annotations[CertificateNotBeforeAnnotation] = ca.Certs[0].NotBefore.Format(time.RFC3339)
	annotations[CertificateIssuer] = ca.Certs[0].Issuer.CommonName
}

type certificateRotation struct {
	UserInfo  user.Info
	Hostnames sets.Set[string]
	Clock     clockFunc
}

func (r *certificateRotation) NewCertificate(signer *crypto.CA, validity time.Duration) (*crypto.TLSCertificateConfig, error) {
	if r.UserInfo == nil {
		return nil, errMissingUserInfo
	}
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

	return signer.MakeServerCertForDuration(r.Hostnames, validity, addClientAuthUsage, addSubject)
}

func (r *certificateRotation) NeedNewCertificate(annotations map[string]string, signer *crypto.CA, caBundleCerts []*x509.Certificate, refresh time.Duration) string {
	reason := needNewCertificate(annotations, r.Clock, refresh, signer)
	if len(reason) > 0 {
		return reason
	}

	// check the signer common name against all the common names in our ca bundle so we don't refresh early
	signerCommonName := annotations[CertificateIssuer]
	if signerCommonName == "" {
		return "missing issuer name"
	}

	var found bool
	for _, caCert := range caBundleCerts {
		if signerCommonName == caCert.Subject.CommonName {
			found = true
			break
		}
	}

	if !found {
		return fmt.Sprintf("issuer %q, not in ca bundle:\n%s", signerCommonName, certs.CertificateBundleToString(caBundleCerts))
	}

	existingHostnames := sets.New[string](strings.Split(annotations[CertificateHostnames], ",")...)
	requiredHostnames := r.Hostnames.Clone()
	if !existingHostnames.Equal(requiredHostnames) {
		existingNotRequired := existingHostnames.Difference(requiredHostnames)
		requiredNotExisting := requiredHostnames.Difference(existingHostnames)
		return fmt.Sprintf("hostnames %q are existing and not required, %q are required and not existing",
			strings.Join(sets.List[string](existingNotRequired), ","),
			strings.Join(sets.List[string](requiredNotExisting), ","),
		)
	}

	return ""
}

func (r *certificateRotation) SetAnnotations(cert *crypto.TLSCertificateConfig, annotations map[string]string) {
	hostnames := sets.Set[string]{}
	for _, ip := range cert.Certs[0].IPAddresses {
		hostnames.Insert(ip.String())
	}
	for _, dnsName := range cert.Certs[0].DNSNames {
		hostnames.Insert(dnsName)
	}

	annotations[CertificateNotAfterAnnotation] = cert.Certs[0].NotAfter.Format(time.RFC3339)
	annotations[CertificateNotBeforeAnnotation] = cert.Certs[0].NotBefore.Format(time.RFC3339)
	annotations[CertificateIssuer] = cert.Certs[0].Issuer.CommonName
	// List does a sort so that we have a consistent representation
	annotations[CertificateHostnames] = strings.Join(sets.List[string](hostnames), ",")
}

func needNewCertificate(annotations map[string]string, clock clockFunc, refresh time.Duration, signer *crypto.CA) string {
	notAfterString := annotations[CertificateNotAfterAnnotation]
	if len(notAfterString) == 0 {
		return "missing notAfter"
	}
	notAfter, err := time.Parse(time.RFC3339, notAfterString)
	if err != nil {
		return fmt.Sprintf("bad expiry: %q", notAfterString)
	}

	notBeforeString := annotations[CertificateNotBeforeAnnotation]
	if len(notAfterString) == 0 {
		return "missing notBefore"
	}
	notBefore, err := time.Parse(time.RFC3339, notBeforeString)
	if err != nil {
		return fmt.Sprintf("bad expiry: %q", notBeforeString)
	}

	now := clock()

	// Is cert expired?
	if now.After(notAfter) {
		return "already expired"
	}

	// Refresh only when expired
	validity := notAfter.Sub(notBefore)
	if validity == refresh {
		return ""
	}

	// Are we at 80% of validity?
	at80Percent := notAfter.Add(-validity / 5)
	if now.After(at80Percent) {
		return fmt.Sprintf("past its latest possible time %v", at80Percent)
	}

	// If Certificate is past its refresh time, we may have action to take. We only do this if the signer is old enough.
	developerSpecifiedRefresh := notBefore.Add(refresh)
	if now.After(developerSpecifiedRefresh) {
		if signer == nil {
			return fmt.Sprintf("past its refresh time %v", developerSpecifiedRefresh)
		}

		// make sure the signer has been valid for more than 10% of the target's refresh time.
		timeToWaitForTrustRotation := refresh / 10
		if now.After(signer.Config.Certs[0].NotBefore.Add(timeToWaitForTrustRotation)) {
			return fmt.Sprintf("past its refresh time %v", developerSpecifiedRefresh)
		}
	}

	return ""
}
