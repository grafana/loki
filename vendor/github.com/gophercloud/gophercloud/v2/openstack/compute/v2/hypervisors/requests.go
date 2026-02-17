package hypervisors

import (
	"context"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

// ListOptsBuilder allows extensions to add additional parameters to the
// List request.
type ListOptsBuilder interface {
	ToHypervisorListQuery() (string, error)
}

// ListOpts allows the filtering and sorting of paginated collections through
// the API. Filtering is achieved by passing in struct field values that map to
// the server attributes you want to see returned. Marker and Limit are used
// for pagination.
type ListOpts struct {
	// Limit is an integer value for the limit of values to return.
	// This requires microversion 2.33 or later.
	Limit *int `q:"limit"`

	// Marker is the ID of the last-seen item as a UUID.
	// This requires microversion 2.53 or later.
	Marker *string `q:"marker"`

	// HypervisorHostnamePattern is the hypervisor hostname or a portion of it.
	// This requires microversion 2.53 or later
	HypervisorHostnamePattern *string `q:"hypervisor_hostname_pattern"`

	// WithServers is a bool to include all servers which belong to each hypervisor
	// This requires microversion 2.53 or later
	WithServers *bool `q:"with_servers"`
}

// ToHypervisorListQuery formats a ListOpts into a query string.
func (opts ListOpts) ToHypervisorListQuery() (string, error) {
	q, err := gophercloud.BuildQueryString(opts)
	return q.String(), err
}

// List makes a request against the API to list hypervisors.
func List(client *gophercloud.ServiceClient, opts ListOptsBuilder) pagination.Pager {
	url := hypervisorsListDetailURL(client)
	if opts != nil {
		query, err := opts.ToHypervisorListQuery()
		if err != nil {
			return pagination.Pager{Err: err}
		}
		url += query
	}

	return pagination.NewPager(client, url, func(r pagination.PageResult) pagination.Page {
		return HypervisorPage{pagination.LinkedPageBase{PageResult: r}}
	})
}

// Statistics makes a request against the API to get hypervisors statistics.
func GetStatistics(ctx context.Context, client *gophercloud.ServiceClient) (r StatisticsResult) {
	resp, err := client.Get(ctx, hypervisorsStatisticsURL(client), &r.Body, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// GetOptsBuilder allows extensions to add additional parameters to the
// Get request.
type GetOptsBuilder interface {
	ToHypervisorGetQuery() (string, error)
}

// GetOpts allows the opt-in to add the servers to the response
type GetOpts struct {
	// WithServers is a bool to include all servers which belong to the hypervisor
	// This requires microversion 2.53 or later
	WithServers *bool `q:"with_servers"`
}

// ToHypervisorGetQuery formats a GetOpts into a query string.
func (opts GetOpts) ToHypervisorGetQuery() (string, error) {
	q, err := gophercloud.BuildQueryString(opts)
	return q.String(), err
}

// Get makes a request against the API to get details for specific hypervisor.
func Get(ctx context.Context, client *gophercloud.ServiceClient, hypervisorID string) (r HypervisorResult) {
	return GetExt(ctx, client, hypervisorID, nil)
}

// Show makes a request against the API to get details for specific hypervisor with optional query parameters
func GetExt(ctx context.Context, client *gophercloud.ServiceClient, hypervisorID string, opts GetOptsBuilder) (r HypervisorResult) {
	url := hypervisorsGetURL(client, hypervisorID)
	if opts != nil {
		query, err := opts.ToHypervisorGetQuery()
		if err != nil {
			return HypervisorResult{gophercloud.Result{Err: err}}
		}
		url += query
	}

	resp, err := client.Get(ctx, url, &r.Body, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}

// GetUptime makes a request against the API to get uptime for specific hypervisor.
func GetUptime(ctx context.Context, client *gophercloud.ServiceClient, hypervisorID string) (r UptimeResult) {
	resp, err := client.Get(ctx, hypervisorsUptimeURL(client, hypervisorID), &r.Body, &gophercloud.RequestOpts{
		OkCodes: []int{200},
	})
	_, r.Header, r.Err = gophercloud.ParseResponse(resp, err)
	return
}
