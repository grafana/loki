package listeners

import (
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/l7policies"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

type LoadBalancerID struct {
	ID string `json:"id"`
}

// Listener is the primary load balancing configuration object that specifies
// the loadbalancer and port on which client traffic is received, as well
// as other details such as the load balancing method to be use, protocol, etc.
type Listener struct {
	// The unique ID for the Listener.
	ID string `json:"id"`

	// Owner of the Listener.
	ProjectID string `json:"project_id"`

	// Human-readable name for the Listener. Does not have to be unique.
	Name string `json:"name"`

	// Human-readable description for the Listener.
	Description string `json:"description"`

	// The protocol to loadbalance. A valid value is TCP, SCTP, HTTP, HTTPS or TERMINATED_HTTPS.
	Protocol string `json:"protocol"`

	// The port on which to listen to client traffic that is associated with the
	// Loadbalancer. A valid value is from 0 to 65535.
	ProtocolPort int `json:"protocol_port"`

	// The UUID of default pool. Must have compatible protocol with listener.
	DefaultPoolID string `json:"default_pool_id"`

	// The default pool with which the Listener is associated.
	DefaultPool *pools.Pool `json:"default_pool"`

	// A list of load balancer IDs.
	Loadbalancers []LoadBalancerID `json:"loadbalancers"`

	// The maximum number of connections allowed for the Loadbalancer.
	// Default is -1, meaning no limit.
	ConnLimit int `json:"connection_limit"`

	// The list of references to TLS secrets.
	SniContainerRefs []string `json:"sni_container_refs"`

	// A reference to a Barbican container of TLS secrets.
	DefaultTlsContainerRef string `json:"default_tls_container_ref"`

	// The administrative state of the Listener. A valid value is true (UP) or false (DOWN).
	AdminStateUp bool `json:"admin_state_up"`

	// Pools are the pools which are part of this listener.
	Pools []pools.Pool `json:"pools"`

	// L7policies are the L7 policies which are part of this listener.
	L7Policies []l7policies.L7Policy `json:"l7policies"`

	// The provisioning status of the Listener.
	// This value is ACTIVE, PENDING_* or ERROR.
	ProvisioningStatus string `json:"provisioning_status"`

	// Frontend client inactivity timeout in milliseconds
	TimeoutClientData int `json:"timeout_client_data"`

	// Backend member inactivity timeout in milliseconds
	TimeoutMemberData int `json:"timeout_member_data"`

	// Backend member connection timeout in milliseconds
	TimeoutMemberConnect int `json:"timeout_member_connect"`

	// Time, in milliseconds, to wait for additional TCP packets for content inspection
	TimeoutTCPInspect int `json:"timeout_tcp_inspect"`

	// A dictionary of optional headers to insert into the request before it is sent to the backend member.
	InsertHeaders map[string]string `json:"insert_headers"`

	// A list of IPv4, IPv6 or mix of both CIDRs
	AllowedCIDRs []string `json:"allowed_cidrs"`

	// List of ciphers in OpenSSL format (colon-separated). See
	// https://www.openssl.org/docs/man1.1.1/man1/ciphers.html
	// New in version 2.15
	TLSCiphers string `json:"tls_ciphers"`

	// A list of TLS protocol versions. Available from microversion 2.17
	TLSVersions []string `json:"tls_versions"`

	// Tags is a list of resource tags. Tags are arbitrarily defined strings
	// attached to the resource. New in version 2.5
	Tags []string `json:"tags"`

	// A list of ALPN protocols. Available protocols: http/1.0, http/1.1, h2
	// New in version 2.20
	ALPNProtocols []string `json:"alpn_protocols"`

	// The TLS client authentication mode. One of the options NONE, OPTIONAL or MANDATORY.
	// New in version 2.8
	ClientAuthentication string `json:"client_authentication"`

	// The ref of the key manager service secret containing a PEM format
	// client CA certificate bundle for TERMINATED_HTTPS listeners.
	// New in version 2.8
	ClientCATLSContainerRef string `json:"client_ca_tls_container_ref"`

	// The URI of the key manager service secret containing a PEM format CA
	// revocation list file for TERMINATED_HTTPS listeners.
	// New in version 2.8
	ClientCRLContainerRef string `json:"client_crl_container_ref"`

	// Defines whether the includeSubDomains directive should be added to
	// the Strict-Transport-Security HTTP response header. This requires
	// setting the hsts_max_age option as well in order to become
	// effective. Available from microversion 2.27.
	HSTSIncludeSubdomains bool `json:"hsts_include_subdomains"`

	// The value of the max_age directive for the Strict-Transport-Security
	// HTTP response header. Setting this enables HTTP Strict Transport
	// Security (HSTS) for the TLS-terminated listener. Available from
	// microversion 2.27.
	HSTSMaxAge int `json:"hsts_max_age"`

	// Defines whether the preload directive should be added to the
	// Strict-Transport-Security HTTP response header. This requires
	// setting the hsts_max_age option as well in order to become
	// effective. Available from microversion 2.27.
	HSTSPreload bool `json:"hsts_preload"`

	// The operating status of the resource
	OperatingStatus string `json:"operating_status"`
}

type Stats struct {
	// The currently active connections.
	ActiveConnections int `json:"active_connections"`

	// The total bytes received.
	BytesIn int `json:"bytes_in"`

	// The total bytes sent.
	BytesOut int `json:"bytes_out"`

	// The total requests that were unable to be fulfilled.
	RequestErrors int `json:"request_errors"`

	// The total connections handled.
	TotalConnections int `json:"total_connections"`
}

// ListenerPage is the page returned by a pager when traversing over a
// collection of listeners.
type ListenerPage struct {
	pagination.LinkedPageBase
}

// NextPageURL is invoked when a paginated collection of listeners has reached
// the end of a page and the pager seeks to traverse over a new one. In order
// to do this, it needs to construct the next page's URL.
func (r ListenerPage) NextPageURL() (string, error) {
	var s struct {
		Links []gophercloud.Link `json:"listeners_links"`
	}
	err := r.ExtractInto(&s)
	if err != nil {
		return "", err
	}
	return gophercloud.ExtractNextURL(s.Links)
}

// IsEmpty checks whether a ListenerPage struct is empty.
func (r ListenerPage) IsEmpty() (bool, error) {
	if r.StatusCode == 204 {
		return true, nil
	}

	is, err := ExtractListeners(r)
	return len(is) == 0, err
}

// ExtractListeners accepts a Page struct, specifically a ListenerPage struct,
// and extracts the elements into a slice of Listener structs. In other words,
// a generic collection is mapped into a relevant slice.
func ExtractListeners(r pagination.Page) ([]Listener, error) {
	var s struct {
		Listeners []Listener `json:"listeners"`
	}
	err := (r.(ListenerPage)).ExtractInto(&s)
	return s.Listeners, err
}

type commonResult struct {
	gophercloud.Result
}

// Extract is a function that accepts a result and extracts a listener.
func (r commonResult) Extract() (*Listener, error) {
	var s struct {
		Listener *Listener `json:"listener"`
	}
	err := r.ExtractInto(&s)
	return s.Listener, err
}

// CreateResult represents the result of a create operation. Call its Extract
// method to interpret it as a Listener.
type CreateResult struct {
	commonResult
}

// GetResult represents the result of a get operation. Call its Extract
// method to interpret it as a Listener.
type GetResult struct {
	commonResult
}

// UpdateResult represents the result of an update operation. Call its Extract
// method to interpret it as a Listener.
type UpdateResult struct {
	commonResult
}

// DeleteResult represents the result of a delete operation. Call its
// ExtractErr method to determine if the request succeeded or failed.
type DeleteResult struct {
	gophercloud.ErrResult
}

// StatsResult represents the result of a GetStats operation.
// Call its Extract method to interpret it as a Stats.
type StatsResult struct {
	gophercloud.Result
}

// Extract is a function that accepts a result and extracts the status of
// a Listener.
func (r StatsResult) Extract() (*Stats, error) {
	var s struct {
		Stats *Stats `json:"stats"`
	}
	err := r.ExtractInto(&s)
	return s.Stats, err
}
