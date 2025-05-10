package godo

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const partnerInterconnectAttachmentsBasePath = "/v2/partner_interconnect/attachments"

// PartnerInterconnectAttachmentsService is an interface for managing Partner Interconnect Attachments with the
// DigitalOcean API.
// See: https://docs.digitalocean.com/reference/api/api-reference/#tag/PartnerInterconnectAttachments
type PartnerInterconnectAttachmentsService interface {
	List(context.Context, *ListOptions) ([]*PartnerInterconnectAttachment, *Response, error)
	Create(context.Context, *PartnerInterconnectAttachmentCreateRequest) (*PartnerInterconnectAttachment, *Response, error)
	Get(context.Context, string) (*PartnerInterconnectAttachment, *Response, error)
	Update(context.Context, string, *PartnerInterconnectAttachmentUpdateRequest) (*PartnerInterconnectAttachment, *Response, error)
	Delete(context.Context, string) (*Response, error)
	GetServiceKey(context.Context, string) (*ServiceKey, *Response, error)
	SetRoutes(context.Context, string, *PartnerInterconnectAttachmentSetRoutesRequest) (*PartnerInterconnectAttachment, *Response, error)
	ListRoutes(context.Context, string, *ListOptions) ([]*RemoteRoute, *Response, error)
}

var _ PartnerInterconnectAttachmentsService = &PartnerInterconnectAttachmentsServiceOp{}

// PartnerInterconnectAttachmentsServiceOp interfaces with the Partner Interconnect Attachment endpoints in the DigitalOcean API.
type PartnerInterconnectAttachmentsServiceOp struct {
	client *Client
}

// PartnerInterconnectAttachmentCreateRequest represents a request to create a Partner Interconnect Attachment.
type PartnerInterconnectAttachmentCreateRequest struct {
	// Name is the name of the Partner Interconnect Attachment
	Name string `json:"name,omitempty"`
	// ConnectionBandwidthInMbps is the bandwidth of the connection in Mbps
	ConnectionBandwidthInMbps int `json:"connection_bandwidth_in_mbps,omitempty"`
	// Region is the region where the Partner Interconnect Attachment is created
	Region string `json:"region,omitempty"`
	// NaaSProvider is the name of the Network as a Service provider
	NaaSProvider string `json:"naas_provider,omitempty"`
	// VPCIDs is the IDs of the VPCs to which the Partner Interconnect Attachment is connected
	VPCIDs []string `json:"vpc_ids,omitempty"`
	// BGP is the BGP configuration of the Partner Interconnect Attachment
	BGP BGP `json:"bgp,omitempty"`
}

type partnerInterconnectAttachmentRequestBody struct {
	// Name is the name of the Partner Interconnect Attachment
	Name string `json:"name,omitempty"`
	// ConnectionBandwidthInMbps is the bandwidth of the connection in Mbps
	ConnectionBandwidthInMbps int `json:"connection_bandwidth_in_mbps,omitempty"`
	// Region is the region where the Partner Interconnect Attachment is created
	Region string `json:"region,omitempty"`
	// NaaSProvider is the name of the Network as a Service provider
	NaaSProvider string `json:"naas_provider,omitempty"`
	// VPCIDs is the IDs of the VPCs to which the Partner Interconnect Attachment is connected
	VPCIDs []string `json:"vpc_ids,omitempty"`
	// BGP is the BGP configuration of the Partner Interconnect Attachment
	BGP *BGP `json:"bgp,omitempty"`
}

func (req *PartnerInterconnectAttachmentCreateRequest) buildReq() *partnerInterconnectAttachmentRequestBody {
	request := &partnerInterconnectAttachmentRequestBody{
		Name:                      req.Name,
		ConnectionBandwidthInMbps: req.ConnectionBandwidthInMbps,
		Region:                    req.Region,
		NaaSProvider:              req.NaaSProvider,
		VPCIDs:                    req.VPCIDs,
	}

	if req.BGP != (BGP{}) {
		request.BGP = &req.BGP
	}

	return request
}

// PartnerInterconnectAttachmentUpdateRequest represents a request to update a Partner Interconnect Attachment.
type PartnerInterconnectAttachmentUpdateRequest struct {
	// Name is the name of the Partner Interconnect Attachment
	Name string `json:"name,omitempty"`
	//VPCIDs is the IDs of the VPCs to which the Partner Interconnect Attachment is connected
	VPCIDs []string `json:"vpc_ids,omitempty"`
}

type PartnerInterconnectAttachmentSetRoutesRequest struct {
	// Routes is the list of routes to be used for the Partner Interconnect Attachment
	Routes []string `json:"routes,omitempty"`
}

// BGP represents the BGP configuration of a Partner Interconnect Attachment.
type BGP struct {
	// LocalASN is the local ASN
	LocalASN int `json:"local_asn,omitempty"`
	// LocalRouterIP is the local router IP
	LocalRouterIP string `json:"local_router_ip,omitempty"`
	// PeerASN is the peer ASN
	PeerASN int `json:"peer_asn,omitempty"`
	// PeerRouterIP is the peer router IP
	PeerRouterIP string `json:"peer_router_ip,omitempty"`
}

// ServiceKey represents the service key of a Partner Interconnect Attachment.
type ServiceKey struct {
	ServiceKey string `json:"service_key,omitempty"`
}

// RemoteRoute represents a route for a Partner Interconnect Attachment.
type RemoteRoute struct {
	// ID is the generated ID of the Route
	ID string `json:"id,omitempty"`
	// Cidr is the CIDR of the route
	Cidr string `json:"cidr,omitempty"`
}

