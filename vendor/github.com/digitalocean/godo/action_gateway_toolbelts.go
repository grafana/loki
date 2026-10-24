package godo

import (
	"context"
	"net/http"
)

// ActionGatewayToolbeltsService manages versioned toolbelts.
type ActionGatewayToolbeltsService struct{ client *Client }

// ActionGatewayToolbelt is an immutable version of a toolbelt.
type ActionGatewayToolbelt struct {
	Name            string     `json:"name"`
	Version         string     `json:"version"`
	DisplayName     string     `json:"display_name"`
	Description     string     `json:"description"`
	Tools           []string   `json:"tools"`
	Status          string     `json:"status"`
	Reference       string     `json:"reference"`
	ReferenceLatest string     `json:"reference_latest"`
	ToolCount       int        `json:"tool_count"`
	CreatedAt       *Timestamp `json:"created_at"`
	UpdatedAt       *Timestamp `json:"updated_at"`
}

// ActionGatewayToolbeltSummary summarizes the latest version.
type ActionGatewayToolbeltSummary struct {
	Name            string     `json:"name"`
	DisplayName     string     `json:"display_name"`
	Description     string     `json:"description"`
	LatestVersion   string     `json:"latest_version"`
	VersionCount    int        `json:"version_count"`
	ToolCount       int        `json:"tool_count"`
	Status          string     `json:"status"`
	ReferenceLatest string     `json:"reference_latest"`
	UpdatedAt       *Timestamp `json:"updated_at"`
}

// ActionGatewayToolbeltTool describes one pinned tool.
type ActionGatewayToolbeltTool struct {
	ToolSlug    string `json:"tool_slug"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Provider    string `json:"provider"`
	Category    string `json:"category"`
	Version     int    `json:"version"`
}

// ActionGatewayToolbeltProvider describes a provider in a toolbelt.
type ActionGatewayToolbeltProvider struct {
	Provider    string     `json:"provider"`
	ToolCount   int        `json:"tool_count"`
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Categories  []string   `json:"categories"`
	CreatedAt   *Timestamp `json:"created_at"`
}

// ActionGatewayToolbeltCreateRequest creates a toolbelt.
type ActionGatewayToolbeltCreateRequest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Description string   `json:"description,omitempty"`
	Tools       []string `json:"tools,omitempty"`
}

// ActionGatewayToolbeltToolsRequest modifies a toolbelt's members.
type ActionGatewayToolbeltToolsRequest struct {
	Tools []string `json:"tools"`
}

// ActionGatewayToolbeltResponse contains the resulting toolbelt.
type ActionGatewayToolbeltResponse struct {
	Toolbelt *ActionGatewayToolbelt `json:"toolbelt"`
}

// ActionGatewayGetToolbeltResponse contains one toolbelt and cursor-paginated member details.
type ActionGatewayGetToolbeltResponse struct {
	Toolbelt      *ActionGatewayToolbelt      `json:"toolbelt"`
	ToolDetails   []ActionGatewayToolbeltTool `json:"tool_details"`
	NextPageToken string                      `json:"next_page_token"`
}

// ActionGatewayToolbeltsResponse contains an offset-paginated list.
type ActionGatewayToolbeltsResponse struct {
	Toolbelts  []ActionGatewayToolbeltSummary `json:"toolbelts"`
	Pagination *ActionGatewayPagination       `json:"pagination"`
}

// ActionGatewaySearchToolbeltsResponse contains cursor-paginated search results.
type ActionGatewaySearchToolbeltsResponse struct {
	Toolbelts     []ActionGatewayToolbeltSummary `json:"toolbelts"`
	NextPageToken string                         `json:"next_page_token"`
}

// ActionGatewayToolbeltProvidersResponse contains a toolbelt's providers.
type ActionGatewayToolbeltProvidersResponse struct {
	Toolbelt   *ActionGatewayToolbelt          `json:"toolbelt"`
	Providers  []ActionGatewayToolbeltProvider `json:"providers"`
	Pagination *ActionGatewayPagination        `json:"pagination"`
}

// ActionGatewayToolbeltProviderToolsResponse contains tools for a provider.
type ActionGatewayToolbeltProviderToolsResponse struct {
	Tools      []ActionGatewayToolbeltTool `json:"tools"`
	Pagination *ActionGatewayPagination    `json:"pagination"`
}

// ActionGatewayToolbeltsListOptions filters an offset-based toolbelt list.
type ActionGatewayToolbeltsListOptions struct {
	Status  string `url:"status,omitempty"`
	Page    int    `url:"page,omitempty"`
	PerPage int    `url:"per_page,omitempty"`
}

// ActionGatewayToolbeltsSearchOptions filters cursor-based toolbelt search.
type ActionGatewayToolbeltsSearchOptions struct {
	Query     string `url:"query,omitempty"`
	Status    string `url:"status,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
	PageToken string `url:"page_token,omitempty"`
}

