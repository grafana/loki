package godo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

const (
	hostedAgentsTemplatesBasePath        = "/v2/agents/templates"
	hostedAgentTemplateByIDPath          = hostedAgentsTemplatesBasePath + "/%s"
	hostedAgentTemplateBuildsPath        = hostedAgentTemplateByIDPath + "/builds"
	hostedAgentTemplateBuildByIDPath     = hostedAgentTemplateBuildsPath + "/%s"
	hostedAgentTemplateBuildLogsByIDPath = hostedAgentTemplateBuildByIDPath + "/logs"
)

// HostedAgentTemplateStatus is the lifecycle status of a team custom template.
type HostedAgentTemplateStatus string

const (
	HostedAgentTemplateStatusUnspecified HostedAgentTemplateStatus = "TEMPLATE_STATUS_UNSPECIFIED"
	HostedAgentTemplateStatusPending     HostedAgentTemplateStatus = "TEMPLATE_STATUS_PENDING"
	HostedAgentTemplateStatusBuilding    HostedAgentTemplateStatus = "TEMPLATE_STATUS_BUILDING"
	HostedAgentTemplateStatusReady       HostedAgentTemplateStatus = "TEMPLATE_STATUS_READY"
	HostedAgentTemplateStatusFailed      HostedAgentTemplateStatus = "TEMPLATE_STATUS_FAILED"
)

// HostedAgentTemplateBuildStatus is the lifecycle status of a template build.
type HostedAgentTemplateBuildStatus string

const (
	HostedAgentTemplateBuildStatusUnspecified HostedAgentTemplateBuildStatus = "STATUS_UNSPECIFIED"
	HostedAgentTemplateBuildStatusPending     HostedAgentTemplateBuildStatus = "PENDING"
	HostedAgentTemplateBuildStatusBuilding    HostedAgentTemplateBuildStatus = "BUILDING"
	HostedAgentTemplateBuildStatusSucceeded   HostedAgentTemplateBuildStatus = "SUCCEEDED"
	HostedAgentTemplateBuildStatusFailed      HostedAgentTemplateBuildStatus = "FAILED"
)

// HostedAgentTemplateImageSource is the customer OCI image used as template input.
type HostedAgentTemplateImageSource struct {
	Registry   string `json:"registry,omitempty"`
	Repository string `json:"repository,omitempty"`
	Tag        string `json:"tag,omitempty"`
	Digest     string `json:"digest,omitempty"`
}

// HostedAgentTemplateSpec is the resolved template source.
type HostedAgentTemplateSpec struct {
	// BaseTemplate is the platform base key: coding-base | coding-codex | coding-opencode.
	BaseTemplate string                          `json:"base_template,omitempty"`
	Image        *HostedAgentTemplateImageSource `json:"image,omitempty"`
}

// HostedAgentTemplate is a team-owned custom sandbox template.
type HostedAgentTemplate struct {
	TemplateID string                    `json:"template_id"`
	Name       string                    `json:"name"`
	Spec       *HostedAgentTemplateSpec  `json:"spec,omitempty"`
	Status     HostedAgentTemplateStatus `json:"status"`
	CreatedAt  Timestamp                 `json:"created_at"`
	UpdatedAt  Timestamp                 `json:"updated_at"`
}

// HostedAgentTemplateBuild is one immutable build of a template.
type HostedAgentTemplateBuild struct {
	BuildID    string                         `json:"build_id"`
	TemplateID string                         `json:"template_id,omitempty"`
	Name       string                         `json:"name,omitempty"`
	Spec       *HostedAgentTemplateSpec       `json:"spec,omitempty"`
	Status     HostedAgentTemplateBuildStatus `json:"status"`
	Error      string                         `json:"error,omitempty"`
	CreatedAt  Timestamp                      `json:"created_at"`
	UpdatedAt  Timestamp                      `json:"updated_at"`
}

// HostedAgentTemplateCreateRequest is the body for POST /v2/agents/templates.
type HostedAgentTemplateCreateRequest struct {
	Name         string `json:"name"`
	BaseTemplate string `json:"base_template"`
	SourceOCIRef string `json:"source_oci_ref"`
}

// HostedAgentTemplateUpdateRequest is the body for PUT /v2/agents/templates/{template_id}.
// Updating source kicks a new build.
type HostedAgentTemplateUpdateRequest struct {
	SourceOCIRef string `json:"source_oci_ref,omitempty"`
	BaseTemplate string `json:"base_template,omitempty"`
}

// HostedAgentTemplateListOptions paginates ListTemplates.
type HostedAgentTemplateListOptions struct {
	PageToken string `url:"page_token,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
}

// HostedAgentTemplatesListResponse is returned by GET /v2/agents/templates.
type HostedAgentTemplatesListResponse struct {
	Templates     []HostedAgentTemplate `json:"templates"`
	NextPageToken string                `json:"next_page_token,omitempty"`
}

// HostedAgentTemplateDeleteResponse is returned by DELETE /v2/agents/templates/{template_id}.
type HostedAgentTemplateDeleteResponse struct {
	TemplateID string `json:"template_id"`
	Deleted    bool   `json:"deleted"`
}

// HostedAgentTemplateBuildListOptions paginates ListTemplateBuilds.
type HostedAgentTemplateBuildListOptions struct {
	PageToken string `url:"page_token,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
}

