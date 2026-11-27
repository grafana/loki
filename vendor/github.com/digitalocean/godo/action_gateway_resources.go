package godo

import (
	"context"
	"encoding/json"
	"net/http"
)

// ActionGatewayOutputViewsService manages tool output projections.
type ActionGatewayOutputViewsService struct{ client *Client }

// ActionGatewayOutputView is a saved or previewed output projection.
type ActionGatewayOutputView struct {
	ViewID       string          `json:"view_id"`
	ToolID       string          `json:"tool_id"`
	Tool         string          `json:"tool"`
	Version      string          `json:"version"`
	TeamID       string          `json:"team_id"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	Kind         string          `json:"kind"`
	Fields       []string        `json:"fields"`
	Transform    json.RawMessage `json:"transform"`
	OutputSchema json.RawMessage `json:"output_schema"`
	Audit        *struct {
		CreatedAt *Timestamp `json:"createdAt"`
		UpdatedAt *Timestamp `json:"updatedAt"`
		DeletedAt *Timestamp `json:"deletedAt"`
		CreatedBy string     `json:"createdBy"`
		UpdatedBy string     `json:"updatedBy"`
	} `json:"audit"`
	DeletedBy string `json:"deleted_by"`
}

// ActionGatewayOutputViewCreateRequest creates an output projection.
type ActionGatewayOutputViewCreateRequest struct {
	Tool        string   `json:"tool,omitempty"`
	ToolID      string   `json:"tool_id,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Fields      []string `json:"fields"`
}

// ActionGatewayOutputViewPreviewRequest previews an output projection without saving it.
type ActionGatewayOutputViewPreviewRequest struct {
	Tool        string   `json:"tool,omitempty"`
	ToolID      string   `json:"tool_id,omitempty"`
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Fields      []string `json:"fields"`
}

// ActionGatewayOutputViewResponse contains one view.
type ActionGatewayOutputViewResponse struct {
	View *ActionGatewayOutputView `json:"view"`
}

// ActionGatewayOutputViewsResponse contains cursor-paginated views.
type ActionGatewayOutputViewsResponse struct {
	Views         []ActionGatewayOutputView `json:"views"`
	ToolID        string                    `json:"tool_id"`
	Tool          string                    `json:"tool"`
	Version       string                    `json:"version"`
	NextPageToken string                    `json:"next_page_token"`
}

