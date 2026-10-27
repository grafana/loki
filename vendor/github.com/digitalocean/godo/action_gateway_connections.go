package godo

import (
	"context"
	"encoding/json"
	"net/http"
)

// ActionGatewayConnectionsService manages provider connections for end users.
type ActionGatewayConnectionsService struct{ client *Client }

// ActionGatewayConnectionDestination permits a private endpoint in a VPC.
type ActionGatewayConnectionDestination struct {
	Host           string   `json:"host"`
	Port           uint32   `json:"port"`
	AllowedIPCIDRs []string `json:"allowed_ip_cidrs"`
}

// ActionGatewayConnectionVPC describes an existing VPC connection.
type ActionGatewayConnectionVPC struct {
	VPCUUID      string                               `json:"vpc_uuid"`
	Region       string                               `json:"region,omitempty"`
	Destinations []ActionGatewayConnectionDestination `json:"destinations,omitempty"`
}

// ActionGatewayConnectionNetwork identifies the VPC and destinations.
type ActionGatewayConnectionNetwork struct {
	VPC *ActionGatewayConnectionVPC `json:"vpc"`
}

// ActionGatewayConnectionCreateVPC selects a VPC on create.
type ActionGatewayConnectionCreateVPC struct {
	VPCUUID      string                               `json:"vpc_uuid"`
	Destinations []ActionGatewayConnectionDestination `json:"destinations,omitempty"`
}

// ActionGatewayConnectionCreateNetwork selects a VPC without output-only region.
type ActionGatewayConnectionCreateNetwork struct {
	VPC *ActionGatewayConnectionCreateVPC `json:"vpc"`
}

// ActionGatewayTeamCredential references a previously registered team credential.
type ActionGatewayTeamCredential struct {
	CredentialID string `json:"credential_id"`
}

// ActionGatewayConnectionCredential selects exactly one authorization mode.
type ActionGatewayConnectionCredential struct {
	DigitalOceanOAuth *struct{}                    `json:"digitalocean_oauth,omitempty"`
	TeamCredential    *ActionGatewayTeamCredential `json:"team_credential,omitempty"`
}

// ActionGatewayConnectionAuthorization describes an incomplete authorization.
type ActionGatewayConnectionAuthorization struct {
	Status           string     `json:"status"`
	ConnectURL       string     `json:"connect_url"`
	VerificationCode string     `json:"verification_code"`
	ExpiresAt        *Timestamp `json:"expires_at"`
}

// ActionGatewayConnection describes a user's provider authorization.
type ActionGatewayConnection struct {
	ID                   string                          `json:"id"`
	Provider             string                          `json:"provider"`
	ProviderDisplayName  string                          `json:"provider_display_name"`
	UserID               string                          `json:"user_id"`
	Status               string                          `json:"status"`
	CreatedAt            *Timestamp                      `json:"created_at"`
	UpdatedAt            *Timestamp                      `json:"updated_at"`
	RevokedAt            *Timestamp                      `json:"revoked_at"`
	ConnectionParameters map[string]interface{}          `json:"connection_parameters"`
	CredentialKind       string                          `json:"credential_kind"`
	CredentialID         string                          `json:"credential_id"`
	Network              *ActionGatewayConnectionNetwork `json:"network"`
	OAuth                *struct {
		Scopes    []string   `json:"scopes"`
		GrantedAt *Timestamp `json:"granted_at"`
	} `json:"oauth"`
	APIKey              json.RawMessage `json:"api_key"`
	Scopes              []string        `json:"scopes"`
	GrantedAt           *Timestamp      `json:"granted_at"`
	OwningUserID        string          `json:"owning_user_id"`
	OwningUserNumericID string          `json:"owning_user_numeric_id"`
}

// ActionGatewayConnectionCreateRequest creates a provider connection.
type ActionGatewayConnectionCreateRequest struct {
	Provider             string                                `json:"provider"`
	UserID               string                                `json:"user_id"`
	Scopes               []string                              `json:"scopes,omitempty"`
	ConnectionParameters map[string]interface{}                `json:"connection_parameters,omitempty"`
	Credential           *ActionGatewayConnectionCredential    `json:"credential,omitempty"`
	Network              *ActionGatewayConnectionCreateNetwork `json:"network,omitempty"`
}

// ActionGatewayConnectionResponse contains one connection and any pending authorization.
type ActionGatewayConnectionResponse struct {
	Connection    *ActionGatewayConnection              `json:"connection"`
	Authorization *ActionGatewayConnectionAuthorization `json:"authorization"`
}

// ActionGatewayConnectionsResponse contains offset-paginated connections.
type ActionGatewayConnectionsResponse struct {
	Connections []ActionGatewayConnection `json:"connections"`
	Pagination  *ActionGatewayPagination  `json:"pagination"`
}

// ActionGatewayConnectionListOptions filters connections.
type ActionGatewayConnectionListOptions struct {
	Provider      string `url:"provider,omitempty"`
	UserID        string `url:"user_id,omitempty"`
	Status        string `url:"status,omitempty"`
	Sort          string `url:"sort,omitempty"`
	SortDirection string `url:"sort_direction,omitempty"`
	Page          int    `url:"page,omitempty"`
	PerPage       int    `url:"per_page,omitempty"`
}

// Create creates a connection.
func (s *ActionGatewayConnectionsService) Create(ctx context.Context, body *ActionGatewayConnectionCreateRequest) (*ActionGatewayConnectionResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	return actionGatewayRequest[ActionGatewayConnectionResponse](ctx, s.client, http.MethodPost, actionGatewayPath+"/connections", nil, body)
}

// List lists connections.
func (s *ActionGatewayConnectionsService) List(ctx context.Context, opt *ActionGatewayConnectionListOptions) (*ActionGatewayConnectionsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayConnectionsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/connections", opt, nil)
}

// Get retrieves one connection.
func (s *ActionGatewayConnectionsService) Get(ctx context.Context, id string) (*ActionGatewayConnectionResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/connections", actionGatewayPathPart{"id", id})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayConnectionResponse](ctx, s.client, http.MethodGet, path, nil, nil)
}

// Delete revokes and deletes a connection.
func (s *ActionGatewayConnectionsService) Delete(ctx context.Context, id string) (*ActionGatewayConnectionResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/connections", actionGatewayPathPart{"id", id})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayConnectionResponse](ctx, s.client, http.MethodDelete, path, nil, nil)
}