// PartnerInterconnectAttachment represents a DigitalOcean Partner Interconnect Attachment.
type PartnerInterconnectAttachment struct {
	// ID is the generated ID of the Partner Interconnect Attachment
	ID string `json:"id,omitempty"`
	// Name is the name of the Partner Interconnect Attachment
	Name string `json:"name,omitempty"`
	// State is the state of the Partner Interconnect Attachment
	State string `json:"state,omitempty"`
	// ConnectionBandwidthInMbps is the bandwidth of the connection in Mbps
	ConnectionBandwidthInMbps int `json:"connection_bandwidth_in_mbps,omitempty"`
	// Region is the region where the Partner Interconnect Attachment is created
	Region string `json:"region,omitempty"`
	// NaaSProvider is the name of the Network as a Service provider
	NaaSProvider string `json:"naas_provider,omitempty"`
	// VPCIDs is the IDs of the VPCs to which the Partner Interconnect Attachment is connected
	VPCIDs []string `json:"vpc_ids,omitempty"`
	// BGP is the BGP configuration of the Partner Interconnect Attachment
	BGP BGP `json:"bgp,omitempty"`
	// CreatedAt is time when this Partner Interconnect Attachment was first created
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type partnerInterconnectAttachmentRoot struct {
	PartnerInterconnectAttachment *PartnerInterconnectAttachment `json:"partner_interconnect_attachment"`
}

type partnerInterconnectAttachmentsRoot struct {
	PartnerInterconnectAttachments []*PartnerInterconnectAttachment `json:"partner_interconnect_attachments"`
	Links                          *Links                           `json:"links"`
	Meta                           *Meta                            `json:"meta"`
}

type serviceKeyRoot struct {
	ServiceKey *ServiceKey `json:"service_key"`
}

type remoteRoutesRoot struct {
	RemoteRoutes []*RemoteRoute `json:"remote_routes"`
	Links        *Links         `json:"links"`
	Meta         *Meta          `json:"meta"`
}

// List returns a list of all Partner Interconnect Attachments, with optional pagination.
func (s *PartnerInterconnectAttachmentsServiceOp) List(ctx context.Context, opt *ListOptions) ([]*PartnerInterconnectAttachment, *Response, error) {
	path, err := addOptions(partnerInterconnectAttachmentsBasePath, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(partnerInterconnectAttachmentsRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}
	return root.PartnerInterconnectAttachments, resp, nil
}

// Create creates a new Partner Interconnect Attachment.
func (s *PartnerInterconnectAttachmentsServiceOp) Create(ctx context.Context, create *PartnerInterconnectAttachmentCreateRequest) (*PartnerInterconnectAttachment, *Response, error) {
	path := partnerInterconnectAttachmentsBasePath

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, create.buildReq())
	if err != nil {
		return nil, nil, err
	}

	root := new(partnerInterconnectAttachmentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.PartnerInterconnectAttachment, resp, nil
}

// Get returns the details of a Partner Interconnect Attachment.
func (s *PartnerInterconnectAttachmentsServiceOp) Get(ctx context.Context, id string) (*PartnerInterconnectAttachment, *Response, error) {
	path := fmt.Sprintf("%s/%s", partnerInterconnectAttachmentsBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(partnerInterconnectAttachmentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.PartnerInterconnectAttachment, resp, nil
}

// Update updates a Partner Interconnect Attachment properties.
func (s *PartnerInterconnectAttachmentsServiceOp) Update(ctx context.Context, id string, update *PartnerInterconnectAttachmentUpdateRequest) (*PartnerInterconnectAttachment, *Response, error) {
	path := fmt.Sprintf("%s/%s", partnerInterconnectAttachmentsBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodPatch, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(partnerInterconnectAttachmentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.PartnerInterconnectAttachment, resp, nil
}

// Delete deletes a Partner Interconnect Attachment.
func (s *PartnerInterconnectAttachmentsServiceOp) Delete(ctx context.Context, id string) (*Response, error) {
	path := fmt.Sprintf("%s/%s", partnerInterconnectAttachmentsBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(ctx, req, nil)
	if err != nil {
		return resp, err
	}

	return resp, nil
}

func (s *PartnerInterconnectAttachmentsServiceOp) GetServiceKey(ctx context.Context, id string) (*ServiceKey, *Response, error) {
	path := fmt.Sprintf("%s/%s/service_key", partnerInterconnectAttachmentsBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(serviceKeyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.ServiceKey, resp, nil
}

// ListRoutes lists all routes for a Partner Interconnect Attachment.
func (s *PartnerInterconnectAttachmentsServiceOp) ListRoutes(ctx context.Context, id string, opt *ListOptions) ([]*RemoteRoute, *Response, error) {
	path, err := addOptions(fmt.Sprintf("%s/%s/remote_routes", partnerInterconnectAttachmentsBasePath, id), opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(remoteRoutesRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.RemoteRoutes, resp, nil
}

// SetRoutes  updates specific properties of a Partner Interconnect Attachment.
func (s *PartnerInterconnectAttachmentsServiceOp) SetRoutes(ctx context.Context, id string, set *PartnerInterconnectAttachmentSetRoutesRequest) (*PartnerInterconnectAttachment, *Response, error) {
	path := fmt.Sprintf("%s/%s/remote_routes", partnerInterconnectAttachmentsBasePath, id)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, set)
	if err != nil {
		return nil, nil, err
	}

	root := new(partnerInterconnectAttachmentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.PartnerInterconnectAttachment, resp, nil
}
