package witbundle

import "github.com/spiffe/go-spiffe/v2/spiffeid"

// Source represents a source of WIT bundles keyed by trust domain.
type Source interface {
	// GetWITBundleForTrustDomain returns the WIT bundle for the given trust
	// domain.
	GetWITBundleForTrustDomain(trustDomain spiffeid.TrustDomain) (*Bundle, error)
}
