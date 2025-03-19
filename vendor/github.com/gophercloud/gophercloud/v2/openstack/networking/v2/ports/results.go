package ports

import (
	"encoding/json"
	"time"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/pagination"
)

type commonResult struct {
	gophercloud.Result
}

// Extract is a function that accepts a result and extracts a port resource.
func (r commonResult) Extract() (*Port, error) {
	var s Port
	err := r.ExtractInto(&s)
	return &s, err
}

func (r commonResult) ExtractInto(v any) error {
	return r.Result.ExtractIntoStructPtr(v, "port")
}

// CreateResult represents the result of a create operation. Call its Extract
// method to interpret it as a Port.
type CreateResult struct {
	commonResult
}

// GetResult represents the result of a get operation. Call its Extract
// method to interpret it as a Port.
type GetResult struct {
	commonResult
}

// UpdateResult represents the result of an update operation. Call its Extract
// method to interpret it as a Port.
type UpdateResult struct {
	commonResult
}

// DeleteResult represents the result of a delete operation. Call its
// ExtractErr method to determine if the request succeeded or failed.
type DeleteResult struct {
	gophercloud.ErrResult
}

// IP is a sub-struct that represents an individual IP.
type IP struct {
	SubnetID  string `json:"subnet_id"`
	IPAddress string `json:"ip_address,omitempty"`
}

// AddressPair contains the IP Address and the MAC address.
type AddressPair struct {
	IPAddress  string `json:"ip_address,omitempty"`
	MACAddress string `json:"mac_address,omitempty"`
}

// Port represents a Neutron port. See package documentation for a top-level
// description of what this is.
type Port struct {
	// UUID for the port.
	ID string `json:"id"`

	// Network that this port is associated with.
	NetworkID string `json:"network_id"`

	// Human-readable name for the port. Might not be unique.
	Name string `json:"name"`

	// Describes the port.
	Description string `json:"description"`

	// Administrative state of port. If false (down), port does not forward
	// packets.
	AdminStateUp bool `json:"admin_state_up"`

	// Indicates whether network is currently operational. Possible values include
	// `ACTIVE', `DOWN', `BUILD', or `ERROR'. Plug-ins might define additional
	// values.
	Status string `json:"status"`

	// Mac address to use on this port.
	MACAddress string `json:"mac_address"`

	// Specifies IP addresses for the port thus associating the port itself with
	// the subnets where the IP addresses are picked from
	FixedIPs []IP `json:"fixed_ips"`

	// TenantID is the project owner of the port.
	TenantID string `json:"tenant_id"`

	// ProjectID is the project owner of the port.
	ProjectID string `json:"project_id"`

	// Identifies the entity (e.g.: dhcp agent) using this port.
	DeviceOwner string `json:"device_owner"`

	// Specifies the IDs of any security groups associated with a port.
	SecurityGroups []string `json:"security_groups"`

	// Identifies the device (e.g., virtual server) using this port.
	DeviceID string `json:"device_id"`

	// Identifies the list of IP addresses the port will recognize/accept
	AllowedAddressPairs []AddressPair `json:"allowed_address_pairs"`

	// Tags optionally set via extensions/attributestags
	Tags []string `json:"tags"`

	// PropagateUplinkStatus enables/disables propagate uplink status on the port.
	PropagateUplinkStatus bool `json:"propagate_uplink_status"`

	// RevisionNumber optionally set via extensions/standard-attr-revisions
	RevisionNumber int `json:"revision_number"`

	// Timestamp when the port was created
	CreatedAt time.Time `json:"created_at"`

	// Timestamp when the port was last updated
	UpdatedAt time.Time `json:"updated_at"`
}

func (r *Port) UnmarshalJSON(b []byte) error {
	type tmp Port

	// Support for older neutron time format
	var s1 struct {
		tmp
		CreatedAt gophercloud.JSONRFC3339NoZ `json:"created_at"`
		UpdatedAt gophercloud.JSONRFC3339NoZ `json:"updated_at"`
	}

	err := json.Unmarshal(b, &s1)
	if err == nil {
		*r = Port(s1.tmp)
		r.CreatedAt = time.Time(s1.CreatedAt)
		r.UpdatedAt = time.Time(s1.UpdatedAt)

		return nil
	}

	// Support for newer neutron time format
	var s2 struct {
		tmp
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}

	err = json.Unmarshal(b, &s2)
	if err != nil {
		return err
	}

	*r = Port(s2.tmp)
	r.CreatedAt = time.Time(s2.CreatedAt)
	r.UpdatedAt = time.Time(s2.UpdatedAt)

	return nil
}

// PortPage is the page returned by a pager when traversing over a collection
// of network ports.
type PortPage struct {
	pagination.LinkedPageBase
}

// NextPageURL is invoked when a paginated collection of ports has reached
// the end of a page and the pager seeks to traverse over a new one. In order
// to do this, it needs to construct the next page's URL.
func (r PortPage) NextPageURL() (string, error) {
	var s struct {
		Links []gophercloud.Link `json:"ports_links"`
	}
	err := r.ExtractInto(&s)
	if err != nil {
		return "", err
	}
	return gophercloud.ExtractNextURL(s.Links)
}

// IsEmpty checks whether a PortPage struct is empty.
func (r PortPage) IsEmpty() (bool, error) {
	if r.StatusCode == 204 {
		return true, nil
	}

	is, err := ExtractPorts(r)
	return len(is) == 0, err
}

// ExtractPorts accepts a Page struct, specifically a PortPage struct,
// and extracts the elements into a slice of Port structs. In other words,
// a generic collection is mapped into a relevant slice.
func ExtractPorts(r pagination.Page) ([]Port, error) {
	var s []Port
	err := ExtractPortsInto(r, &s)
	return s, err
}

func ExtractPortsInto(r pagination.Page, v any) error {
	return r.(PortPage).Result.ExtractIntoSlicePtr(v, "ports")
}
