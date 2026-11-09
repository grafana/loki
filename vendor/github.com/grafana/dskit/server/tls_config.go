// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/server/tls_config.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package server

import (
	"crypto/tls"
	fmt "fmt"
	"strings"

	"github.com/prometheus/exporter-toolkit/web"
)

// Collect all cipher suite names and IDs recognized by Go, including insecure ones.
func allCiphers() map[string]web.Cipher {
	acceptedCiphers := make(map[string]web.Cipher)
	for _, suite := range tls.CipherSuites() {
		acceptedCiphers[suite.Name] = web.Cipher(suite.ID)
	}
	for _, suite := range tls.InsecureCipherSuites() {
		acceptedCiphers[suite.Name] = web.Cipher(suite.ID)
	}
	return acceptedCiphers
}

func stringToCipherSuites(s string) ([]web.Cipher, error) {
	if s == "" {
		return nil, nil
	}
	ciphersSlice := []web.Cipher{}
	possibleCiphers := allCiphers()
	for _, cipher := range strings.Split(s, ",") {
		intValue, ok := possibleCiphers[cipher]
		if !ok {
			return nil, fmt.Errorf("cipher suite %q not recognized", cipher)
		}
		ciphersSlice = append(ciphersSlice, intValue)
	}
	return ciphersSlice, nil
}

// Using the same names that Kubernetes does
var tlsVersions = map[string]uint16{
	"VersionTLS10": tls.VersionTLS10,
	"VersionTLS11": tls.VersionTLS11,
	"VersionTLS12": tls.VersionTLS12,
	"VersionTLS13": tls.VersionTLS13,
}

// stringToCurvePreferences parses a comma-separated list of TLS curve/group
// names into a slice of web.Curve values recognized by the exporter-toolkit.
func stringToCurvePreferences(s string) ([]web.Curve, error) {
	if s == "" {
		return nil, nil
	}
	var curveSlice []web.Curve
	possibleCurves := allCurves()
	for _, name := range strings.Split(s, ",") {
		curve, ok := possibleCurves[name]
		if !ok {
			return nil, fmt.Errorf("curve %q not recognized", name)
		}
		curveSlice = append(curveSlice, curve)
	}
	return curveSlice, nil
}

// allCurves returns the set of TLS curve/group names supported for key exchange.
// Includes classical ECDH curves and hybrid post-quantum KEM groups.
func allCurves() map[string]web.Curve {
	return map[string]web.Curve{
		"CurveP256":          (web.Curve)(tls.CurveP256),
		"CurveP384":          (web.Curve)(tls.CurveP384),
		"CurveP521":          (web.Curve)(tls.CurveP521),
		"X25519":             (web.Curve)(tls.X25519),
		"X25519MLKEM768":     (web.Curve)(tls.X25519MLKEM768),
		"SecP256r1MLKEM768":  (web.Curve)(tls.SecP256r1MLKEM768),
		"SecP384r1MLKEM1024": (web.Curve)(tls.SecP384r1MLKEM1024),
	}
}

func stringToTLSVersion(s string) (web.TLSVersion, error) {
	if s == "" {
		return 0, nil
	}
	if version, ok := tlsVersions[s]; ok {
		return web.TLSVersion(version), nil
	}
	return 0, fmt.Errorf("TLS version %q not recognized", s)
}
