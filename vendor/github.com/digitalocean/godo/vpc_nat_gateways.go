package godo

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	vpcNatGatewaysBasePath = "/v2/vpc_nat_gateways"
)

// VPCNATGatewaysService defines an interface for managing VPC NAT Gateways through the DigitalOcean API
type VPCNATGatewaysService interface {
	Create(context.Context, *VPCNATGatewayRequest) (*VPCNATGateway, *Response, error)
	Get(context.Context, string) (*VPCNATGateway, *Response, error)
	List(context.Context, *VPCNATGatewaysListOptions) ([]*VPCNATGateway, *Response, error)
	Update(context.Context, string, *VPCNATGatewayRequest) (*VPCNATGateway, *Response, error)
	Delete(context.Context, string) (*Response, error)
}

// VPCNATGatewayRequest represents a DigitalOcean VPC NAT Gateway create/update request
type VPCNATGatewayRequest struct {
	Name               string        `json:"name"`
	Type               string        `json:"type"`
	Region             string        `json:"region"`
	Size               uint32        `json:"size"`
	VPCs               []*IngressVPC `json:"vpcs"`
	UDPTimeoutSeconds  uint32        `json:"udp_timeout_seconds,omitempty"`
	ICMPTimeoutSeconds uint32        `json:"icmp_timeout_seconds,omitempty"`
	TCPTimeoutSeconds  uint32        `json:"tcp_timeout_seconds,omitempty"`
}

// VPCNATGateway represents a DigitalOcean VPC NAT Gateway resource
type VPCNATGateway struct {
	ID                 string        `json:"id"`
	Name               string        `json:"name"`
	Type               string        `json:"type"`
	State              string        `json:"state"`
	Region             string        `json:"region"`
	Size               uint32        `json:"size"`
	VPCs               []*IngressVPC `json:"vpcs"`
	Egresses           *Egresses     `json:"egresses,omitempty"`
	UDPTimeoutSeconds  uint32        `json:"udp_timeout_seconds,omitempty"`
	ICMPTimeoutSeconds uint32        `json:"icmp_timeout_seconds,omitempty"`
	TCPTimeoutSeconds  uint32        `json:"tcp_timeout_seconds,omitempty"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

// IngressVPC defines the ingress configs supported by a VPC NAT Gateway
type IngressVPC struct {
	VpcUUID        string `json:"vpc_uuid"`
	GatewayIP      string `json:"gateway_ip,omitempty"`
	DefaultGateway bool   `json:"default_gateway,omitempty"`
}

// Egresses define egress routes supported by a VPC NAT Gateway
type Egresses struct {
	PublicGateways []*PublicGateway `json:"public_gateways,omitempty"`
}

// PublicGateway defines the public egress supported by a VPC NAT Gateway
type PublicGateway struct {
	IPv4 string `json:"ipv4"`
}

// VPCNATGatewaysListOptions define custom options for listing VPC NAT Gateways
type VPCNATGatewaysListOptions struct {
	ListOptions
	State  []string `url:"state,omitempty"`
	Region []string `url:"region,omitempty"`
	Type   []string `url:"type,omitempty"`
	Name   []string `url:"name,omitempty"`
}

type vpcNatGatewayRoot struct {
	VPCNATGateway *VPCNATGateway `json:"vpc_nat_gateway"`
}

type vpcNatGatewaysRoot struct {
	VPCNATGateways []*VPCNATGateway `json:"vpc_nat_gateways"`
	Links          *Links           `json:"links"`
	Meta           *Meta            `json:"meta"`
}

// VPCNATGatewaysServiceOp handles communication with VPC NAT Gateway methods of the DigitalOcean API
type VPCNATGatewaysServiceOp struct {
	client *Client
}

var _ VPCNATGatewaysService = &VPCNATGatewaysServiceOp{}

// Create a new VPC NAT Gateway
func (n *VPCNATGatewaysServiceOp) Create(ctx context.Context, createReq *VPCNATGatewayRequest) (*VPCNATGateway, *Response, error) {
	req, err := n.client.NewRequest(ctx, http.MethodPost, vpcNatGatewaysBasePath, createReq)
	if err != nil {
		return nil, nil, err
	}
	root := new(vpcNatGatewayRoot)
	resp, err := n.client.Do(ctx, req, root)
	if err != nil {
		return nil, nil, err
	}
	return root.VPCNATGateway, resp, nil
}

// Get an existing VPC NAT Gateway
func (n *VPCNATGatewaysServiceOp) Get(ctx context.Context, id string) (*VPCNATGateway, *Response, error) {
	req, err := n.client.NewRequest(ctx, http.MethodGet, fmt.Sprintf("%s/%s", vpcNatGatewaysBasePath, id), nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(vpcNatGatewayRoot)
	resp, err := n.client.Do(ctx, req, root)
	if err != nil {
		return nil, nil, err
	}
	return root.VPCNATGateway, resp, nil
}

// List all active VPC NAT Gateways
func (n *VPCNATGatewaysServiceOp) List(ctx context.Context, opts *VPCNATGatewaysListOptions) ([]*VPCNATGateway, *Response, error) {
	path, err := addOptions(vpcNatGatewaysBasePath, opts)
	if err != nil {
		return nil, nil, err
	}
	req, err := n.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(vpcNatGatewaysRoot)
	resp, err := n.client.Do(ctx, req, root)
	if err != nil {
		return nil, nil, err
	}
	if root.Links != nil {
		resp.Links = root.Links
	}
	if root.Meta != nil {
		resp.Meta = root.Meta
	}
	return root.VPCNATGateways, resp, nil
}

// Update an existing VPC NAT Gateway
func (n *VPCNATGatewaysServiceOp) Update(ctx context.Context, id string, updateReq *VPCNATGatewayRequest) (*VPCNATGateway, *Response, error) {
	req, err := n.client.NewRequest(ctx, http.MethodPut, fmt.Sprintf("%s/%s", vpcNatGatewaysBasePath, id), updateReq)
	if err != nil {
		return nil, nil, err
	}
	root := new(vpcNatGatewayRoot)
	resp, err := n.client.Do(ctx, req, root)
	if err != nil {
		return nil, nil, err
	}
	return root.VPCNATGateway, resp, nil
}

// Delete an existing VPC NAT Gateway
func (n *VPCNATGatewaysServiceOp) Delete(ctx context.Context, id string) (*Response, error) {
	req, err := n.client.NewRequest(ctx, http.MethodDelete, fmt.Sprintf("%s/%s", vpcNatGatewaysBasePath, id), nil)
	if err != nil {
		return nil, err
	}
	return n.client.Do(ctx, req, nil)
}
