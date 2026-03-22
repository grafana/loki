package godo

import (
	"context"
	"fmt"
	"net/http"
)

// ReservedIPActionsService is an interface for interfacing with the
// reserved IPs actions endpoints of the Digital Ocean API.
// See: https://docs.digitalocean.com/reference/api/api-reference/#tag/Reserved-IP-Actions
type ReservedIPV6ActionsService interface {
	Assign(ctx context.Context, ip string, dropletID int) (*Action, *Response, error)
	Unassign(ctx context.Context, ip string) (*Action, *Response, error)
}

// ReservedIPActionsServiceOp handles communication with the reserved IPs
// action related methods of the DigitalOcean API.
type ReservedIPV6ActionsServiceOp struct {
	client *Client
}

// Assign a reserved IP to a droplet.
func (s *ReservedIPV6ActionsServiceOp) Assign(ctx context.Context, ip string, dropletID int) (*Action, *Response, error) {
	request := &ActionRequest{
		"type":       "assign",
		"droplet_id": dropletID,
	}
	return s.doV6Action(ctx, ip, request)
}

// Unassign a rerserved IP from the droplet it is currently assigned to.
func (s *ReservedIPV6ActionsServiceOp) Unassign(ctx context.Context, ip string) (*Action, *Response, error) {
	request := &ActionRequest{"type": "unassign"}
	return s.doV6Action(ctx, ip, request)
}

func (s *ReservedIPV6ActionsServiceOp) doV6Action(ctx context.Context, ip string, request *ActionRequest) (*Action, *Response, error) {
	path := reservedIPV6ActionPath(ip)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, request)
	if err != nil {
		return nil, nil, err
	}

	root := new(actionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Event, resp, err
}

func reservedIPV6ActionPath(ip string) string {
	return fmt.Sprintf("%s/%s/actions", reservedIPV6sBasePath, ip)
}
