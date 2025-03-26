package loadbalancers

import (
	"context"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/listeners"
	"github.com/gophercloud/gophercloud/v2/openstack/loadbalancer/v2/pools"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

// ListOptsBuilder allows extensions to add additional parameters to the
// List request.
type ListOptsBuilder interface {
	ToLoadBalancerListQuery() (string, error)
}

// ListOpts allows the filtering and sorting of paginated collections through
// the API. Filtering is achieved by passing in struct field values that map to
// the Loadbalancer attributes you want to see returned. SortKey allows you to
// sort by a particular attribute. SortDir sets the direction, and is
// either `asc' or `desc'. Marker and Limit are used for pagination.
type ListOpts struct {
	Description        string   `q:"description"`
	AdminStateUp       *bool    `q:"admin_state_up"`
	ProjectID          string   `q:"project_id"`
	ProvisioningStatus string   `q:"provisioning_status"`
	VipAddress         string   `q:"vip_address"`
	VipPortID          string   `q:"vip_port_id"`
	VipSubnetID        string   `q:"vip_subnet_id"`
	VipNetworkID       string   `q:"vip_network_id"`
	ID                 string   `q:"id"`
	OperatingStatus    string   `q:"operating_status"`
	Name               string   `q:"name"`
	FlavorID           string   `q:"flavor_id"`
	AvailabilityZone   string   `q:"availability_zone"`
	Provider           string   `q:"provider"`
	Limit              int      `q:"limit"`
	Marker             string   `q:"marker"`
	SortKey            string   `q:"sort_key"`
	SortDir            string   `q:"sort_dir"`
	Tags               []string `q:"tags"`
	TagsAny            []string `q:"tags-any"`
	TagsNot            []string `q:"not-tags"`
	TagsNotAny         []string `q:"not-tags-any"`
}

// ToLoadBalancerListQuery formats a ListOpts into a query string.
func (opts ListOpts) ToLoadBalancerListQuery() (string, error) {
	q, err := gophercloud.BuildQueryString(opts)
	return q.String(), err
}

// List returns a Pager which allows you to iterate over a collection of
// load balancers. It accepts a ListOpts struct, which allows you to filter
// and sort the returned collection for greater efficiency.
//
// Default policy settings return only those load balancers that are owned by
// the project who submits the request, unless an admin user submits the request.
func List(c *gophercloud.ServiceClient, opts ListOptsBuilder) pagination.Pager {
	url := rootURL(c)
	if opts != nil {
		query, err := opts.ToLoadBalancerListQuery()
		if err != nil {
			return pagination.Pager{Err: err}
		}
		url += query
	}
	return pagination.NewPager(c, url, func(r pagination.PageResult) pagination.Page {
		return LoadBalancerPage{pagination.LinkedPageBase{PageResult: r}}
	})
}

// CreateOptsBuilder allows extensions to add additional parameters to the
// Create request.
type CreateOptsBuilder interface {
	ToLoadBalancerCreateMap() (map[string]any, error)
}

// CreateOpts is the common options struct used in this package's Create
// operation.
type CreateOpts struct {
	// Human-readable name for the Loadbalancer. Does not have to be unique.
	Name string `json:"name,omitempty"`

	// Human-readable description for the Loadbalancer.
	Description string `json:"description,omitempty"`

	// Providing a neutron port ID for the vip_port_id tells Octavia to use this
	// port for the VIP. If the port has more than one subnet you must specify
	// either the vip_subnet_id or vip_address to clarify which address should
	// be used for the VIP.
	VipPortID string `json:"vip_port_id,omitempty"`

	// The subnet on which to allocate the Loadbalancer's address. A project can
	// only create Loadbalancers on networks authorized by policy (e.g. networks
	// that belong to them or networks that are shared).
	VipSubnetID string `json:"vip_subnet_id,omitempty"`

	// The network on which to allocate the Loadbalancer's address. A tenant can
	// only create Loadbalancers on networks authorized by policy (e.g. networks
	// that belong to them or networks that are shared).
	VipNetworkID string `json:"vip_network_id,omitempty"`

	// ProjectID is the UUID of the project who owns the Loadbalancer.
	// Only administrative users can specify a project UUID other than their own.
	ProjectID string `json:"project_id,omitempty"`

	// The IP address of the Loadbalancer.
	VipAddress string `json:"vip_address,omitempty"`

	// The ID of the QoS Policy which will apply to the Virtual IP
	VipQosPolicyID string `json:"vip_qos_policy_id,omitempty"`

	// The administrative state of the Loadbalancer. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// The UUID of a flavor.
	FlavorID string `json:"flavor_id,omitempty"`

	// The name of an Octavia availability zone.
	// Requires Octavia API version 2.14 or later.
	AvailabilityZone string `json:"availability_zone,omitempty"`

	// The name of the provider.
	Provider string `json:"provider,omitempty"`

	// Listeners is a slice of listeners.CreateOpts which allows a set
	// of listeners to be created at the same time the Loadbalancer is created.
	//
	// This is only possible to use when creating a fully populated
	// load balancer.
	Listeners []listeners.CreateOpts `json:"listeners,omitempty"`

	// Pools is a slice of pools.CreateOpts which allows a set of pools
	// to be created at the same time the Loadbalancer is created.
	//
	// This is only possible to use when creating a fully populated
	// load balancer.
	Pools []pools.CreateOpts `json:"pools,omitempty"`

	// Tags is a set of resource tags.
	Tags []string `json:"tags,omitempty"`

	// The additional ips of the loadbalancer. Subnets must all belong to the same network as the primary VIP.
	// New in version 2.26
	AdditionalVips []AdditionalVip `json:"additional_vips,omitempty"`
}

// ToLoadBalancerCreateMap builds a request body from CreateOpts.
func (opts CreateOpts) ToLoadBalancerCreateMap() (map[string]any, error) {
	return gophercloud.BuildRequestBody(opts, "loadbalancer")
}

// Create is an operation which provisions a new loadbalancer based on the
// configuration defined in the CreateOpts struct. Once the request is
// validated and progress has started on the provisioning process, a
// CreateResult will be returned.
func Create(ctx context.Context, c *gophercloud.ServiceClient, opts CreateOptsBuilder) (r CreateResult) {
	b, err := opts.ToLoadBalancerCreateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Post(ctx, rootURL(c), b, &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// Get retrieves a particular Loadbalancer based on its unique ID.
func Get(ctx context.Context, c *gophercloud.ServiceClient, id string) (r GetResult) {
	resp, err := c.Get(ctx, resourceURL(c, id), &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// UpdateOptsBuilder allows extensions to add additional parameters to the
// Update request.
type UpdateOptsBuilder interface {
	ToLoadBalancerUpdateMap() (map[string]any, error)
}

// UpdateOpts is the common options struct used in this package's Update
// operation.
type UpdateOpts struct {
	// Human-readable name for the Loadbalancer. Does not have to be unique.
	Name *string `json:"name,omitempty"`

	// Human-readable description for the Loadbalancer.
	Description *string `json:"description,omitempty"`

	// The administrative state of the Loadbalancer. A valid value is true (UP)
	// or false (DOWN).
	AdminStateUp *bool `json:"admin_state_up,omitempty"`

	// The ID of the QoS Policy which will apply to the Virtual IP
	VipQosPolicyID *string `json:"vip_qos_policy_id,omitempty"`

	// Tags is a set of resource tags.
	Tags *[]string `json:"tags,omitempty"`
}

// ToLoadBalancerUpdateMap builds a request body from UpdateOpts.
func (opts UpdateOpts) ToLoadBalancerUpdateMap() (map[string]any, error) {
	return gophercloud.BuildRequestBody(opts, "loadbalancer")
}

// Update is an operation which modifies the attributes of the specified
// LoadBalancer.
func Update(ctx context.Context, c *gophercloud.ServiceClient, id string, opts UpdateOpts) (r UpdateResult) {
	b, err := opts.ToLoadBalancerUpdateMap()
	if err != nil {
		r.Err = err
		return
	}
	resp, err := c.Put(ctx, resourceURL(c, id), b, &r.Body, &gophercloud.RequestOpts{
		OkCodes: []int{200, 202},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// DeleteOptsBuilder allows extensions to add additional parameters to the
// Delete request.
type DeleteOptsBuilder interface {
	ToLoadBalancerDeleteQuery() (string, error)
}

// DeleteOpts is the common options struct used in this package's Delete
// operation.
type DeleteOpts struct {
	// Cascade will delete all children of the load balancer (listners, monitors, etc).
	Cascade bool `q:"cascade"`
}

// ToLoadBalancerDeleteQuery formats a DeleteOpts into a query string.
func (opts DeleteOpts) ToLoadBalancerDeleteQuery() (string, error) {
	q, err := gophercloud.BuildQueryString(opts)
	return q.String(), err
}

// Delete will permanently delete a particular LoadBalancer based on its
// unique ID.
func Delete(ctx context.Context, c *gophercloud.ServiceClient, id string, opts DeleteOptsBuilder) (r DeleteResult) {
	url := resourceURL(c, id)
	if opts != nil {
		query, err := opts.ToLoadBalancerDeleteQuery()
		if err != nil {
			r.Err = err
			return
		}
		url += query
	}
	resp, err := c.Delete(ctx, url, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// GetStatuses will return the status of a particular LoadBalancer.
func GetStatuses(ctx context.Context, c *gophercloud.ServiceClient, id string) (r GetStatusesResult) {
	resp, err := c.Get(ctx, statusRootURL(c, id), &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// GetStats will return the shows the current statistics of a particular LoadBalancer.
func GetStats(ctx context.Context, c *gophercloud.ServiceClient, id string) (r StatsResult) {
	resp, err := c.Get(ctx, statisticsRootURL(c, id), &r.Body, nil)
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// Failover performs a failover of a load balancer.
func Failover(ctx context.Context, c *gophercloud.ServiceClient, id string) (r FailoverResult) {
	resp, err := c.Put(ctx, failoverRootURL(c, id), nil, nil, &gophercloud.RequestOpts{
		OkCodes: []int{202},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}
