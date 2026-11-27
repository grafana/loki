package godo

import (
	"context"
	"encoding/json"
	"net/http"
)

// ActionGatewayToolsService exposes catalog and health operations.
type ActionGatewayToolsService struct{ client *Client }

// ActionGatewayToolkit describes a catalog toolkit.
type ActionGatewayToolkit struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Categories  []string   `json:"categories"`
	CreatedAt   *Timestamp `json:"created_at"`
}

// ActionGatewayToolAnnotations describes a tool's behavioral hints.
type ActionGatewayToolAnnotations struct {
	Title           string `json:"title"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	IdempotentHint  bool   `json:"idempotentHint"`
	OpenWorldHint   bool   `json:"openWorldHint"`
	DestructiveHint bool   `json:"destructiveHint"`
}

// ActionGatewayTool describes a catalog tool.
type ActionGatewayTool struct {
	Name           string                        `json:"name"`
	ToolkitID      string                        `json:"toolkitId"`
	ToolSlug       string                        `json:"toolSlug"`
	Title          string                        `json:"title"`
	Description    string                        `json:"description"`
	Version        string                        `json:"version"`
	InputSchema    json.RawMessage               `json:"inputSchema"`
	OutputSchema   json.RawMessage               `json:"outputSchema"`
	Annotations    *ActionGatewayToolAnnotations `json:"annotations"`
	Parallelizable bool                          `json:"parallelizable"`
	StreamingSafe  bool                          `json:"streamingSafe"`
}

// ActionGatewayHTTPLookup describes HTTP-based credential resolution.
type ActionGatewayHTTPLookup struct {
	Method              string   `json:"method"`
	URL                 string   `json:"url"`
	MatchField          string   `json:"matchField"`
	MatchValue          string   `json:"matchValue"`
	MatchValueParameter string   `json:"match_value_parameter"`
	CaseInsensitive     bool     `json:"caseInsensitive"`
	TrimTrailingSlash   bool     `json:"trimTrailingSlash"`
	ExtractField        string   `json:"extractField"`
	BaseURLTemplate     string   `json:"baseUrlTemplate"`
	RequiredScopes      []string `json:"requiredScopes"`
}

// ActionGatewayToolDefinition is the full public catalog definition.
type ActionGatewayToolDefinition struct {
	ToolID         string                           `json:"toolId"`
	Name           string                           `json:"name"`
	ToolkitID      string                           `json:"toolkitId"`
	ToolSlug       string                           `json:"toolSlug"`
	Title          string                           `json:"title"`
	Description    string                           `json:"description"`
	InputSchema    json.RawMessage                  `json:"inputSchema"`
	OutputSchema   json.RawMessage                  `json:"outputSchema"`
	Annotations    *ActionGatewayToolAnnotations    `json:"annotations"`
	Version        string                           `json:"version"`
	SchemaVersion  string                           `json:"schemaVersion"`
	Status         string                           `json:"status"`
	Parallelizable bool                             `json:"parallelizable"`
	StreamingSafe  bool                             `json:"streamingSafe"`
	Auth           *ActionGatewayToolAuth           `json:"auth"`
	Execution      *ActionGatewayToolExecution      `json:"execution"`
	Transform      *ActionGatewayToolTransform      `json:"transform"`
	Policy         *ActionGatewayToolPolicy         `json:"policy"`
	Reliability    *ActionGatewayToolReliability    `json:"reliability"`
	Hooks          *ActionGatewayToolHooks          `json:"hooks"`
	Classification *ActionGatewayToolClassification `json:"classification"`
	FlipperName    string                           `json:"flipperName"`
	Tags           []string                         `json:"tags"`
	ProviderKind   string                           `json:"providerKind"`
}

// ActionGatewayToolAuth describes a definition's authentication requirements.
type ActionGatewayToolAuth struct {
	Modes                  []string `json:"modes"`
	Provider               string   `json:"provider"`
	Scopes                 []string `json:"scopes"`
	CredentialBinding      string   `json:"credentialBinding"`
	CredentialRefSource    string   `json:"credentialRefSource"`
	DOManagedCredentialRef string   `json:"doManagedCredentialRef"`
	Injection              *struct {
		Location string `json:"location"`
		Name     string `json:"name"`
		Scheme   string `json:"scheme"`
	} `json:"injection"`
	BaseURLResolution *struct {
		HTTPLookup *ActionGatewayHTTPLookup `json:"httpLookup"`
	} `json:"baseUrlResolution"`
	HeaderResolution []struct {
		HeaderName string                   `json:"headerName"`
		HTTPLookup *ActionGatewayHTTPLookup `json:"httpLookup"`
	} `json:"headerResolution"`
	TeamAPIKey *struct {
		TokenExchange *struct {
			Method        string `json:"method"`
			URLTemplate   string `json:"urlTemplate"`
			ExtractField  string `json:"extractField"`
			BodyParameter string `json:"bodyParameter"`
		} `json:"tokenExchange"`
	} `json:"teamApiKey"`
}

// ActionGatewayToolExecution describes a tool execution adapter.
type ActionGatewayToolExecution struct {
	Type           string `json:"type"`
	AdapterVersion string `json:"adapterVersion"`
	ConfigRef      string `json:"configRef"`
	HTTP           *struct {
		BaseURL             string   `json:"baseUrl"`
		Path                string   `json:"path"`
		Method              string   `json:"method"`
		AllowedHosts        []string `json:"allowedHosts"`
		RequestEncoding     string   `json:"requestEncoding"`
		ResponseFormat      string   `json:"responseFormat"`
		AllowedHostSuffixes []string `json:"allowed_host_suffixes"`
	} `json:"http"`
	MCP *struct {
		Endpoint            string   `json:"endpoint"`
		ToolName            string   `json:"toolName"`
		Transport           string   `json:"transport"`
		ServerRef           string   `json:"serverRef"`
		AllowedHosts        []string `json:"allowedHosts"`
		AllowedHostSuffixes []string `json:"allowed_host_suffixes"`
	} `json:"mcp"`
}

// ActionGatewayToolTransform describes public input and output mappings.
type ActionGatewayToolTransform struct {
	Language string          `json:"language"`
	Input    json.RawMessage `json:"input"`
	Output   json.RawMessage `json:"output"`
}

// ActionGatewayToolPolicy describes a tool's permission classification.
type ActionGatewayToolPolicy struct {
	Permission string `json:"permission"`
}

// ActionGatewayToolReliability describes timeouts and retry settings.
type ActionGatewayToolReliability struct {
	TimeoutMS      int    `json:"timeoutMs"`
	MaxOutputBytes string `json:"maxOutputBytes"`
	Retry          *struct {
		MaxAttempts     int      `json:"maxAttempts"`
		Backoff         string   `json:"backoff"`
		RetryOn         []string `json:"retryOn"`
		HonorRetryAfter bool     `json:"honorRetryAfter"`
		BackoffPolicy   *struct {
			InitialMS  string  `json:"initialMs"`
			Multiplier float64 `json:"multiplier"`
			MaxMS      string  `json:"maxMs"`
			Jitter     string  `json:"jitter"`
		} `json:"backoffPolicy"`
		Replay *struct {
			Mode string `json:"mode"`
		} `json:"replay"`
		Billable *struct {
			Enabled bool `json:"enabled"`
		} `json:"billable"`
	} `json:"retry"`
	ProviderRateLimit *struct {
		Scope string `json:"scope"`
	} `json:"provider_rate_limit"`
}

// ActionGatewayToolHooks describes billing hooks.
type ActionGatewayToolHooks struct {
	Usage *struct {
		Billable bool `json:"billable"`
		Meters   []struct {
			SKU            string `json:"sku"`
			Unit           string `json:"unit"`
			QuantitySource string `json:"quantitySource"`
		} `json:"meters"`
	} `json:"usage"`
}

// ActionGatewayToolClassification describes a tool's risk and data classes.
type ActionGatewayToolClassification struct {
	Operation   string   `json:"operation"`
	Risk        string   `json:"risk"`
	DataClasses []string `json:"dataClasses"`
}

// ActionGatewayProvider describes a tool provider and its connection requirements.
type ActionGatewayProvider struct {
	Name                 string                             `json:"name"`
	DisplayName          string                             `json:"display_name"`
	Description          string                             `json:"description"`
	AuthTypes            []string                           `json:"auth_types"`
	AuthType             string                             `json:"auth_type"`
	Scopes               []string                           `json:"scopes"`
	ConnectionParameters []ActionGatewayConnectionParameter `json:"connection_parameters"`
	CredentialParameters []ActionGatewayConnectionParameter `json:"credential_parameters"`
}

// ActionGatewayConnectionParameter describes one required provider setting.
type ActionGatewayConnectionParameter struct {
	Key                 string   `json:"key"`
	Label               string   `json:"label"`
	Description         string   `json:"description"`
	InputKind           string   `json:"input_kind"`
	Required            bool     `json:"required"`
	Normalization       string   `json:"normalization"`
	AllowedValues       []string `json:"allowed_values"`
	AllowedHostSuffixes []string `json:"allowed_host_suffixes"`
	MaxLength           int      `json:"max_length"`
	Pattern             string   `json:"pattern"`
}

// ActionGatewayToolSearchResult is a compact search hit.
type ActionGatewayToolSearchResult struct {
	ToolSlug    string `json:"tool_slug"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Provider    string `json:"provider"`
	Category    string `json:"category"`
	Version     int    `json:"version"`
}

