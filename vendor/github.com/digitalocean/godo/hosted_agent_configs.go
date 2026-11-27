package godo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

const (
	hostedAgentsConfigsBasePath   = "/v2/agents/configs"
	hostedAgentConfigByIDPath     = hostedAgentsConfigsBasePath + "/%s"
	hostedAgentConfigSessionsPath = hostedAgentConfigByIDPath + "/sessions"
)

// HostedAgentConfigCredentialSlot is redacted credential metadata on GET/create.
// It echoes only the manifest's declaration for the slot; secret values are
// never returned. Whether a slot is fulfilled is implied by Source: a
// tenantSecret is always stored at create, and an oauth slot is a declaration
// only until oauth brokering is implemented.
//
// Prefer HostedEnvironmentCredentialSlot; this name remains for compatibility.
type HostedAgentConfigCredentialSlot struct {
	Name     string `json:"name"`
	Source   string `json:"source,omitempty"`
	Provider string `json:"provider,omitempty"`
}

// HostedAgentConfig is an immutable team-scoped environment definition
// (product name: Environment; historically called Agent Config).
// Prefer HostedEnvironment; this name remains for compatibility.
type HostedAgentConfig struct {
	ID                     string                            `json:"id"`
	Name                   string                            `json:"name"`
	AgentSpecSchemaVersion string                            `json:"agentspec_schema_version"`
	Manifest               json.RawMessage                   `json:"manifest"`
	ContentHash            string                            `json:"content_hash"`
	CreatedBy              string                            `json:"created_by"`
	CreatedAt              Timestamp                         `json:"created_at"`
	UpdatedAt              Timestamp                         `json:"updated_at"`
	Credentials            []HostedAgentConfigCredentialSlot `json:"credentials,omitempty"`
	// Warnings carries non-fatal create-time advisories computed from the
	// manifest. It is populated on the create response only.
	Warnings []string `json:"warnings,omitempty"`
}

// HostedAgentConfigSummary is the list view (no manifest / credentials).
// Prefer HostedEnvironmentSummary; this name remains for compatibility.
type HostedAgentConfigSummary struct {
	ID                     string    `json:"id"`
	Name                   string    `json:"name"`
	AgentSpecSchemaVersion string    `json:"agentspec_schema_version"`
	ContentHash            string    `json:"content_hash"`
	CreatedBy              string    `json:"created_by"`
	CreatedAt              Timestamp `json:"created_at"`
	UpdatedAt              Timestamp `json:"updated_at"`
}

// HostedAgentConfigCreateRequest is the body for POST /v2/agents/configs.
// Credentials are not request fields: every secret is declared in the manifest
// under secrets (Environment Spec) or the legacy agents.yaml shape, and a
// tenantSecret slot carries its plaintext in a write-only value that is
// extracted server-side and never persisted or returned. Sending the retired
// secrets or oauth_assignments maps is rejected with a 400.
// Prefer HostedEnvironmentCreateRequest; this name remains for compatibility.
type HostedAgentConfigCreateRequest struct {
	Name         string `json:"name"`
	ManifestYAML string `json:"manifest_yaml"`
}

