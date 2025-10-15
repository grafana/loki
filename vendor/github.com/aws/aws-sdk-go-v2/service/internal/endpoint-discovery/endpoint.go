package endpointdiscovery

import (
	"net/url"
	"time"
)

// Endpoint represents an endpoint used in endpoint discovery.
type Endpoint struct {
	Key       string
	Addresses WeightedAddresses
}

// WeightedAddresses represents a list of WeightedAddress.
type WeightedAddresses []WeightedAddress

// WeightedAddress represents an address with a given weight.
type WeightedAddress struct {
	URL     *url.URL
	Expired time.Time
}

// HasExpired will return whether or not the endpoint has expired with
// the exception of a zero expiry meaning does not expire.
func (e WeightedAddress) HasExpired() bool {
	return e.Expired.Before(time.Now())
}

// Add will add a given WeightedAddress to the address list of Endpoint.
func (e *Endpoint) Add(addr WeightedAddress) {
	e.Addresses = append(e.Addresses, addr)
}

// Len returns the number of valid endpoints where valid means the endpoint
// has not expired.
func (e *Endpoint) Len() int {
	validEndpoints := 0
	for _, endpoint := range e.Addresses {
		if endpoint.HasExpired() {
			continue
		}

		validEndpoints++
	}
	return validEndpoints
}

// GetValidAddress will return a non-expired weight endpoint
func (e *Endpoint) GetValidAddress() (WeightedAddress, bool) {
	for i := 0; i < len(e.Addresses); i++ {
		we := e.Addresses[i]

		if we.HasExpired() {
			continue
		}

		we.URL = cloneURL(we.URL)

		return we, true
	}

	return WeightedAddress{}, false
}

// Prune will prune the expired addresses from the endpoint by allocating a new []WeightAddress.
// This is not concurrent safe, and should be called from a single owning thread.
func (e *Endpoint) Prune() bool {
	validLen := e.Len()
	if validLen == len(e.Addresses) {
		return false
	}
	wa := make([]WeightedAddress, 0, validLen)
	for i := range e.Addresses {
		if e.Addresses[i].HasExpired() {
			continue
		}
		wa = append(wa, e.Addresses[i])
	}
	e.Addresses = wa
	return true
}

func cloneURL(u *url.URL) (clone *url.URL) {
	clone = &url.URL{}

	*clone = *u

	if u.User != nil {
		user := *u.User
		clone.User = &user
	}

	return clone
}