// ActionGatewayHealthSnapshot describes a health sampling window.
type ActionGatewayHealthSnapshot struct {
	MeasuredAt *Timestamp `json:"measured_at"`
	Window     string     `json:"window"`
	Stale      bool       `json:"stale"`
}

// ActionGatewayHealthMetrics describes measured tool or provider health.
type ActionGatewayHealthMetrics struct {
	DataStatus       string  `json:"data_status"`
	UptimePercentage float64 `json:"uptime_percentage"`
	LatencyP50MS     float64 `json:"latency_p50_ms"`
	LatencyP95MS     float64 `json:"latency_p95_ms"`
}

// ActionGatewayProviderHealth is a provider's health summary.
type ActionGatewayProviderHealth struct {
	Provider string                      `json:"provider"`
	Health   *ActionGatewayHealthMetrics `json:"health"`
}

// ActionGatewayToolHealth is a tool's health summary.
type ActionGatewayToolHealth struct {
	ToolSlug string                      `json:"tool_slug"`
	Provider string                      `json:"provider"`
	Health   *ActionGatewayHealthMetrics `json:"health"`
}

// ActionGatewayHealthHistory contains per-bucket measurements.
type ActionGatewayHealthHistory struct {
	Resolution string `json:"resolution"`
	Stale      bool   `json:"stale"`
	Points     []struct {
		BucketStart *Timestamp                  `json:"bucket_start"`
		Health      *ActionGatewayHealthMetrics `json:"health"`
	} `json:"points"`
}