// ActionGatewayGetToolbeltOptions selects a version and pages member details.
type ActionGatewayGetToolbeltOptions struct {
	Version   string `url:"version,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
	PageToken string `url:"page_token,omitempty"`
	Search    string `url:"search,omitempty"`
}

// ActionGatewayToolbeltProviderOptions selects a version and offset page.
type ActionGatewayToolbeltProviderOptions struct {
	Version string `url:"version,omitempty"`
	Page    int    `url:"page,omitempty"`
	PerPage int    `url:"per_page,omitempty"`
	Search  string `url:"search,omitempty"`
}

// Create creates a toolbelt.
func (s *ActionGatewayToolbeltsService) Create(ctx context.Context, body *ActionGatewayToolbeltCreateRequest) (*ActionGatewayToolbeltResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	return actionGatewayRequest[ActionGatewayToolbeltResponse](ctx, s.client, http.MethodPost, actionGatewayPath+"/toolbelts", nil, body)
}

// List lists toolbelts.
func (s *ActionGatewayToolbeltsService) List(ctx context.Context, opt *ActionGatewayToolbeltsListOptions) (*ActionGatewayToolbeltsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayToolbeltsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/toolbelts", opt, nil)
}

// Search searches toolbelts using cursor pagination.
func (s *ActionGatewayToolbeltsService) Search(ctx context.Context, opt *ActionGatewayToolbeltsSearchOptions) (*ActionGatewaySearchToolbeltsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewaySearchToolbeltsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/toolbelts/search", opt, nil)
}

// Get retrieves one toolbelt.
func (s *ActionGatewayToolbeltsService) Get(ctx context.Context, name string, opt *ActionGatewayGetToolbeltOptions) (*ActionGatewayGetToolbeltResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/toolbelts", actionGatewayPathPart{"name", name})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayGetToolbeltResponse](ctx, s.client, http.MethodGet, path, opt, nil)
}

// Delete deletes a toolbelt.
func (s *ActionGatewayToolbeltsService) Delete(ctx context.Context, name string) (*ActionGatewayToolbeltResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/toolbelts", actionGatewayPathPart{"name", name})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayToolbeltResponse](ctx, s.client, http.MethodDelete, path, nil, nil)
}

// AddTools creates a new version containing the additional tools.
func (s *ActionGatewayToolbeltsService) AddTools(ctx context.Context, name string, body *ActionGatewayToolbeltToolsRequest) (*ActionGatewayToolbeltResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	path, err := actionGatewayItem(actionGatewayPath+"/toolbelts", actionGatewayPathPart{"name", name})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayToolbeltResponse](ctx, s.client, http.MethodPost, path+"/tools/add", nil, body)
}

// RemoveTools creates a new version without the specified tools.
func (s *ActionGatewayToolbeltsService) RemoveTools(ctx context.Context, name string, body *ActionGatewayToolbeltToolsRequest) (*ActionGatewayToolbeltResponse, *Response, error) {
	if body == nil {
		return nil, nil, NewArgError("body", "cannot be nil")
	}
	path, err := actionGatewayItem(actionGatewayPath+"/toolbelts", actionGatewayPathPart{"name", name})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayToolbeltResponse](ctx, s.client, http.MethodPost, path+"/tools/remove", nil, body)
}

// ListProviders lists providers in a toolbelt.
func (s *ActionGatewayToolbeltsService) ListProviders(ctx context.Context, name string, opt *ActionGatewayToolbeltProviderOptions) (*ActionGatewayToolbeltProvidersResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/toolbelts", actionGatewayPathPart{"name", name})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayToolbeltProvidersResponse](ctx, s.client, http.MethodGet, path+"/providers", opt, nil)
}

// ListProviderTools lists a provider's tools in a toolbelt.
func (s *ActionGatewayToolbeltsService) ListProviderTools(ctx context.Context, name, provider string, opt *ActionGatewayToolbeltProviderOptions) (*ActionGatewayToolbeltProviderToolsResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/toolbelts", actionGatewayPathPart{"name", name})
	if err != nil {
		return nil, nil, err
	}
	path, err = actionGatewayItem(path+"/providers", actionGatewayPathPart{"provider", provider})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayToolbeltProviderToolsResponse](ctx, s.client, http.MethodGet, path+"/tools", opt, nil)
}
