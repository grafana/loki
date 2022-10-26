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

type CACreator struct {
	Issuer string
}

func (r *CACreator) NewCertificate(validity time.Duration) (*crypto.TLSCertificateConfig, error) {
	if r.Issuer == "" {
		return nil, errMissingIssuer
	}

	signerName := fmt.Sprintf("%s@%d", r.Issuer, time.Now().Unix())
	return crypto.MakeSelfSignedCAConfigForDuration(signerName, validity)
}

func (r *CACreator) NeedNewCertificate(annotations map[string]string, refresh time.Duration) string {
	notBefore, notAfter, reason := getValidityFromAnnotations(annotations)
	if len(reason) > 0 {
		return reason
	}

	reason = needNewCertificate(annotations, notBefore, notAfter, refresh, nil)
	if len(reason) > 0 {
		return reason
	}

	developerSpecifiedRefresh := notBefore.Add(refresh)
	if time.Now().After(developerSpecifiedRefresh) {
		return fmt.Sprintf("past its refresh time %v", developerSpecifiedRefresh)
	}

	return ""
}

func (r *CACreator) SetAnnotations(ca *crypto.TLSCertificateConfig, annotations map[string]string) {
	annotations[CertificateNotAfterAnnotation] = ca.Certs[0].NotAfter.Format(time.RFC3339)
	annotations[CertificateNotBeforeAnnotation] = ca.Certs[0].NotBefore.Format(time.RFC3339)
	annotations[CertificateIssuer] = ca.Certs[0].Issuer.CommonName
}

type CertCreator struct {
	UserInfo  user.Info
	Hostnames []string
}

func (r *CertCreator) NewCertificate(signer *crypto.CA, validity time.Duration) (*crypto.TLSCertificateConfig, error) {
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

	return signer.MakeServerCertForDuration(sets.NewString(r.Hostnames...), validity, addClientAuthUsage, addSubject)
}

func (r *CertCreator) NeedNewCertificate(annotations map[string]string, signer *crypto.CA, caBundleCerts []*x509.Certificate, refresh time.Duration) string {
	notBefore, notAfter, reason := getValidityFromAnnotations(annotations)
	if len(reason) > 0 {
		return reason
	}

	reason = needNewCertificate(annotations, notBefore, notAfter, refresh, signer)
	if len(reason) > 0 {
		return reason
	}

	// check the signer common name against all the common names in our ca bundle so we don't refresh early
	signerCommonName := annotations[CertificateIssuer]
	if len(signerCommonName) == 0 {
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

	existingHostnames := sets.NewString(strings.Split(annotations[CertificateHostnames], ",")...)
	requiredHostnames := sets.NewString(r.Hostnames...)
	if !existingHostnames.Equal(requiredHostnames) {
		existingNotRequired := existingHostnames.Difference(requiredHostnames)
		requiredNotExisting := requiredHostnames.Difference(existingHostnames)
		return fmt.Sprintf("%q are existing and not required, %q are required and not existing", strings.Join(existingNotRequired.List(), ","), strings.Join(requiredNotExisting.List(), ","))
	}

	return ""
}

func (r *CertCreator) SetAnnotations(cert *crypto.TLSCertificateConfig, annotations map[string]string) {
	hostnames := sets.String{}
	for _, ip := range cert.Certs[0].IPAddresses {
		hostnames.Insert(ip.String())
	}
	for _, dnsName := range cert.Certs[0].DNSNames {
		hostnames.Insert(dnsName)
	}

	// List does a sort so that we have a consistent representation
	annotations[CertificateNotAfterAnnotation] = cert.Certs[0].NotAfter.Format(time.RFC3339)
	annotations[CertificateNotBeforeAnnotation] = cert.Certs[0].NotBefore.Format(time.RFC3339)
	annotations[CertificateIssuer] = cert.Certs[0].Issuer.CommonName
	annotations[CertificateHostnames] = strings.Join(hostnames.List(), ",")
}

func needNewCertificate(annotations map[string]string, notBefore, notAfter time.Time, refresh time.Duration, signer *crypto.CA) string {
	// Is cert expired?
	if time.Now().After(notAfter) {
		return "already expired"
	}

	// Refresh only when expired
	validity := notAfter.Sub(notBefore)
	if validity == refresh {
		return ""
	}

	// Are we at 80% of validity?
	at80Percent := notAfter.Add(-validity / 5)
	if time.Now().After(at80Percent) {
		return fmt.Sprintf("past its latest possible time %v", at80Percent)
	}

	// If Certificate is past its refresh time, we may have action to take. We only do this if the signer is old enough.
	developerSpecifiedRefresh := notBefore.Add(refresh)
	if time.Now().After(developerSpecifiedRefresh) {
		if signer == nil {
			return fmt.Sprintf("past its refresh time %v", developerSpecifiedRefresh)
		}

		// make sure the signer has been valid for more than 10% of the target's refresh time.
		timeToWaitForTrustRotation := refresh / 10
		if time.Now().After(signer.Config.Certs[0].NotBefore.Add(timeToWaitForTrustRotation)) {
			return fmt.Sprintf("past its refresh time %v", developerSpecifiedRefresh)
		}
	}

	return ""
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