// HostedAgentTemplateBuildsListResponse is returned by GET .../builds.
type HostedAgentTemplateBuildsListResponse struct {
	Builds        []HostedAgentTemplateBuild `json:"builds"`
	NextPageToken string                     `json:"next_page_token,omitempty"`
}

// HostedAgentTemplateBuildLogs is returned by GET .../builds/{build_id}/logs.
type HostedAgentTemplateBuildLogs struct {
	// SignedURL is a short-lived (15-minute) pre-signed Spaces GET URL.
	SignedURL string `json:"signed_url"`
}

type hostedAgentTemplateRoot struct {
	Template *HostedAgentTemplate `json:"template"`
}

type hostedAgentTemplateBuildRoot struct {
	Build *HostedAgentTemplateBuild `json:"build"`
}

// ListTemplates returns the caller's team custom templates.
func (s *HostedAgentsServiceOp) ListTemplates(ctx context.Context, opt *HostedAgentTemplateListOptions) (*HostedAgentTemplatesListResponse, *Response, error) {
	path, err := addOptions(hostedAgentsTemplatesBasePath, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentTemplatesListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CreateTemplate creates a team custom template and kicks a build.
func (s *HostedAgentsServiceOp) CreateTemplate(ctx context.Context, create *HostedAgentTemplateCreateRequest) (*HostedAgentTemplate, *Response, error) {
	if create == nil {
		return nil, nil, errors.New("hosted agents: create template request is required")
	}
	if create.Name == "" {
		return nil, nil, errors.New("hosted agents: name is required")
	}
	if create.BaseTemplate == "" {
		return nil, nil, errors.New("hosted agents: base_template is required")
	}
	if create.SourceOCIRef == "" {
		return nil, nil, errors.New("hosted agents: source_oci_ref is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, hostedAgentsTemplatesBasePath, create)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTemplateRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Template == nil {
		return nil, resp, errors.New("hosted agents: create template returned no template")
	}
	return root.Template, resp, nil
}

// GetTemplate returns a team custom template by public id.
func (s *HostedAgentsServiceOp) GetTemplate(ctx context.Context, templateID string) (*HostedAgentTemplate, *Response, error) {
	if templateID == "" {
		return nil, nil, errors.New("hosted agents: template id is required")
	}
	path := fmt.Sprintf(hostedAgentTemplateByIDPath, templateID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTemplateRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Template == nil {
		return nil, resp, errors.New("hosted agents: get template returned no template")
	}
	return root.Template, resp, nil
}

// UpdateTemplate updates a team template's source and kicks a new build.
func (s *HostedAgentsServiceOp) UpdateTemplate(ctx context.Context, templateID string, update *HostedAgentTemplateUpdateRequest) (*HostedAgentTemplate, *Response, error) {
	if templateID == "" {
		return nil, nil, errors.New("hosted agents: template id is required")
	}
	if update == nil {
		return nil, nil, errors.New("hosted agents: update template request is required")
	}
	path := fmt.Sprintf(hostedAgentTemplateByIDPath, templateID)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, update)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTemplateRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Template == nil {
		return nil, resp, errors.New("hosted agents: update template returned no template")
	}
	return root.Template, resp, nil
}

// DeleteTemplate deletes a team custom template.
func (s *HostedAgentsServiceOp) DeleteTemplate(ctx context.Context, templateID string) (*HostedAgentTemplateDeleteResponse, *Response, error) {
	if templateID == "" {
		return nil, nil, errors.New("hosted agents: template id is required")
	}
	path := fmt.Sprintf(hostedAgentTemplateByIDPath, templateID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentTemplateDeleteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListTemplateBuilds returns build history for a template.
func (s *HostedAgentsServiceOp) ListTemplateBuilds(ctx context.Context, templateID string, opt *HostedAgentTemplateBuildListOptions) (*HostedAgentTemplateBuildsListResponse, *Response, error) {
	if templateID == "" {
		return nil, nil, errors.New("hosted agents: template id is required")
	}
	path := fmt.Sprintf(hostedAgentTemplateBuildsPath, templateID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentTemplateBuildsListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetTemplateBuild returns a single template build.
func (s *HostedAgentsServiceOp) GetTemplateBuild(ctx context.Context, templateID, buildID string) (*HostedAgentTemplateBuild, *Response, error) {
	if templateID == "" {
		return nil, nil, errors.New("hosted agents: template id is required")
	}
	if buildID == "" {
		return nil, nil, errors.New("hosted agents: build id is required")
	}
	path := fmt.Sprintf(hostedAgentTemplateBuildByIDPath, templateID, buildID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTemplateBuildRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Build == nil {
		return nil, resp, errors.New("hosted agents: get template build returned no build")
	}
	return root.Build, resp, nil
}

// GetTemplateBuildLogs returns a short-lived signed URL for archived build logs.
func (s *HostedAgentsServiceOp) GetTemplateBuildLogs(ctx context.Context, templateID, buildID string) (*HostedAgentTemplateBuildLogs, *Response, error) {
	if templateID == "" {
		return nil, nil, errors.New("hosted agents: template id is required")
	}
	if buildID == "" {
		return nil, nil, errors.New("hosted agents: build id is required")
	}
	path := fmt.Sprintf(hostedAgentTemplateBuildLogsByIDPath, templateID, buildID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentTemplateBuildLogs)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}
