package certrotation

import (
	"fmt"
)

const (
	// CertificateNotBeforeAnnotation contains the certificate expiration date in RFC3339 format.
	CertificateNotBeforeAnnotation = "loki.grafana.com/certificate-not-before"
	// CertificateNotAfterAnnotation contains the certificate expiration date in RFC3339 format.
	CertificateNotAfterAnnotation = "loki.grafana.com/certificate-not-after"
	// CertificateIssuer contains the common name of the certificate that signed another certificate.
	CertificateIssuer = "loki.grafana.com/certificate-issuer"
	// CertificateHostnames contains the hostnames used by a signer.
	CertificateHostnames = "loki.grafana.com/certificate-hostnames"
)

const (
	// CAFile is the file name of the certificate authority file
	CAFile = "service-ca.crt"
)

// SigningCASecretName returns the lokistack signing CA secret name
func SigningCASecretName(stackName string) string {
	return fmt.Sprintf("%s-signing-ca", stackName)
}

// CABundleName returns the lokistack ca bundle configmap name
func CABundleName(stackName string) string {
	return fmt.Sprintf("%s-ca-bundle", stackName)
}

// ComponentCertSecretNames retruns a list of all loki component certificate secret names.
func ComponentCertSecretNames(stackName string) []string {
	return []string{
		fmt.Sprintf("%s-gateway-client-http", stackName),
		fmt.Sprintf("%s-compactor-http", stackName),
		fmt.Sprintf("%s-compactor-grpc", stackName),
		fmt.Sprintf("%s-distributor-http", stackName),
		fmt.Sprintf("%s-distributor-grpc", stackName),
		fmt.Sprintf("%s-index-gateway-http", stackName),
		fmt.Sprintf("%s-index-gateway-grpc", stackName),
		fmt.Sprintf("%s-ingester-http", stackName),
		fmt.Sprintf("%s-ingester-grpc", stackName),
		fmt.Sprintf("%s-querier-http", stackName),
		fmt.Sprintf("%s-querier-grpc", stackName),
		fmt.Sprintf("%s-query-frontend-http", stackName),
		fmt.Sprintf("%s-query-frontend-grpc", stackName),
		fmt.Sprintf("%s-ruler-http", stackName),
		fmt.Sprintf("%s-ruler-grpc", stackName),
	}
}
