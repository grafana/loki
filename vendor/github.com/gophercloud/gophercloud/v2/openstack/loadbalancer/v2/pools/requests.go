package pools

import (
	"context"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/monitors"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

// Type TLSVersion represents a tls version
type TLSVersion string

const (
	TLSVersionSSLv3   TLSVersion = "SSLv3"
	TLSVersionTLSv1   TLSVersion = "TLSv1"
	TLSVersionTLSv1_1 TLSVersion = "TLSv1.1"
	TLSVersionTLSv1_2 TLSVersion = "TLSv1.2"
	TLSVersionTLSv1_3 TLSVersion = "TLSv1.3"
)

// ListOptsBuilder allows extensions to add additional parameters to the
// List request.
type ListOptsBuilder interface {
	ToPoolListQuery() (string, error)
}

// ListOpts allows the filtering and sorting of paginated collections through
// the API. Filtering is achieved by passing in struct field values that map to
// the Pool attributes you want to see returned. SortKey allows you to
// sort by a particular Pool attribute. SortDir sets the direction, and is
// either `asc' or `desc'. Marker and Limit are used for pagination.
type ListOpts struct {
	LBMethod       string   `q:"lb_algorithm"`
	Protocol       string   `q:"protocol"`
	ProjectID      string   `q:"project_id"`
	AdminStateUp   *bool    `q:"admin_state_up"`
	Name           string   `q:"name"`
	ID             string   `q:"id"`
	LoadbalancerID string   `q:"loadbalancer_id"`
	Limit          int      `q:"limit"`
	Marker         string   `q:"marker"`
	SortKey        string   `q:"sort_key"`
	SortDir        string   `q:"sort_dir"`
	Tags           []string `q:"tags"`
}

// ToPoolListQuery formats a ListOpts into a query string.
func (opts ListOpts) ToPoolListQuery() (string, error) {
	q, err := gophercloud.BuildQueryString(opts)
	return q.String(), err
}

// List returns a Pager which allows you to iterate over a collection of
// pools. It accepts a ListOpts struct, which allows you to filter and sort
// the returned collection for greater efficiency.
//
// Default policy settings return only those pools that are owned by the
// project who submits the request, unless an admin user submits the request.
func List(c *gophercloud.ServiceClient, opts ListOptsBuilder) pagination.Pager {
	url := rootURL(c)
	if opts != nil {
		query, err := opts.ToPoolListQuery()
		if err != nil {
			return pagination.Pager{Err: err}
		}
		url += query
	}
	return pagination.NewPager(c, url, func(r pagination.PageResult) pagination.Page {
		return PoolPage{pagination.LinkedPageBase{PageResult: r}}
	})
}

type LBMethod string
type Protocol string

// Supported attributes for create/update operations.
const (
	LBMethodRoundRobin       LBMethod = "ROUND_ROBIN"
	LBMethodLeastConnections LBMethod = "LEAST_CONNECTIONS"
	LBMethodSourceIp         LBMethod = "SOURCE_IP"
	LBMethodSourceIpPort     LBMethod = "SOURCE_IP_PORT"

	ProtocolTCP   Protocol = "TCP"
	ProtocolUDP   Protocol = "UDP"
	ProtocolPROXY Protocol = "PROXY"
	ProtocolHTTP  Protocol = "HTTP"
	ProtocolHTTPS Protocol = "HTTPS"
	// Protocol PROXYV2 requires octavia microversion 2.22
	ProtocolPROXYV2 Protocol = "PROXYV2"
	// Protocol SCTP requires octavia microversion 2.23
	ProtocolSCTP Protocol = "SCTP"
)

// CreateOptsBuilder allows extensions to add additional parameters to the
// Create request.
type CreateOptsBuilder interface {
	ToPoolCreateMap() (map[string]any, error)
}

// CreateOpts is the common options struct used in this package's Create
// operation.
type CreateOpts struct {
	// The algorithm used to distribute load between the members of the pool. The
	// current specification supports LBMethodRoundRobin, LBMethodLeastConnections,
	// LBMethodSourceIp and LBMethodSourceIpPort as valid values for this attribute.
	LBMethod LBMethod `json:"lb_algorithm" required:"true"`

	// The protocol used by the pool members, you can use either
	// ProtocolTCP, ProtocolUDP, ProtocolPROXY, ProtocolHTTP, ProtocolHTTPS,
	// ProtocolSCTP or ProtocolPROXYV2.
	Protocol Protocol `json:"protocol" required:"true"`

	// The Loadbalancer on which the members of the pool will be associated with.
	// Note: one of LoadbalancerID or ListenerID must be provided.
	LoadbalancerID string `json:"loadbalancer_id,omitempty"`

	// The Listener on which the members of the pool will be associated with.
	// Note: one of LoadbalancerID or ListenerID must be provided.
	ListenerID string `json:"listener_id,omitempty"`

	// ProjectID is the UUID of the project who owns the Pool.
	// Only administrative users can specify a project UUID other than their own.
	ProjectID string `json:"project_id,omitempty"`

	// Name of the pool.
	Name string `json:"name,omitempty"`

	// Human-readable description for the pool.
	Description string `json:"description,omitempty"`

	// Persistence is the session persistence of the pool.
	// Omit this field to prevent session persistence.
	Persistence *SessionPersistence `json:"session_persistence,omitempty"`

	// A list of ALPN protocols. Available protocols: http/1.0, http/1.1,
	// h2. Available from microversion 2.24.
	ALPNProtocols []string `json:"alpn_protocols,omitempty"`

	// The reference of the key manager service secret containing a PEM
	// format CA certificate bundle for tls_enabled pools. Available from
	// microversion 2.8.
	CATLSContainerRef string `json:"ca_tls_container_ref,omitempty"`

	// The reference of the key manager service secret containing a PEM
	// format CA revocation list file for tls_enabled pools. Available from
	// microversion 2.8.
	CRLContainerRef string `json:"crl_container_ref,omitempty"`

	// When true connections to backend member servers will use TLS
	// encryption. Default is false. Available from microversion 2.8.
	TLSEnabled bool `json:"tls_enabled,omitempty"`

	// List of ciphers in OpenSSL format (colon-separated). Available from
	// microversion 2.15.
	TLSCiphers string `json:"tls_ciphers,omitempty"`

	// The reference to the key manager service secret containing a PKCS12
	// format certificate/key bundle for tls_enabled pools for TLS client
	// authentication to the member servers. Available from microversion 2.8.
	TLSContainerRef string `json:"tls_container_ref,omitempty"`

	// A list of TLS protocol versions. Available versions: SSLv3, TLSv1,
	// TLSv1.1, TLSv1.2, TLSv1.3. Available from microversion 2.17.
	TLSVersions []TLSVersion `json:"tls_versions,omitempty"`

	// The administrative state of the Pool. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Members is a slice of CreateMemberOpts which allows a set of
	// members to be created at the same time the pool is created.
	//
	// This is only possible to use when creating a fully populated
	// Loadbalancer.
	Members []CreateMemberOpts `json:"members,omitempty"`

	// Monitor is an instance of monitors.CreateOpts which allows a monitor
	// to be created at the same time the pool is created.
	//
	// This is only possible to use when creating a fully populated
	// Loadbalancer.
	Monitor monitors.CreateOptsBuilder `json:"healthmonitor,omitempty"`

	// Tags is a set of resource tags. New in version 2.5
	Tags []string `json:"tags,omitempty"`
}

// ToPoolCreateMap builds a request body from CreateOpts.
func (opts CreateOpts) ToPoolCreateMap() (map[string]any, error) {
	return gophercloud.BuildRequestBody(opts, "pool")
}

// Create accepts a CreateOpts struct and uses the values to create a new
// load balancer pool.
func Create(ctx context.Context, c *gophercloud.ServiceClient, opts CreateOptsBuilder) (r CreateResult) {
	b, err := opts.ToPoolCreateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Post(ctx, rootURL(c), b, &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// Get retrieves a particular pool based on its unique ID.
func Get(ctx context.Context, c *gophercloud.ServiceClient, id string) (r GetResult) {
	resp, err := c.Get(ctx, resourceURL(c, id), &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// UpdateOptsBuilder allows extensions to add additional parameters to the
// Update request.
type UpdateOptsBuilder interface {
	ToPoolUpdateMap() (map[string]any, error)
}

// UpdateOpts is the common options struct used in this package's Update
// operation.
type UpdateOpts struct {
	// Name of the pool.
	Name *string `json:"name,omitempty"`

	// Human-readable description for the pool.
	Description *string `json:"description,omitempty"`

	// The algorithm used to distribute load between the members of the pool. The
	// current specification supports LBMethodRoundRobin, LBMethodLeastConnections,
	// LBMethodSourceIp and LBMethodSourceIpPort as valid values for this attribute.
	LBMethod LBMethod `json:"lb_algorithm,omitempty"`

	// The administrative state of the Pool. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Persistence is the session persistence of the pool.
	Persistence *SessionPersistence `json:"session_persistence,omitempty"`

	// A list of ALPN protocols. Available protocols: http/1.0, http/1.1,
	// h2. Available from microversion 2.24.
	ALPNProtocols *[]string `json:"alpn_protocols,omitempty"`

	// The reference of the key manager service secret containing a PEM
	// format CA certificate bundle for tls_enabled pools. Available from
	// microversion 2.8.
	CATLSContainerRef *string `json:"ca_tls_container_ref,omitempty"`

	// The reference of the key manager service secret containing a PEM
	// format CA revocation list file for tls_enabled pools. Available from
	// microversion 2.8.
	CRLContainerRef *string `json:"crl_container_ref,omitempty"`

	// When true connections to backend member servers will use TLS
	// encryption. Default is false. Available from microversion 2.8.
	TLSEnabled *bool `json:"tls_enabled,omitempty"`

	// List of ciphers in OpenSSL format (colon-separated). Available from
	// microversion 2.15.
	TLSCiphers *string `json:"tls_ciphers,omitempty"`

	// The reference to the key manager service secret containing a PKCS12
	// format certificate/key bundle for tls_enabled pools for TLS client
	// authentication to the member servers. Available from microversion 2.8.
	TLSContainerRef *string `json:"tls_container_ref,omitempty"`

	// A list of TLS protocol versions. Available versions: SSLv3, TLSv1,
	// TLSv1.1, TLSv1.2, TLSv1.3. Available from microversion 2.17.
	TLSVersions *[]TLSVersion `json:"tls_versions,omitempty"`

	// Tags is a set of resource tags. New in version 2.5
	Tags *[]string `json:"tags,omitempty"`
}

// ToPoolUpdateMap builds a request body from UpdateOpts.
func (opts UpdateOpts) ToPoolUpdateMap() (map[string]any, error) {
	b, err := gophercloud.BuildRequestBody(opts, "pool")
	if err != nil {
		return nil, err
	}

	m := b["pool"].(map[string]any)

	// allow to unset session_persistence on empty SessionPersistence struct
	if opts.Persistence != nil && *opts.Persistence == (SessionPersistence{}) {
		m["session_persistence"] = nil
	}

	// allow to unset alpn_protocols on empty slice
	if opts.ALPNProtocols != nil && len(*opts.ALPNProtocols) == 0 {
		m["alpn_protocols"] = nil
	}

	// allow to unset tls_versions on empty slice
	if opts.TLSVersions != nil && len(*opts.TLSVersions) == 0 {
		m["tls_versions"] = nil
	}

	return b, nil
}

// Update allows pools to be updated.
func Update(ctx context.Context, c *gophercloud.ServiceClient, id string, opts UpdateOptsBuilder) (r UpdateResult) {
	b, err := opts.ToPoolUpdateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Put(ctx, resourceURL(c, id), b, &r.Body, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// Delete will permanently delete a particular pool based on its unique ID.
func Delete(ctx context.Context, c *gophercloud.ServiceClient, id string) (r DeleteResult) {
	resp, err := c.Delete(ctx, resourceURL(c, id), nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// ListMemberOptsBuilder allows extensions to add additional parameters to the
// ListMembers request.
type ListMembersOptsBuilder interface {
	ToMembersListQuery() (string, error)
}

// ListMembersOpts allows the filtering and sorting of paginated collections
// through the API. Filtering is achieved by passing in struct field values
// that map to the Member attributes you want to see returned. SortKey allows
// you to sort by a particular Member attribute. SortDir sets the direction,
// and is either `asc' or `desc'. Marker and Limit are used for pagination.
type ListMembersOpts struct {
	Name         string `q:"name"`
	Weight       int    `q:"weight"`
	AdminStateUp *bool  `q:"admin_state_up"`
	ProjectID    string `q:"project_id"`
	Address      string `q:"address"`
	ProtocolPort int    `q:"protocol_port"`
	ID           string `q:"id"`
	Limit        int    `q:"limit"`
	Marker       string `q:"marker"`
	SortKey      string `q:"sort_key"`
	SortDir      string `q:"sort_dir"`
}

// ToMemberListQuery formats a ListOpts into a query string.
func (opts ListMembersOpts) ToMembersListQuery() (string, error) {
	q, err := gophercloud.BuildQueryString(opts)
	return q.String(), err
}

// ListMembers returns a Pager which allows you to iterate over a collection of
// members. It accepts a ListMembersOptsBuilder, which allows you to filter and
// sort the returned collection for greater efficiency.
//
// Default policy settings return only those members that are owned by the
// project who submits the request, unless an admin user submits the request.
func ListMembers(c *gophercloud.ServiceClient, poolID string, opts ListMembersOptsBuilder) pagination.Pager {
	url := memberRootURL(c, poolID)
	if opts != nil {
		query, err := opts.ToMembersListQuery()
		if err != nil {
			return pagination.Pager{Err: err}
		}
		url += query
	}
	return pagination.NewPager(c, url, func(r pagination.PageResult) pagination.Page {
		return MemberPage{pagination.LinkedPageBase{PageResult: r}}
	})
}

// CreateMemberOptsBuilder allows extensions to add additional parameters to the
// CreateMember request.
type CreateMemberOptsBuilder interface {
	ToMemberCreateMap() (map[string]any, error)
}

// CreateMemberOpts is the common options struct used in this package's CreateMember
// operation.
type CreateMemberOpts struct {
	// The IP address of the member to receive traffic from the load balancer.
	Address string `json:"address" required:"true"`

	// The port on which to listen for client traffic.
	ProtocolPort int `json:"protocol_port" required:"true"`

	// Name of the Member.
	Name string `json:"name,omitempty"`

	// ProjectID is the UUID of the project who owns the Member.
	// Only administrative users can specify a project UUID other than their own.
	ProjectID string `json:"project_id,omitempty"`

	// A positive integer value that indicates the relative portion of traffic
	// that this member should receive from the pool. For example, a member with
	// a weight of 10 receives five times as much traffic as a member with a
	// weight of 2.
	Weight *int `json:"weight,omitempty"`

	// If you omit this parameter, LBaaS uses the vip_subnet_id parameter value
	// for the subnet UUID.
	SubnetID string `json:"subnet_id,omitempty"`

	// The administrative state of the Pool. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Is the member a backup? Backup members only receive traffic when all
	// non-backup members are down.
	// Requires microversion 2.1 or later.
	Backup *bool `json:"backup,omitempty"`

	// An alternate IP address used for health monitoring a backend member.
	MonitorAddress string `json:"monitor_address,omitempty"`

	// An alternate protocol port used for health monitoring a backend member.
	MonitorPort *int `json:"monitor_port,omitempty"`

	// A list of simple strings assigned to the resource.
	// Requires microversion 2.5 or later.
	Tags []string `json:"tags,omitempty"`
}

// ToMemberCreateMap builds a request body from CreateMemberOpts.
func (opts CreateMemberOpts) ToMemberCreateMap() (map[string]any, error) {
	return gophercloud.BuildRequestBody(opts, "member")
}

// CreateMember will create and associate a Member with a particular Pool.
func CreateMember(ctx context.Context, c *gophercloud.ServiceClient, poolID string, opts CreateMemberOptsBuilder) (r CreateMemberResult) {
	b, err := opts.ToMemberCreateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Post(ctx, memberRootURL(c, poolID), b, &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// GetMember retrieves a particular Pool Member based on its unique ID.
func GetMember(ctx context.Context, c *gophercloud.ServiceClient, poolID string, memberID string) (r GetMemberResult) {
	resp, err := c.Get(ctx, memberResourceURL(c, poolID, memberID), &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// UpdateMemberOptsBuilder allows extensions to add additional parameters to the
// List request.
type UpdateMemberOptsBuilder interface {
	ToMemberUpdateMap() (map[string]any, error)
}

// UpdateMemberOpts is the common options struct used in this package's Update
// operation.
type UpdateMemberOpts struct {
	// Name of the Member.
	Name *string `json:"name,omitempty"`

	// A positive integer value that indicates the relative portion of traffic
	// that this member should receive from the pool. For example, a member with
	// a weight of 10 receives five times as much traffic as a member with a
	// weight of 2.
	Weight *int `json:"weight,omitempty"`

	// The administrative state of the Pool. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Is the member a backup? Backup members only receive traffic when all
	// non-backup members are down.
	// Requires microversion 2.1 or later.
	Backup *bool `json:"backup,omitempty"`

	// An alternate IP address used for health monitoring a backend member.
	MonitorAddress *string `json:"monitor_address,omitempty"`

	// An alternate protocol port used for health monitoring a backend member.
	MonitorPort *int `json:"monitor_port,omitempty"`

	// A list of simple strings assigned to the resource.
	// Requires microversion 2.5 or later.
	Tags []string `json:"tags,omitempty"`
}

// ToMemberUpdateMap builds a request body from UpdateMemberOpts.
func (opts UpdateMemberOpts) ToMemberUpdateMap() (map[string]any, error) {
	return gophercloud.BuildRequestBody(opts, "member")
}

// Update allows Member to be updated.
func UpdateMember(ctx context.Context, c *gophercloud.ServiceClient, poolID string, memberID string, opts UpdateMemberOptsBuilder) (r UpdateMemberResult) {
	b, err := opts.ToMemberUpdateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Put(ctx, memberResourceURL(c, poolID, memberID), b, &r.Body, &gophercloud.RequestOpts{
		OkCodes: []int{200, 201, 202},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// BatchUpdateMemberOptsBuilder allows extensions to add additional parameters to the BatchUpdateMembers request.
type BatchUpdateMemberOptsBuilder interface {
	ToBatchMemberUpdateMap() (map[string]any, error)
}

// BatchUpdateMemberOpts is the common options struct used in this package's BatchUpdateMembers
// operation.
type BatchUpdateMemberOpts struct {
	// The IP address of the member to receive traffic from the load balancer.
	Address string `json:"address" required:"true"`

	// The port on which to listen for client traffic.
	ProtocolPort int `json:"protocol_port" required:"true"`

	// Name of the Member.
	Name *string `json:"name,omitempty"`

	// ProjectID is the UUID of the project who owns the Member.
	// Only administrative users can specify a project UUID other than their own.
	ProjectID string `json:"project_id,omitempty"`

	// A positive integer value that indicates the relative portion of traffic
	// that this member should receive from the pool. For example, a member with
	// a weight of 10 receives five times as much traffic as a member with a
	// weight of 2.
	Weight *int `json:"weight,omitempty"`

	// If you omit this parameter, LBaaS uses the vip_subnet_id parameter value
	// for the subnet UUID.
	SubnetID *string `json:"subnet_id,omitempty"`

	// The administrative state of the Pool. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// Is the member a backup? Backup members only receive traffic when all
	// non-backup members are down.
	// Requires microversion 2.1 or later.
	Backup *bool `json:"backup,omitempty"`

	// An alternate IP address used for health monitoring a backend member.
	MonitorAddress *string `json:"monitor_address,omitempty"`

	// An alternate protocol port used for health monitoring a backend member.
	MonitorPort *int `json:"monitor_port,omitempty"`

	// A list of simple strings assigned to the resource.
	// Requires microversion 2.5 or later.
	Tags []string `json:"tags,omitempty"`
}

// ToBatchMemberUpdateMap builds a request body from BatchUpdateMemberOpts.
func (opts BatchUpdateMemberOpts) ToBatchMemberUpdateMap() (map[string]any, error) {
	b, err := gophercloud.BuildRequestBody(opts, "")
	if err != nil {
		return nil, err
	}

	if b["subnet_id"] == "" {
		b["subnet_id"] = nil
	}

	return b, nil
}

// BatchUpdateMembers updates the pool members in batch
func BatchUpdateMembers[T BatchUpdateMemberOptsBuilder](ctx context.Context, c *gophercloud.ServiceClient, poolID string, opts []T) (r UpdateMembersResult) {
	members := []map[string]any{}
	for _, opt := range opts {
		b, err := opt.ToBatchMemberUpdateMap()
		if err != nil {
			r.Err = err
			return
		}
		members = append(members, b)
	}

	b := map[string]any{"members": members}

	resp, err := c.Put(ctx, memberRootURL(c, poolID), b, nil, &gophercloud.RequestOpts{OkCodes: []int{202}})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// DeleteMember will remove and disassociate a Member from a particular Pool.
func DeleteMember(ctx context.Context, c *gophercloud.ServiceClient, poolID string, memberID string) (r DeleteMemberResult) {
	resp, err := c.Delete(ctx, memberResourceURL(c, poolID, memberID), nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}