// ActionGatewayToolListOptions filters the tool catalog.
type ActionGatewayToolListOptions struct {
	ToolkitID string `url:"toolkit_id,omitempty"`
	Page      int    `url:"page,omitempty"`
	PerPage   int    `url:"per_page,omitempty"`
}

// ActionGatewayToolSearchOptions filters the cursor-based tool search.
type ActionGatewayToolSearchOptions struct {
	Query     string   `url:"query,omitempty"`
	Providers []string `url:"provider,omitempty"`
	Toolbelt  string   `url:"toolbelt,omitempty"`
	PageSize  int      `url:"page_size,omitempty"`
	PageToken string   `url:"page_token,omitempty"`
}

// ActionGatewayProviderSearchOptions filters provider search.
type ActionGatewayProviderSearchOptions struct {
	Query     string `url:"query,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
	PageToken string `url:"page_token,omitempty"`
}

// ActionGatewayHealthOptions filters a paginated health listing.
type ActionGatewayHealthOptions struct {
	Provider string `url:"provider,omitempty"`
	Window   string `url:"window,omitempty"`
	Page     int    `url:"page,omitempty"`
	PerPage  int    `url:"per_page,omitempty"`
}

// ActionGatewayToolHealthOptions selects a tool's health window and history.
type ActionGatewayToolHealthOptions struct {
	Window         string `url:"window,omitempty"`
	IncludeHistory bool   `url:"include_history,omitempty"`
}

// ActionGatewayToolkitsResponse contains toolkits and their catalog version.
type ActionGatewayToolkitsResponse struct {
	Version  string                 `json:"version"`
	Toolkits []ActionGatewayToolkit `json:"toolkits"`
}

// ActionGatewayToolsResponse contains a page of tools and definitions.
type ActionGatewayToolsResponse struct {
	Version     string                        `json:"version"`
	Tools       []ActionGatewayTool           `json:"tools"`
	Definitions []ActionGatewayToolDefinition `json:"definitions"`
	Pagination  *ActionGatewayPagination      `json:"pagination"`
}

// ActionGatewayProvidersResponse contains providers.
type ActionGatewayProvidersResponse struct {
	Providers []ActionGatewayProvider `json:"providers"`
}

// ActionGatewaySearchToolsResponse contains cursor-paginated tool hits.
type ActionGatewaySearchToolsResponse struct {
	Tools         []ActionGatewayToolSearchResult `json:"tools"`
	NextPageToken string                          `json:"next_page_token"`
}

// ActionGatewaySearchProvidersResponse contains cursor-paginated provider hits.
type ActionGatewaySearchProvidersResponse struct {
	Providers     []ActionGatewayProvider `json:"providers"`
	NextPageToken string                  `json:"next_page_token"`
}

// ActionGatewayProviderHealthResponse contains paginated provider health.
type ActionGatewayProviderHealthResponse struct {
	Snapshot   *ActionGatewayHealthSnapshot  `json:"snapshot"`
	Providers  []ActionGatewayProviderHealth `json:"providers"`
	Pagination *ActionGatewayPagination      `json:"pagination"`
}

// ActionGatewayToolHealthResponse contains paginated tool health.
type ActionGatewayToolHealthResponse struct {
	Snapshot   *ActionGatewayHealthSnapshot `json:"snapshot"`
	Tools      []ActionGatewayToolHealth    `json:"tools"`
	Pagination *ActionGatewayPagination     `json:"pagination"`
}

// ActionGatewayGetToolHealthResponse contains one tool's health and history.
type ActionGatewayGetToolHealthResponse struct {
	Snapshot *ActionGatewayHealthSnapshot `json:"snapshot"`
	Tool     *ActionGatewayToolHealth     `json:"tool"`
	History  *ActionGatewayHealthHistory  `json:"history"`
}

// List retrieves a page of catalog tools.
func (s *ActionGatewayToolsService) List(ctx context.Context, opt *ActionGatewayToolListOptions) (*ActionGatewayToolsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayToolsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/tools", opt, nil)
}

// ListToolkits retrieves catalog toolkits.
func (s *ActionGatewayToolsService) ListToolkits(ctx context.Context) (*ActionGatewayToolkitsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayToolkitsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/tools/toolkits", nil, nil)
}

// ListProviders retrieves tool providers.
func (s *ActionGatewayToolsService) ListProviders(ctx context.Context) (*ActionGatewayProvidersResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayProvidersResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/tools/providers", nil, nil)
}

// Search retrieves cursor-paginated tool search results.
func (s *ActionGatewayToolsService) Search(ctx context.Context, opt *ActionGatewayToolSearchOptions) (*ActionGatewaySearchToolsResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewaySearchToolsResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/tools/search", opt, nil)
}

// SearchProviders retrieves cursor-paginated provider search results.
func (s *ActionGatewayToolsService) SearchProviders(ctx context.Context, opt *ActionGatewayProviderSearchOptions) (*ActionGatewaySearchProvidersResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewaySearchProvidersResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/tools/providers/search", opt, nil)
}

// ListProviderHealth retrieves provider health snapshots.
func (s *ActionGatewayToolsService) ListProviderHealth(ctx context.Context, opt *ActionGatewayHealthOptions) (*ActionGatewayProviderHealthResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayProviderHealthResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/tools/health/providers", opt, nil)
}

// ListHealth retrieves tool health snapshots.
func (s *ActionGatewayToolsService) ListHealth(ctx context.Context, opt *ActionGatewayHealthOptions) (*ActionGatewayToolHealthResponse, *Response, error) {
	return actionGatewayRequest[ActionGatewayToolHealthResponse](ctx, s.client, http.MethodGet, actionGatewayPath+"/tools/health", opt, nil)
}

// GetHealth retrieves health and optionally history for one tool.
func (s *ActionGatewayToolsService) GetHealth(ctx context.Context, slug string, opt *ActionGatewayToolHealthOptions) (*ActionGatewayGetToolHealthResponse, *Response, error) {
	path, err := actionGatewayItem(actionGatewayPath+"/tools/health/tools", actionGatewayPathPart{"slug", slug})
	if err != nil {
		return nil, nil, err
	}
	return actionGatewayRequest[ActionGatewayGetToolHealthResponse](ctx, s.client, http.MethodGet, path, opt, nil)
}
