package godo

import (
	"context"
	"net/url"
	"strings"
)

const actionGatewayPath = "/v2/action-gateway"

// ActionGatewayService groups the public Action Gateway control-plane resources.
type ActionGatewayService struct {
	Tools       *ActionGatewayToolsService
	Toolbelts   *ActionGatewayToolbeltsService
	OutputViews *ActionGatewayOutputViewsService
	MCPServers  *ActionGatewayMCPServersService
	Connections *ActionGatewayConnectionsService
	Sessions    *ActionGatewaySessionsService
	Users       *ActionGatewayUsersService
	ActorLimits *ActionGatewayActorLimitsService
}

func newActionGatewayService(client *Client) *ActionGatewayService {
	return &ActionGatewayService{
		Tools:       &ActionGatewayToolsService{client},
		Toolbelts:   &ActionGatewayToolbeltsService{client},
		OutputViews: &ActionGatewayOutputViewsService{client},
		MCPServers:  &ActionGatewayMCPServersService{client},
		Connections: &ActionGatewayConnectionsService{client},
		Sessions:    &ActionGatewaySessionsService{client},
		Users:       &ActionGatewayUsersService{client},
		ActorLimits: &ActionGatewayActorLimitsService{client},
	}
}

func actionGatewayRequest[T any](ctx context.Context, client *Client, method, path string, options, body interface{}) (*T, *Response, error) {
	if options != nil {
		var err error
		path, err = addOptions(path, options)
		if err != nil {
			return nil, nil, err
		}
	}
	request, err := client.NewRequest(ctx, method, path, body)
	if err != nil {
		return nil, nil, err
	}
	result := new(T)
	response, err := client.Do(ctx, request, result)
	if err != nil {
		return nil, response, err
	}
	return result, response, nil
}

type actionGatewayPathPart struct {
	name  string
	value string
}

func actionGatewayItem(path string, part actionGatewayPathPart) (string, error) {
	if strings.TrimSpace(part.value) == "" {
		return "", NewArgError(part.name, "cannot be empty")
	}
	return path + "/" + url.PathEscape(part.value), nil
}

// ActionGatewayPagination describes offset-based list results.
type ActionGatewayPagination struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

// ActionGatewayListOptions selects an offset-based page.
type ActionGatewayListOptions struct {
	Page    int `url:"page,omitempty"`
	PerPage int `url:"per_page,omitempty"`
}

// ActionGatewayCursorOptions selects an opaque cursor-based page.
type ActionGatewayCursorOptions struct {
	PageSize  int    `url:"page_size,omitempty"`
	PageToken string `url:"page_token,omitempty"`
}
