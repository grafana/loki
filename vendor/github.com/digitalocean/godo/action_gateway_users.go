package godo

import (
	"context"
	"net/http"
)

// ActionGatewayUsersService exposes end-user summaries.
type ActionGatewayUsersService struct{ client *Client }

// ActionGatewayUserSession is a session associated with an end user.
type ActionGatewayUserSession struct {
	SessionURN string     `json:"session_urn"`
	Name       string     `json:"name"`
	CreatedAt  *Timestamp `json:"created_at"`
	UpdatedAt  *Timestamp `json:"updated_at"`
}

// ActionGatewayUserConnection is a connection associated with an end user.
type ActionGatewayUserConnection struct {
	ID                   string                          `json:"id"`
	Provider             string                          `json:"provider"`
	ProviderDisplayName  string                          `json:"provider_display_name"`
	UserID               string                          `json:"user_id"`
	Scopes               []string                        `json:"scopes"`
	Status               string                          `json:"status"`
	GrantedAt            *Timestamp                      `json:"granted_at"`
	RevokedAt            *Timestamp                      `json:"revoked_at"`
	CreatedAt            *Timestamp                      `json:"created_at"`
	UpdatedAt            *Timestamp                      `json:"updated_at"`
	ConnectionParameters map[string]interface{}          `json:"connection_parameters"`
	CredentialKind       string                          `json:"credential_kind"`
	CredentialID         string                          `json:"credential_id"`
	Network              *ActionGatewayConnectionNetwork `json:"network"`
	OwningUserID         string                          `json:"owning_user_id"`
	OwningUserNumericID  string                          `json:"owning_user_numeric_id"`
}

// ActionGatewayUser is a derived end-user summary.
type ActionGatewayUser struct {
	UserID      string                        `json:"user_id"`
	Sessions    []ActionGatewayUserSession    `json:"sessions"`
	Connections []ActionGatewayUserConnection `json:"connections"`
}

// ActionGatewayUsersResponse contains offset-paginated end-user IDs.
type ActionGatewayUsersResponse struct {
	UserIDs    []string                 `json:"user_ids"`
	Pagination *ActionGatewayPagination `json:"pagination"`
}

// ActionGatewayUserResponse contains one end-user summary.
type ActionGatewayUserResponse struct {
	User *ActionGatewayUser `json:"user"`
}

// ActionGatewayUsersListOptions filters end users.
type ActionGatewayUsersListOptions struct {
	UserID        string `url:"user_id,omitempty"`
	Sort          string `url:"sort,omitempty"`
	SortDirection string `url:"sort_direction,omitempty"`
	Page          int    `url:"page,omitempty"`
	PerPage       int    `url:"per_page,omitempty"`
}

// List lists end-user IDs.
func (s *ActionGatewayUsersService) List(ctx context.Context, opt *ActionGatewayUsersListOptions) (*ActionGatewayUsersResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayUsersResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/users", opt, nil)
}

// Get retrieves an end-user summary.
func (s *ActionGatewayUsersService) Get(ctx context.Context, userID string) (*ActionGatewayUserResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/users", actionGatewayPathPart{"userID", userID})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayUserResponse](ctx, s.client, http.MethodGet, path, nil, nil)
}

// ActionGatewayActorLimitsService manages end-user rate-limit overrides.
type ActionGatewayActorLimitsService struct{ client *Client }

// ActionGatewayLimitOverride changes one quota category.
type ActionGatewayLimitOverride struct {
	Category          string `json:"category"`
	RequestsPerMinute string `json:"requests_per_minute"`
}

// ActionGatewayToolQuotas lists effective per-minute limits.
type ActionGatewayToolQuotas struct {
	WebSearchRequestsPerMinute          string `json:"web_search_requests_per_minute"`
	WebFetchRequestsPerMinute           string `json:"web_fetch_requests_per_minute"`
	NativeToolCallsRequestsPerMinute    string `json:"native_tool_calls_requests_per_minute"`
	NonNativeToolCallsRequestsPerMinute string `json:"non_native_tool_calls_requests_per_minute"`
}

// ActionGatewayActorLimitsResponse contains overrides and effective quotas.
type ActionGatewayActorLimitsResponse struct {
	ConfiguredLimits []ActionGatewayLimitOverride `json:"configured_limits"`
	EffectiveLimits  *ActionGatewayToolQuotas     `json:"effective_limits"`
}

// ActionGatewaySetLimitsRequest supplies one or more overrides.
type ActionGatewaySetLimitsRequest struct {
	Overrides []ActionGatewayLimitOverride `json:"overrides"`
}

// ActionGatewayClearLimitsRequest selects categories to clear.
type ActionGatewayClearLimitsRequest struct {
	Categories []string `json:"categories"`
}

// Get retrieves an actor's configured and effective limits.
func (s *ActionGatewayActorLimitsService) Get(ctx context.Context, actorID string) (*ActionGatewayActorLimitsResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/actors", actionGatewayPathPart{"actorID", actorID})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayActorLimitsResponse](ctx, s.client, http.MethodGet, path+"/limits", nil, nil)
}

// Set changes selected limit categories using the public limits:set action.
func (s *ActionGatewayActorLimitsService) Set(ctx context.Context, actorID string, body *ActionGatewaySetLimitsRequest) (*Response, error) {
	if body == nil {
		return nil, NewArgError("body", "cannot be nil")
	}
	path, err := actionGatewayItem(actionGatewayPath+"/actors", actionGatewayPathPart{"actorID", actorID})
	if err != nil {
		return nil, err
	}
	_, response, err := actionGatewayRequest[struct{}](ctx, s.client, http.MethodPost, path+"/limits:set", nil, body)
	return response, err
}

// Clear removes selected overrides using the public limits:clear action.
func (s *ActionGatewayActorLimitsService) Clear(ctx context.Context, actorID string, body *ActionGatewayClearLimitsRequest) (*Response, error) {
	if body == nil {
		return nil, NewArgError("body", "cannot be nil")
	}
	path, err := actionGatewayItem(actionGatewayPath+"/actors", actionGatewayPathPart{"actorID", actorID})
	if err != nil {
		return nil, err
	}
	_, response, err := actionGatewayRequest[struct{}](ctx, s.client, http.MethodPost, path+"/limits:clear", nil, body)
	return response, err
}
