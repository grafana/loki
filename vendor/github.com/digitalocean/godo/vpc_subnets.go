package godo

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// VPCSubnet represents a subnet within a VPC.
type VPCSubnet struct {
	ID        string    `json:"id,omitempty"`
	VPCID     string    `json:"vpc_id,omitempty"`
	Name      string    `json:"name,omitempty"`
	IPRange   string    `json:"ip_range,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	Default   bool      `json:"default,omitempty"`
	Type      string    `json:"type,omitempty"`
	URN       string    `json:"urn,omitempty"`
	Region    string    `json:"region,omitempty"`
}

// VPCSubnetCreateRequest represents a request to create a subnet within a VPC.
type VPCSubnetCreateRequest struct {
	Name    string `json:"name"`
	IPRange string `json:"ip_range,omitempty"`
}

// VPCSubnetUpdateRequest represents a request to update a subnet.
type VPCSubnetUpdateRequest struct {
	Name string `json:"name,omitempty"`
}

type vpcSubnetRoot struct {
	Subnet *VPCSubnet `json:"vpc_subnet"`
}

type vpcSubnetsRoot struct {
	Subnets []*VPCSubnet `json:"vpc_subnets"`
	Links   *Links       `json:"links"`
	Meta    *Meta        `json:"meta"`
}

func vpcSubnetsBasePath(vpcID string) string {
	return fmt.Sprintf("%s/%s/subnets", vpcsBasePath, vpcID)
}

func vpcSubnetPath(vpcID, subnetID string) string {
	return fmt.Sprintf("%s/%s/subnets/%s", vpcsBasePath, vpcID, subnetID)
}

// CreateSubnet creates a new subnet within a VPC.
func (v *VPCsServiceOp) CreateSubnet(ctx context.Context, vpcID string, create *VPCSubnetCreateRequest) (*VPCSubnet, *Response, error) {
	path := vpcSubnetsBasePath(vpcID)
	req, err := v.client.NewRequest(ctx, http.MethodPost, path, create)
	if err != nil {
		return nil, nil, err
	}

	root := new(vpcSubnetRoot)
	resp, err := v.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Subnet, resp, nil
}

// GetSubnet retrieves a specific subnet by ID.
func (v *VPCsServiceOp) GetSubnet(ctx context.Context, vpcID string, subnetID string) (*VPCSubnet, *Response, error) {
	path := vpcSubnetPath(vpcID, subnetID)
	req, err := v.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(vpcSubnetRoot)
	resp, err := v.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Subnet, resp, nil
}

// ListSubnets lists all subnets within a VPC.
func (v *VPCsServiceOp) ListSubnets(ctx context.Context, vpcID string, opt *ListOptions) ([]*VPCSubnet, *Response, error) {
	path, err := addOptions(vpcSubnetsBasePath(vpcID), opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := v.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(vpcSubnetsRoot)
	resp, err := v.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.Subnets, resp, nil
}

// UpdateSubnet updates a subnet's properties.
func (v *VPCsServiceOp) UpdateSubnet(ctx context.Context, vpcID string, subnetID string, update *VPCSubnetUpdateRequest) (*VPCSubnet, *Response, error) {
	path := vpcSubnetPath(vpcID, subnetID)
	req, err := v.client.NewRequest(ctx, http.MethodPatch, path, update)
	if err != nil {
		return nil, nil, err
	}

	root := new(vpcSubnetRoot)
	resp, err := v.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Subnet, resp, nil
}

// DeleteSubnet deletes a subnet from a VPC.
func (v *VPCsServiceOp) DeleteSubnet(ctx context.Context, vpcID string, subnetID string) (*Response, error) {
	path := vpcSubnetPath(vpcID, subnetID)
	req, err := v.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := v.client.Do(ctx, req, nil)
	if err != nil {
		return resp, err
	}

	return resp, nil
}
