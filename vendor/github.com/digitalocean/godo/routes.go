package godo

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// Route types assigned by the server.
const (
	RouteTypeStatic  = "STATIC"
	RouteTypeDynamic = "DYNAMIC"
)

// RoutesService is an interface for managing VPC and subnet scoped routes with the
// DigitalOcean API.
// See: https://docs.digitalocean.com/reference/api/api-reference/#tag/Routes
type RoutesService interface {
	ListVPCRoutes(context.Context, string, *ListOptions) ([]*Route, *Response, error)
	ListSubnetRoutes(context.Context, string, string, *ListOptions) ([]*Route, *Response, error)
	CreateSubnetRoute(context.Context, string, string, *RouteCreateRequest) (*Route, *Response, error)
	UpdateSubnetRoute(context.Context, string, string, string, *RouteUpdateRequest) (*Route, *Response, error)
	DeleteSubnetRoute(context.Context, string, string, string) (*Response, error)
}

var _ RoutesService = &RoutesServiceOp{}

// RoutesServiceOp interfaces with Routes endpoints in the DigitalOcean API.
type RoutesServiceOp struct {
	client *Client
}

// Route represents a DigitalOcean VPC or subnet-scoped route.
type Route struct {
	ID              string    `json:"id,omitempty"`
	Type            string    `json:"type,omitempty"`
	DestinationCIDR string    `json:"destination_cidr,omitempty"`
	TargetURNs      []string  `json:"target_urns,omitempty"`
	Modifiable      bool      `json:"modifiable,omitempty"`
	CreatedAt       time.Time `json:"created_at,omitempty"`
}

// RouteCreateRequest represents a request to create a subnet-scoped static route.
type RouteCreateRequest struct {
	DestinationCIDR string   `json:"destination_cidr"`
	TargetURNs      []string `json:"target_urns"`
}

// RouteUpdateRequest represents a request to update a subnet-scoped static route.
type RouteUpdateRequest struct {
	TargetURNs []string `json:"target_urns"`
}

type routeRoot struct {
	Route *Route `json:"route"`
}

type routesRoot struct {
	Routes []*Route `json:"routes"`
	Links  *Links   `json:"links"`
	Meta   *Meta    `json:"meta"`
}

func vpcRoutesPath(vpcUUID string) string {
	return fmt.Sprintf("%s/%s/routes", vpcsBasePath, vpcUUID)
}

func subnetRoutesPath(vpcUUID, subnetUUID string) string {
	return fmt.Sprintf("%s/%s/subnets/%s/routes", vpcsBasePath, vpcUUID, subnetUUID)
}

func subnetRoutePath(vpcUUID, subnetUUID, routeUUID string) string {
	return fmt.Sprintf("%s/%s/subnets/%s/routes/%s", vpcsBasePath, vpcUUID, subnetUUID, routeUUID)
}

// ListVPCRoutes lists VPC-scoped routes. Entries are read-only in the public API.
func (r *RoutesServiceOp) ListVPCRoutes(ctx context.Context, vpcUUID string, opt *ListOptions) ([]*Route, *Response, error) {
	return r.list(ctx, vpcRoutesPath(vpcUUID), opt)
}

// ListSubnetRoutes lists routes for a subnet, including VPC-scoped routes
// propagated into the subnet's effective routing.
func (r *RoutesServiceOp) ListSubnetRoutes(ctx context.Context, vpcUUID, subnetUUID string, opt *ListOptions) ([]*Route, *Response, error) {
	return r.list(ctx, subnetRoutesPath(vpcUUID, subnetUUID), opt)
}

func (r *RoutesServiceOp) list(ctx context.Context, path string, opt *ListOptions) ([]*Route, *Response, error) {
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := r.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(routesRoot)
	resp, err := r.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return root.Routes, resp, nil
}

// CreateSubnetRoute creates a customer-managed static route for a subnet.
func (r *RoutesServiceOp) CreateSubnetRoute(ctx context.Context, vpcUUID, subnetUUID string, create *RouteCreateRequest) (*Route, *Response, error) {
	req, err := r.client.NewRequest(ctx, http.MethodPost, subnetRoutesPath(vpcUUID, subnetUUID), create)
	if err != nil {
		return nil, nil, err
	}

	root := new(routeRoot)
	resp, err := r.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Route, resp, nil
}

// UpdateSubnetRoute updates a customer-managed static route for a subnet.
func (r *RoutesServiceOp) UpdateSubnetRoute(ctx context.Context, vpcUUID, subnetUUID, routeUUID string, update *RouteUpdateRequest) (*Route, *Response, error) {
	req, err := r.client.NewRequest(ctx, http.MethodPatch, subnetRoutePath(vpcUUID, subnetUUID, routeUUID), update)
	if err != nil {
		return nil, nil, err
	}

	root := new(routeRoot)
	resp, err := r.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Route, resp, nil
}

// DeleteSubnetRoute deletes a customer-managed static route for a subnet.
func (r *RoutesServiceOp) DeleteSubnetRoute(ctx context.Context, vpcUUID, subnetUUID, routeUUID string) (*Response, error) {
	req, err := r.client.NewRequest(ctx, http.MethodDelete, subnetRoutePath(vpcUUID, subnetUUID, routeUUID), nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.client.Do(ctx, req, nil)
	if err != nil {
		return resp, err
	}

	return resp, nil
}
