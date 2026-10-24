package godo

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// ActionGatewaySessionsService manages control-plane session records.
type ActionGatewaySessionsService struct{ client *Client }

// ActionGatewaySessionPolicy controls tool permissions.
type ActionGatewaySessionPolicy struct {
	DefaultAction string                           `json:"defaultAction,omitempty"`
	Rules         []ActionGatewaySessionPolicyRule `json:"rules,omitempty"`
}

// ActionGatewaySessionPolicyRule selects a tool and allowed argument matches.
type ActionGatewaySessionPolicyRule struct {
	Tool   string            `json:"tool"`
	Match  map[string]string `json:"match,omitempty"`
	Action string            `json:"action"`
}

// ActionGatewaySessionNetwork places a session on a VPC.
type ActionGatewaySessionNetwork struct {
	VPCUUID string `json:"vpcUuid"`
}

// ActionGatewaySessionInsights controls session telemetry.
type ActionGatewaySessionInsights struct {
	Metrics *bool `json:"metrics,omitempty"`
	Logs    *bool `json:"logs,omitempty"`
	Traces  *bool `json:"traces,omitempty"`
}

// ActionGatewaySessionToolReference is a pinned tool or toolbelt reference.
type ActionGatewaySessionToolReference struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ActionGatewaySessionRecord is the public control-plane session representation.
type ActionGatewaySessionRecord struct {
	SessionURN string                      `json:"sessionUrn"`
	Name       string                      `json:"name"`
	ActorID    string                      `json:"actorId"`
	Policy     *ActionGatewaySessionPolicy `json:"policy"`
	Tools      *struct {
		References []ActionGatewaySessionToolReference `json:"references"`
	} `json:"tools"`
	Config              json.RawMessage               `json:"config"`
	Network             *ActionGatewaySessionNetwork  `json:"network"`
	Insights            *ActionGatewaySessionInsights `json:"insights"`
	AgentURN            string                        `json:"agentUrn"`
	AgentName           string                        `json:"agentName"`
	CreatedAt           *Timestamp                    `json:"createdAt"`
	UpdatedAt           *Timestamp                    `json:"updatedAt"`
	OwningUserID        string                        `json:"owning_user_id"`
	OwningUserNumericID string                        `json:"owning_user_numeric_id"`
}

// ActionGatewaySessionCreateRequest is the wire request for creating a session.
// Nil Tools enables all tools; a non-nil empty slice enables none.
type ActionGatewaySessionCreateRequest struct {
	Name     string                        `json:"name"`
	ActorID  string                        `json:"actorId,omitempty"`
	Policy   *ActionGatewaySessionPolicy   `json:"policy,omitempty"`
	Tools    []string                      `json:"tools,omitempty"`
	Config   json.RawMessage               `json:"config,omitempty"`
	Network  *ActionGatewaySessionNetwork  `json:"network,omitempty"`
	Insights *ActionGatewaySessionInsights `json:"insights,omitempty"`
}

// MarshalJSON preserves an explicitly empty tools selection on create.
func (body ActionGatewaySessionCreateRequest) MarshalJSON() ([]byte, error) {
	type wire ActionGatewaySessionCreateRequest
	encoded, err := json.Marshal(wire(body))
	if err != nil || body.Tools == nil {
		return encoded, err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	fields["tools"], err = json.Marshal(body.Tools)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

// ActionGatewaySessionCreateResponse contains the new session and pinned MCP URL.
type ActionGatewaySessionCreateResponse struct {
	Session *ActionGatewaySessionRecord `json:"session"`
	MCPURL  string                      `json:"mcpUrl"`
	Tools   []string                    `json:"tools"`
}

// ActionGatewaySessionsResponse contains offset-paginated sessions.
type ActionGatewaySessionsResponse struct {
	Sessions   []ActionGatewaySessionRecord `json:"sessions"`
	Pagination *ActionGatewayPagination     `json:"pagination"`
}

// ActionGatewaySearchSessionsResponse contains cursor-paginated sessions.
type ActionGatewaySearchSessionsResponse struct {
	Sessions      []ActionGatewaySessionRecord `json:"sessions"`
	NextPageToken string                       `json:"next_page_token"`
}

// ActionGatewaySessionListOptions filters sessions by end user.
type ActionGatewaySessionListOptions struct {
	EndUserID string `url:"end_user_id,omitempty"`
	Page      int    `url:"page,omitempty"`
	PerPage   int    `url:"per_page,omitempty"`
}

// ActionGatewaySessionSearchOptions searches sessions by query and user.
type ActionGatewaySessionSearchOptions struct {
	Query     string `url:"query,omitempty"`
	EndUserID string `url:"end_user_id,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
	PageToken string `url:"page_token,omitempty"`
}

// Create creates a control-plane session.
func (s *ActionGatewaySessionsService) Create(ctx context.Context, body *ActionGatewaySessionCreateRequest) (*ActionGatewaySessionCreateResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	return actionGatewayRequest[ActionGatewaySessionCreateResponse](ctx, s.client, http.MethodPost, actionGatewayPath+"/sessions", nil, body)
}

// List lists sessions.
func (s *ActionGatewaySessionsService) List(ctx context.Context, opt *ActionGatewaySessionListOptions) (*ActionGatewaySessionsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewaySessionsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/sessions", opt, nil)
}

// Search searches sessions using cursor pagination.
func (s *ActionGatewaySessionsService) Search(ctx context.Context, opt *ActionGatewaySessionSearchOptions) (*ActionGatewaySearchSessionsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewaySearchSessionsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/sessions/search", opt, nil)
}

// Delete deletes a session by URN.
func (s *ActionGatewaySessionsService) Delete(ctx context.Context, urn string) (*Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/sessions", actionGatewayPathPart{"urn", urn})
	if err != nil {
		return nil, err
	}
	_, response, err := actionGatewayRequest[struct{}](ctx, s.client, http.MethodDelete, path, nil, nil)
	return response, err
}

// CreateRuntime creates and binds a session to its MCP endpoint. It supplies a
// generated name and an ask-by-default policy when those are omitted.
func (s *ActionGatewaySessionsService) CreateRuntime(ctx context.Context, body *ActionGatewaySessionCreateRequest) (*ActionGatewaySession, *Response, error) {
	if body == nil || strings.TrimSpace(body.ActorID) == "" {
		return nil, nil, NewArgError("actorID", "cannot be empty")
	}
	copy := *body
	copy.ActorID = strings.TrimSpace(body.ActorID)
	if copy.Name == "" {
		copy.Name = "godo-session-" + time.Now().UTC().Format("20060102-150405.000000000")
	}
	if copy.Policy == nil {
		copy.Policy = &ActionGatewaySessionPolicy{DefaultAction: "ask"}
	}
	created, response, err := s.Create(ctx, &copy)
	if err != nil {
		return nil, response, err
	}
	session, err := newActionGatewaySession(s.client, created, copy.ActorID)
	return session, response, err
}