// HostedAgentConfigListOptions specifies optional list pagination.
// Prefer HostedEnvironmentListOptions; this name remains for compatibility.
type HostedAgentConfigListOptions struct {
	PageToken string `url:"page_token,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
}

// HostedAgentConfigsListResponse is returned by GET /v2/agents/configs.
// The JSON field remains "configs" on the wire. Prefer
// HostedEnvironmentsListResponse; this name remains for compatibility.
type HostedAgentConfigsListResponse struct {
	Configs       []HostedAgentConfigSummary `json:"configs"`
	NextPageToken string                     `json:"next_page_token"`
}

type hostedAgentConfigRoot struct {
	Config *HostedAgentConfig `json:"config"`
}

// Advertised Environment aliases. Prefer these over the HostedAgentConfig*
// names; the Agent Config identifiers remain as compatible aliases.
type (
	HostedEnvironment               = HostedAgentConfig
	HostedEnvironmentCredentialSlot = HostedAgentConfigCredentialSlot
	HostedEnvironmentSummary        = HostedAgentConfigSummary
	HostedEnvironmentCreateRequest  = HostedAgentConfigCreateRequest
	HostedEnvironmentListOptions    = HostedAgentConfigListOptions
	HostedEnvironmentsListResponse  = HostedAgentConfigsListResponse
)

// ListEnvironments lists active Environments for the caller's team.
func (s *HostedAgentsServiceOp) ListEnvironments(ctx context.Context, opt *HostedEnvironmentListOptions) (*HostedEnvironmentsListResponse, *Response, error) {
	return s.ListAgentConfigs(ctx, opt)
}

// GetEnvironment returns one Environment with redacted credential slots.
func (s *HostedAgentsServiceOp) GetEnvironment(ctx context.Context, environmentID string) (*HostedEnvironment, *Response, error) {
	return s.GetAgentConfig(ctx, environmentID)
}

// CreateEnvironment creates an immutable Environment from a name and
// environment spec YAML. Secret values belong in the manifest's secrets map
// (or legacy agents.yaml secret slots), not as separate request fields.
func (s *HostedAgentsServiceOp) CreateEnvironment(ctx context.Context, create *HostedEnvironmentCreateRequest) (*HostedEnvironment, *Response, error) {
	return s.CreateAgentConfig(ctx, create)
}

// DeleteEnvironment soft-deletes an Environment. The API returns HTTP 204.
func (s *HostedAgentsServiceOp) DeleteEnvironment(ctx context.Context, environmentID string) (*Response, error) {
	return s.DeleteAgentConfig(ctx, environmentID)
}

// ListEnvironmentSessions lists sessions created from an Environment.
func (s *HostedAgentsServiceOp) ListEnvironmentSessions(ctx context.Context, environmentID string, opt *HostedAgentSessionListOptions) (*HostedAgentSessionsListResponse, *Response, error) {
	return s.ListAgentConfigSessions(ctx, environmentID, opt)
}

// ListAgentConfigs lists active Environments for the caller's team.
// Prefer ListEnvironments; this name remains for compatibility.
func (s *HostedAgentsServiceOp) ListAgentConfigs(ctx context.Context, opt *HostedAgentConfigListOptions) (*HostedAgentConfigsListResponse, *Response, error) {
	path, err := addOptions(hostedAgentsConfigsBasePath, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentConfigsListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetAgentConfig returns one Environment with redacted credential slots.
// Prefer GetEnvironment; this name remains for compatibility.
func (s *HostedAgentsServiceOp) GetAgentConfig(ctx context.Context, configID string) (*HostedAgentConfig, *Response, error) {
	if configID == "" {
		return nil, nil, errors.New("hosted agents: config id is required")
	}
	path := fmt.Sprintf(hostedAgentConfigByIDPath, configID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentConfigRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Config == nil {
		return nil, resp, errors.New("hosted agents: get config returned no config")
	}
	return root.Config, resp, nil
}

// CreateAgentConfig creates an immutable Environment from a name and
// environment spec YAML. Prefer CreateEnvironment; this name remains for
// compatibility. Secret values belong in the manifest, not as separate
// request fields.
func (s *HostedAgentsServiceOp) CreateAgentConfig(ctx context.Context, create *HostedAgentConfigCreateRequest) (*HostedAgentConfig, *Response, error) {
	if create == nil {
		return nil, nil, errors.New("hosted agents: create request is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, hostedAgentsConfigsBasePath, create)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentConfigRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Config == nil {
		return nil, resp, errors.New("hosted agents: create config returned no config")
	}
	return root.Config, resp, nil
}

// DeleteAgentConfig soft-deletes an Environment. The API returns HTTP 204.
// Prefer DeleteEnvironment; this name remains for compatibility.
func (s *HostedAgentsServiceOp) DeleteAgentConfig(ctx context.Context, configID string) (*Response, error) {
	if configID == "" {
		return nil, errors.New("hosted agents: config id is required")
	}
	path := fmt.Sprintf(hostedAgentConfigByIDPath, configID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, nil)
}

// ListAgentConfigSessions lists sessions created from an Environment.
// Prefer ListEnvironmentSessions; this name remains for compatibility.
func (s *HostedAgentsServiceOp) ListAgentConfigSessions(ctx context.Context, configID string, opt *HostedAgentSessionListOptions) (*HostedAgentSessionsListResponse, *Response, error) {
	if configID == "" {
		return nil, nil, errors.New("hosted agents: config id is required")
	}
	path := fmt.Sprintf(hostedAgentConfigSessionsPath, configID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentSessionsListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}
