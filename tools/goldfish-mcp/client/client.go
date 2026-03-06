package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/grafana/loki/v3/pkg/goldfish"
)

const (
	queriesEndpoint = "/ui/api/v1/goldfish/queries"
	statsEndpoint   = "/ui/api/v1/goldfish/stats"
	resultsEndpoint = "/ui/api/v1/goldfish/results"
)

// Client is an HTTP client for the Goldfish API
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Goldfish API client
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// HealthCheck performs a health check by calling the stats endpoint
// Returns nil if the API is accessible, error otherwise
func (c *Client) HealthCheck(ctx context.Context) error {
	endpoint := statsEndpoint
	fullURL := c.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if IsConnectionError(err) {
			return fmt.Errorf("%s", FormatConnectionError(c.baseURL, endpoint, err))
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return fmt.Errorf("%s", FormatAPIError(resp.StatusCode, errResp.Error, endpoint))
		}
		return fmt.Errorf("%s", FormatAPIError(resp.StatusCode, string(body), endpoint))
	}

	return nil
}

// GetQueries retrieves a list of queries with the specified filters
func (c *Client) GetQueries(ctx context.Context, params QueryParams) (*QueriesResponse, error) {
	endpoint := queriesEndpoint
	fullURL := c.baseURL + endpoint

	// Build query parameters
	queryParams := url.Values{}
	if params.Page > 0 {
		queryParams.Set("page", strconv.Itoa(params.Page))
	}
	if params.PageSize > 0 {
		queryParams.Set("pageSize", strconv.Itoa(params.PageSize))
	}
	if params.Tenant != "" {
		queryParams.Set("tenant", params.Tenant)
	}
	if params.User != "" {
		queryParams.Set("user", params.User)
	}
	if params.ComparisonStatus != "" {
		queryParams.Set("comparisonStatus", string(params.ComparisonStatus))
	}
	if params.UsedNewEngine != nil {
		queryParams.Set("newEngine", strconv.FormatBool(*params.UsedNewEngine))
	}
	if !params.From.IsZero() {
		queryParams.Set("from", params.From.Format(time.RFC3339))
	}
	if !params.To.IsZero() {
		queryParams.Set("to", params.To.Format(time.RFC3339))
	}

	if len(queryParams) > 0 {
		fullURL += "?" + queryParams.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if IsConnectionError(err) {
			return nil, fmt.Errorf("%s", FormatConnectionError(c.baseURL, endpoint, err))
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", FormatAPIError(resp.StatusCode, errResp.Error, endpoint))
		}
		return nil, fmt.Errorf("%s", FormatAPIError(resp.StatusCode, string(body), endpoint))
	}

	var apiResult apiQueriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResult); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return apiResult.toQueriesResponse(), nil
}

// GetQueryByCorrelationID retrieves a specific query by its correlation ID
func (c *Client) GetQueryByCorrelationID(ctx context.Context, correlationID string) (*goldfish.QuerySample, error) {
	endpoint := queriesEndpoint
	fullURL := c.baseURL + endpoint + "?correlationId=" + url.QueryEscape(correlationID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if IsConnectionError(err) {
			return nil, fmt.Errorf("%s", FormatConnectionError(c.baseURL, endpoint, err))
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", FormatAPIError(resp.StatusCode, errResp.Error, endpoint))
		}
		return nil, fmt.Errorf("%s", FormatAPIError(resp.StatusCode, string(body), endpoint))
	}

	var apiResult apiQueriesResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResult); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResult.Queries) == 0 {
		return nil, fmt.Errorf("query with correlation ID %s not found", correlationID)
	}

	result := apiResult.toQueriesResponse()
	return &result.Queries[0], nil
}

// GetStatistics retrieves aggregated statistics
func (c *Client) GetStatistics(ctx context.Context, params StatsParams) (*goldfish.Statistics, error) {
	endpoint := statsEndpoint
	fullURL := c.baseURL + endpoint

	// Build query parameters
	queryParams := url.Values{}
	if !params.From.IsZero() {
		queryParams.Set("from", params.From.Format(time.RFC3339))
	}
	if !params.To.IsZero() {
		queryParams.Set("to", params.To.Format(time.RFC3339))
	}
	// Only set usesRecentData if it's false (true is the default)
	if !params.UsesRecentData {
		queryParams.Set("usesRecentData", "false")
	}

	if len(queryParams) > 0 {
		fullURL += "?" + queryParams.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if IsConnectionError(err) {
			return nil, fmt.Errorf("%s", FormatConnectionError(c.baseURL, endpoint, err))
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", FormatAPIError(resp.StatusCode, errResp.Error, endpoint))
		}
		return nil, fmt.Errorf("%s", FormatAPIError(resp.StatusCode, string(body), endpoint))
	}

	var result goldfish.Statistics
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

// GetResult retrieves the result payload for a specific cell
// cell should be "cell-a" or "cell-b"
func (c *Client) GetResult(ctx context.Context, correlationID, cell string) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("%s/%s/%s", resultsEndpoint, correlationID, cell)
	fullURL := c.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if IsConnectionError(err) {
			return nil, fmt.Errorf("%s", FormatConnectionError(c.baseURL, endpoint, err))
		}
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Special handling for missing results
		return nil, fmt.Errorf("%s", FormatResultNotFoundError(correlationID))
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("%s", FormatAPIError(resp.StatusCode, errResp.Error, endpoint))
		}
		return nil, fmt.Errorf("%s", FormatAPIError(resp.StatusCode, string(body), endpoint))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}