// ActionGatewayOutputViewListOptions filters and pages output views.
type ActionGatewayOutputViewListOptions struct {
	Tool      string `url:"tool,omitempty"`
	ToolID    string `url:"tool_id,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
	PageToken string `url:"page_token,omitempty"`
}

// Create saves an output view.
func (s *ActionGatewayOutputViewsService) Create(ctx context.Context, body *ActionGatewayOutputViewCreateRequest) (*ActionGatewayOutputViewResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	return actionGatewayRequest[ActionGatewayOutputViewResponse](ctx, s.client, http.MethodPost, actionGatewayPath+"/output-views", nil, body)
}

// Preview validates an output view without saving it.
func (s *ActionGatewayOutputViewsService) Preview(ctx context.Context, body *ActionGatewayOutputViewPreviewRequest) (*ActionGatewayOutputViewResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	return actionGatewayRequest[ActionGatewayOutputViewResponse](ctx, s.client, http.MethodPost, actionGatewayPath+"/output-views/preview", nil, body)
}

// List retrieves cursor-paginated output views.
func (s *ActionGatewayOutputViewsService) List(ctx context.Context, opt *ActionGatewayOutputViewListOptions) (*ActionGatewayOutputViewsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayOutputViewsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/output-views", opt, nil)
}

// Get retrieves an output view.
func (s *ActionGatewayOutputViewsService) Get(ctx context.Context, viewID string) (*ActionGatewayOutputViewResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/output-views", actionGatewayPathPart{"viewID", viewID})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayOutputViewResponse](ctx, s.client, http.MethodGet, path, nil, nil)
}

// Delete deletes an output view.
func (s *ActionGatewayOutputViewsService) Delete(ctx context.Context, viewID string) (*ActionGatewayOutputViewResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/output-views", actionGatewayPathPart{"viewID", viewID})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayOutputViewResponse](ctx, s.client, http.MethodDelete, path, nil, nil)
}

// ActionGatewayMCPServersService manages registered MCP servers.
type ActionGatewayMCPServersService struct{ client *Client }

// ActionGatewayMCPServer describes a registered MCP server.
type ActionGatewayMCPServer struct {
	ServerRef                    string   `json:"serverRef"`
	Endpoint                     string   `json:"endpoint"`
	Transport                    string   `json:"transport"`
	CredentialRefSource          string   `json:"credentialRefSource"`
	CredentialRef                string   `json:"credentialRef"`
	SyncStatus                   string   `json:"syncStatus"`
	SyncError                    string   `json:"syncError"`
	LastSyncedAt                 string   `json:"lastSyncedAt"`
	ProtocolVersion              string   `json:"protocolVersion"`
	ToolCount                    int      `json:"toolCount"`
	CreatedAt                    string   `json:"createdAt"`
	UpdatedAt                    string   `json:"updatedAt"`
	Description                  string   `json:"description"`
	OAuthAuthorizeURL            string   `json:"oauth_authorize_url"`
	OAuthScopes                  []string `json:"oauth_scopes"`
	OAuthAuthorizationTTLSeconds string   `json:"oauth_authorization_ttl_seconds"`
}

// ActionGatewayMCPServerCreateRequest registers an MCP server.
type ActionGatewayMCPServerCreateRequest struct {
	ServerRef                    string   `json:"serverRef"`
	Endpoint                     string   `json:"endpoint"`
	Transport                    string   `json:"transport,omitempty"`
	CredentialRefSource          string   `json:"credentialRefSource,omitempty"`
	Description                  string   `json:"description,omitempty"`
	CredentialRef                string   `json:"credentialRef,omitempty"`
	APIKey                       string   `json:"api_key,omitempty"`
	OAuthClientID                string   `json:"oauth_client_id,omitempty"`
	OAuthClientSecret            string   `json:"oauth_client_secret,omitempty"`
	OAuthAuthorizeURL            string   `json:"oauth_authorize_url,omitempty"`
	OAuthTokenURL                string   `json:"oauth_token_url,omitempty"`
	OAuthScopes                  []string `json:"oauth_scopes,omitempty"`
	OAuthAuthorizationTTLSeconds string   `json:"oauth_authorization_ttl_seconds,omitempty"`
}

// ActionGatewayMCPServerUpdateRequest updates a server's description.
type ActionGatewayMCPServerUpdateRequest struct {
	Description string `json:"description"`
}

// ActionGatewayMCPServerResyncRequest optionally selects a user for resync.
type ActionGatewayMCPServerResyncRequest struct {
	UserID string `json:"user_id,omitempty"`
}

// ActionGatewayMCPServerToolsUpdateRequest selects the enabled server tools.
type ActionGatewayMCPServerToolsUpdateRequest struct {
	EnabledToolSlugs []string `json:"enabledToolSlugs"`
	UserID           string   `json:"user_id,omitempty"`
}

// ActionGatewayMCPServerTool is a discovered MCP tool.
type ActionGatewayMCPServerTool struct {
	Name             string `json:"name"`
	ToolSlug         string `json:"toolSlug"`
	Description      string `json:"description"`
	Enabled          bool   `json:"enabled"`
	Quarantined      bool   `json:"quarantined"`
	QuarantineReason string `json:"quarantineReason"`
	CatalogSlug      string `json:"catalogSlug"`
}

// ActionGatewayMCPServerResponse contains one MCP server.
type ActionGatewayMCPServerResponse struct {
	MCPServer *ActionGatewayMCPServer `json:"mcpServer"`
}

// ActionGatewayMCPServersResponse contains registered servers.
type ActionGatewayMCPServersResponse struct {
	MCPServers []ActionGatewayMCPServer `json:"mcpServers"`
}

// ActionGatewayMCPServerToolsResponse contains discovered tools.
type ActionGatewayMCPServerToolsResponse struct {
	Tools []ActionGatewayMCPServerTool `json:"tools"`
}

// ActionGatewayMCPServerResyncResponse reports an immediate or pending resync.
type ActionGatewayMCPServerResyncResponse struct {
	MCPServer     *ActionGatewayMCPServer               `json:"mcpServer"`
	Tools         []ActionGatewayMCPServerTool          `json:"tools"`
	Pending       bool                                  `json:"pending"`
	Authorization *ActionGatewayConnectionAuthorization `json:"authorization"`
}

// Create registers an MCP server.
func (s *ActionGatewayMCPServersService) Create(ctx context.Context, body *ActionGatewayMCPServerCreateRequest) (*ActionGatewayMCPServerResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	return actionGatewayRequest[ActionGatewayMCPServerResponse](ctx, s.client, http.MethodPost, actionGatewayPath+"/mcp-servers", nil, body)
}

// List retrieves all registered MCP servers.
func (s *ActionGatewayMCPServersService) List(ctx context.Context) (*ActionGatewayMCPServersResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayMCPServersResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/mcp-servers", nil, nil)
}

// Get retrieves one MCP server.
func (s *ActionGatewayMCPServersService) Get(ctx context.Context, ref string) (*ActionGatewayMCPServerResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/mcp-servers", actionGatewayPathPart{"ref", ref})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayMCPServerResponse](ctx, s.client, http.MethodGet, path, nil, nil)
}

// Update changes an MCP server's description.
func (s *ActionGatewayMCPServersService) Update(ctx context.Context, ref string, body *ActionGatewayMCPServerUpdateRequest) (*ActionGatewayMCPServerResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	path, err := actionGatewayItem(actionGatewayPath+"/mcp-servers", actionGatewayPathPart{"ref", ref})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayMCPServerResponse](ctx, s.client, http.MethodPatch, path, nil, body)
}

// Delete unregisters an MCP server.
func (s *ActionGatewayMCPServersService) Delete(ctx context.Context, ref string) (*ActionGatewayMCPServerResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/mcp-servers", actionGatewayPathPart{"ref", ref})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayMCPServerResponse](ctx, s.client, http.MethodDelete, path, nil, nil)
}

// Resync refreshes a server's discovered tools; the response may be pending.
func (s *ActionGatewayMCPServersService) Resync(ctx context.Context, ref string, body *ActionGatewayMCPServerResyncRequest) (*ActionGatewayMCPServerResyncResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	path, err := actionGatewayItem(actionGatewayPath+"/mcp-servers", actionGatewayPathPart{"ref", ref})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayMCPServerResyncResponse](ctx, s.client, http.MethodPost, path+"/resync", nil, body)
}

// ListTools retrieves the discovered server tools.
func (s *ActionGatewayMCPServersService) ListTools(ctx context.Context, ref string) (*ActionGatewayMCPServerToolsResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/mcp-servers", actionGatewayPathPart{"ref", ref})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayMCPServerToolsResponse](ctx, s.client, http.MethodGet, path+"/tools", nil, nil)
}

// UpdateTools changes the enabled server tools.
func (s *ActionGatewayMCPServersService) UpdateTools(ctx context.Context, ref string, body *ActionGatewayMCPServerToolsUpdateRequest) (*ActionGatewayMCPServerToolsResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	path, err := actionGatewayItem(actionGatewayPath+"/mcp-servers", actionGatewayPathPart{"ref", ref})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayMCPServerToolsResponse](ctx, s.client, http.MethodPut, path+"/tools", nil, body)
}
