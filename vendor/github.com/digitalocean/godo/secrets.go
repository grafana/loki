package godo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const secretsBasePath = "v2/security/secrets"

// SecretsService is an interface for interacting with the Secrets endpoints of
// the DigitalOcean API.
type SecretsService interface {
	List(context.Context, *ListOptions) (*SecretsList, *Response, error)
	Get(context.Context, string, string) (*Secret, *Response, error)
	ListVersions(context.Context, string, string) ([]*SecretVersion, *Response, error)
	Create(context.Context, *SecretCreateRequest) (*SecretWriteResult, *Response, error)
	Update(context.Context, string, *SecretUpdateRequest) (*SecretWriteResult, *Response, error)
	Delete(context.Context, string, string) (*Response, error)
	Restore(context.Context, string, string) (*Response, error)
}

// SecretsServiceOp handles communication with Secrets related methods of the
// DigitalOcean API.
type SecretsServiceOp struct {
	client *Client
}

var _ SecretsService = &SecretsServiceOp{}

// Secret represents a DigitalOcean secret.
type Secret struct {
	Name              string            `json:"secret"`
	Region            string            `json:"region,omitempty"`
	Version           int               `json:"version"`
	Values            map[string]string `json:"values,omitempty"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at,omitempty"`
	DeleteRequestedAt *string           `json:"delete_requested_at,omitempty"`
}

// SecretVersion represents a version of a secret.
type SecretVersion struct {
	Version   int    `json:"version"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// SecretCreateRequest represents a request to create a secret.
type SecretCreateRequest struct {
	Name   string            `json:"name"`
	Region string            `json:"region"`
	Values map[string]string `json:"values"`
}

// SecretUpdateRequest represents a request to update a secret.
type SecretUpdateRequest struct {
	Region  string            `json:"region"`
	Version int               `json:"version"`
	Values  map[string]string `json:"values"`
}

// SecretWriteResult represents the response from creating or updating a secret.
type SecretWriteResult struct {
	Name    string `json:"name"`
	Region  string `json:"region"`
	Version int    `json:"version"`
}

// SecretsList holds the result of a list secrets request.
type SecretsList struct {
	Secrets            []*Secret
	UnavailableRegions []string
}

type secretRegionOptions struct {
	Region string `url:"region"`
}

type secretsListRoot struct {
	Secrets            []*Secret `json:"secrets"`
	Links              *Links    `json:"links"`
	Meta               *Meta     `json:"meta"`
	UnavailableRegions []string  `json:"unavailable_regions,omitempty"`
}

type secretVersionsRoot struct {
	Versions []*SecretVersion `json:"versions"`
}

// List returns a paginated list of secrets across all regions.
func (s *SecretsServiceOp) List(ctx context.Context, opts *ListOptions) (*SecretsList, *Response, error) {
	path, err := addOptions(secretsBasePath, opts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(secretsListRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if l := root.Links; l != nil {
		resp.Links = l
	}
	if m := root.Meta; m != nil {
		resp.Meta = m
	}

	return &SecretsList{
		Secrets:            root.Secrets,
		UnavailableRegions: root.UnavailableRegions,
	}, resp, nil
}

// Get retrieves a secret by name and region.
func (s *SecretsServiceOp) Get(ctx context.Context, name, region string) (*Secret, *Response, error) {
	if name == "" {
		return nil, nil, NewArgError("name", "cannot be empty")
	}
	if region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", secretsBasePath, name)
	path, err := addOptions(path, &secretRegionOptions{Region: region})
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	secret := new(Secret)
	resp, err := s.client.Do(ctx, req, secret)
	if err != nil {
		return nil, resp, err
	}

	return secret, resp, nil
}

// ListVersions returns all versions of a secret.
func (s *SecretsServiceOp) ListVersions(ctx context.Context, name, region string) ([]*SecretVersion, *Response, error) {
	if name == "" {
		return nil, nil, NewArgError("name", "cannot be empty")
	}
	if region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/versions", secretsBasePath, name)
	path, err := addOptions(path, &secretRegionOptions{Region: region})
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(secretVersionsRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Versions, resp, nil
}

// Create creates a new secret.
func (s *SecretsServiceOp) Create(ctx context.Context, createRequest *SecretCreateRequest) (*SecretWriteResult, *Response, error) {
	if createRequest == nil {
		return nil, nil, NewArgError("createRequest", "cannot be nil")
	}
	if createRequest.Region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, secretsBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	result := new(SecretWriteResult)
	resp, err := s.doDecodeJSON(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// Update updates an existing secret.
func (s *SecretsServiceOp) Update(ctx context.Context, name string, updateRequest *SecretUpdateRequest) (*SecretWriteResult, *Response, error) {
	if name == "" {
		return nil, nil, NewArgError("name", "cannot be empty")
	}
	if updateRequest == nil {
		return nil, nil, NewArgError("updateRequest", "cannot be nil")
	}
	if updateRequest.Region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", secretsBasePath, name)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	result := new(SecretWriteResult)
	resp, err := s.doDecodeJSON(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// Delete schedules a secret for soft deletion.
func (s *SecretsServiceOp) Delete(ctx context.Context, name, region string) (*Response, error) {
	if name == "" {
		return nil, NewArgError("name", "cannot be empty")
	}
	if region == "" {
		return nil, NewArgError("region", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", secretsBasePath, name)
	path, err := addOptions(path, &secretRegionOptions{Region: region})
	if err != nil {
		return nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}

// Restore restores a secret that was scheduled for deletion.
func (s *SecretsServiceOp) Restore(ctx context.Context, name, region string) (*Response, error) {
	if name == "" {
		return nil, NewArgError("name", "cannot be empty")
	}
	if region == "" {
		return nil, NewArgError("region", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/restore", secretsBasePath, name)
	path, err := addOptions(path, &secretRegionOptions{Region: region})
	if err != nil {
		return nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}

// doDecodeJSON sends a request and decodes the JSON response body even when
// the API returns 204 No Content with a body.
func (s *SecretsServiceOp) doDecodeJSON(ctx context.Context, req *http.Request, v interface{}) (*Response, error) {
	c := s.client
	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, err
		}
	}

	resp, err := DoRequestWithClient(ctx, c.HTTPClient, req)
	if err != nil {
		return nil, err
	}
	if c.onRequestCompleted != nil {
		c.onRequestCompleted(req, resp)
	}

	defer func() {
		const maxBodySlurpSize = 2 << 10
		if resp.ContentLength == -1 || resp.ContentLength <= maxBodySlurpSize {
			io.CopyN(io.Discard, resp.Body, maxBodySlurpSize)
		}
		resp.Body.Close()
	}()

	response := newResponse(resp)
	c.ratemtx.Lock()
	c.Rate = response.Rate
	c.ratemtx.Unlock()

	if err = CheckResponse(resp); err != nil {
		return response, err
	}

	if v != nil {
		if err = json.NewDecoder(resp.Body).Decode(v); err != nil {
			return response, err
		}
	}

	return response, nil
}
