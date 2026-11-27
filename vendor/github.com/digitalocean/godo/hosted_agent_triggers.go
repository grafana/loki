package godo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

const (
	hostedAgentTriggersBasePath           = "/v2/agents/triggers"
	hostedAgentTriggerByIDPath            = hostedAgentTriggersBasePath + "/%s"
	hostedAgentTriggerRotateSecretPath    = hostedAgentTriggerByIDPath + "/rotate-secret"
	hostedAgentTriggerExecutionsPath      = hostedAgentTriggerByIDPath + "/executions"
	hostedAgentTriggerExecutionByIDPath   = hostedAgentTriggerExecutionsPath + "/%s"
	hostedAgentTriggerExecutionCancelPath = hostedAgentTriggerExecutionByIDPath + "/cancel"
	hostedAgentTriggerBySessionPath       = hostedAgentTriggersBasePath + "/by-session/%s"
	hostedAgentReusableSessionsPath       = hostedAgentTriggersBasePath + "/reusable-sessions"
	hostedAgentWebhookProvidersPath       = "/v2/agents/webhook-providers"
)

// HostedAgentTriggersService exposes the Hosted Agents Triggers public REST API
// (webhook & cron triggers for agent runs) under /v2/agents/triggers and
// /v2/agents/webhook-providers.
type HostedAgentTriggersService interface {
	List(context.Context, *HostedAgentTriggerListOptions) (*HostedAgentTriggersListResponse, *Response, error)
	Create(context.Context, *HostedAgentTriggerCreateRequest) (*HostedAgentTriggerCreateResponse, *Response, error)
	Get(context.Context, string) (*HostedAgentTrigger, *Response, error)
	Update(context.Context, string, *HostedAgentTriggerUpdateRequest) (*HostedAgentTrigger, *Response, error)
	Delete(context.Context, string) (*Response, error)
	RotateSecret(context.Context, string, *HostedAgentTriggerRotateSecretOptions) (*HostedAgentTriggerRotateSecretResponse, *Response, error)
	ListExecutions(context.Context, string, *HostedAgentTriggerExecutionListOptions) (*HostedAgentTriggerExecutionsListResponse, *Response, error)
	GetExecution(context.Context, string, string) (*HostedAgentTriggerExecution, *Response, error)
	Cancel(context.Context, string, string, *HostedAgentTriggerCancelExecutionOptions) (*HostedAgentTriggerExecution, *Response, error)
	GetBySession(context.Context, string) (*HostedAgentTrigger, *Response, error)
	ListReusableSessions(context.Context, *HostedAgentReusableSessionListOptions) (*HostedAgentReusableSessionsListResponse, *Response, error)
	ListWebhookProviders(context.Context) (*HostedAgentWebhookProvidersListResponse, *Response, error)
}

// HostedAgentTriggersServiceOp handles communication with Hosted Agents Trigger methods.
type HostedAgentTriggersServiceOp struct {
	client *Client
}

var _ HostedAgentTriggersService = &HostedAgentTriggersServiceOp{}

// HostedAgentTriggerKind is how a trigger fires.
type HostedAgentTriggerKind string

const (
	HostedAgentTriggerKindWebhook HostedAgentTriggerKind = "webhook"
	HostedAgentTriggerKindCron    HostedAgentTriggerKind = "cron"
)

// HostedAgentTriggerStatus is the trigger lifecycle state.
type HostedAgentTriggerStatus string

const (
	HostedAgentTriggerStatusActive HostedAgentTriggerStatus = "active"
	HostedAgentTriggerStatusPaused HostedAgentTriggerStatus = "paused"
)

// HostedAgentTriggerSessionMode controls whether each firing creates a new
// session or reuses a bound paused session.
type HostedAgentTriggerSessionMode string

const (
	HostedAgentTriggerSessionModeFresh HostedAgentTriggerSessionMode = "fresh"
	HostedAgentTriggerSessionModeReuse HostedAgentTriggerSessionMode = "reuse"
)

// HostedAgentTriggerOutputMode is how run output is delivered.
type HostedAgentTriggerOutputMode string

const (
	HostedAgentTriggerOutputModeNone  HostedAgentTriggerOutputMode = "none"
	HostedAgentTriggerOutputModeEmail HostedAgentTriggerOutputMode = "email"
	// HostedAgentTriggerOutputModeSlack delivers run output to a Slack incoming webhook.
	HostedAgentTriggerOutputModeSlack HostedAgentTriggerOutputMode = "slack"
)

// HostedAgentWebhookProviderKey identifies the signature-verification scheme.
type HostedAgentWebhookProviderKey string

const (
	HostedAgentWebhookProviderGitHub HostedAgentWebhookProviderKey = "github"
	HostedAgentWebhookProviderGitLab HostedAgentWebhookProviderKey = "gitlab"
	HostedAgentWebhookProviderCustom HostedAgentWebhookProviderKey = "custom"
)

// HostedAgentTriggerExecutionStatus is the per-firing outcome.
type HostedAgentTriggerExecutionStatus string

const (
	HostedAgentTriggerExecutionStatusPending   HostedAgentTriggerExecutionStatus = "pending"
	HostedAgentTriggerExecutionStatusRunning   HostedAgentTriggerExecutionStatus = "running"
	HostedAgentTriggerExecutionStatusSucceeded HostedAgentTriggerExecutionStatus = "succeeded"
	HostedAgentTriggerExecutionStatusFailed    HostedAgentTriggerExecutionStatus = "failed"
)

// HostedAgentWebhookSignatureScheme is how an external system authenticates a delivery.
type HostedAgentWebhookSignatureScheme string

const (
	HostedAgentWebhookSignatureHMACSHA256 HostedAgentWebhookSignatureScheme = "hmac-sha256"
	HostedAgentWebhookSignaturePlaintext  HostedAgentWebhookSignatureScheme = "plaintext"
)

// HostedAgentTrigger is a webhook or cron automation bound to an agent/session.
type HostedAgentTrigger struct {
	TriggerID       string                        `json:"trigger_id,omitempty"`
	TeamID          int64                         `json:"team_id,omitempty"`
	Kind            HostedAgentTriggerKind        `json:"kind,omitempty"`
	Name            string                        `json:"name,omitempty"`
	Status          HostedAgentTriggerStatus      `json:"status,omitempty"`
	SessionMode     HostedAgentTriggerSessionMode `json:"session_mode,omitempty"`
	AgentKind       HostedAgentKind               `json:"agent_kind,omitempty"`
	PromptTemplate  string                        `json:"prompt_template,omitempty"`
	Output          *HostedAgentTriggerOutputRead `json:"output,omitempty"`
	SessionTemplate string                        `json:"session_template,omitempty"`
	BoundSessionID  string                        `json:"bound_session_id,omitempty"`
	Webhook         *HostedAgentWebhookConfig     `json:"webhook,omitempty"`
	Cron            *HostedAgentCronConfig        `json:"cron,omitempty"`
	CreatedAt       Timestamp                     `json:"created_at,omitempty"`
	UpdatedAt       Timestamp                     `json:"updated_at,omitempty"`
}

// HostedAgentTriggerOutputRead is output-delivery config as returned on reads.
// Email addresses and Slack webhook URLs are write-only and never returned.
type HostedAgentTriggerOutputRead struct {
	Mode            HostedAgentTriggerOutputMode `json:"mode,omitempty"`
	EmailConfigured bool                         `json:"email_configured,omitempty"`
	SlackConfigured bool                         `json:"slack_configured,omitempty"`
}

// HostedAgentTriggerOutputWrite is output-delivery config on create/update.
// Email is required when mode=email; Slack is required when mode=slack.
// Destinations are stored encrypted and never returned on reads.
type HostedAgentTriggerOutputWrite struct {
	Mode  HostedAgentTriggerOutputMode        `json:"mode"`
	Email string                              `json:"email,omitempty"`
	Slack *HostedAgentTriggerSlackOutputWrite `json:"slack,omitempty"`
}

// HostedAgentTriggerSlackOutputWrite is the Slack destination on create/update.
type HostedAgentTriggerSlackOutputWrite struct {
	WebhookURL string `json:"webhook_url"`
}

// HostedAgentWebhookConfig is present when kind=webhook.
type HostedAgentWebhookConfig struct {
	Provider   HostedAgentWebhookProviderKey `json:"provider,omitempty"`
	WebhookURL string                        `json:"webhook_url,omitempty"`
}

// HostedAgentCronConfig is present when kind=cron.
type HostedAgentCronConfig struct {
	CronExpr  string    `json:"cron_expr,omitempty"`
	Timezone  string    `json:"timezone,omitempty"`
	NextRunAt Timestamp `json:"next_run_at,omitempty"`
}

// HostedAgentTriggerCreateRequest creates a webhook or cron trigger.
type HostedAgentTriggerCreateRequest struct {
	Kind            HostedAgentTriggerKind          `json:"kind"`
	Name            string                          `json:"name"`
	SessionMode     HostedAgentTriggerSessionMode   `json:"session_mode"`
	PromptTemplate  string                          `json:"prompt_template"`
	Output          HostedAgentTriggerOutputWrite   `json:"output"`
	SessionTemplate string                          `json:"session_template,omitempty"`
	BoundSessionID  string                          `json:"bound_session_id,omitempty"`
	Webhook         *HostedAgentCreateWebhookConfig `json:"webhook,omitempty"`
	Cron            *HostedAgentCreateCronConfig    `json:"cron,omitempty"`
}

// HostedAgentCreateWebhookConfig is the webhook block on create.
type HostedAgentCreateWebhookConfig struct {
	Provider HostedAgentWebhookProviderKey `json:"provider,omitempty"`
}

// HostedAgentCreateCronConfig is the cron block on create/update.
type HostedAgentCreateCronConfig struct {
	CronExpr string `json:"cron_expr"`
	Timezone string `json:"timezone"`
}

// HostedAgentTriggerUpdateRequest is a partial update (pause/re-enable via status).
type HostedAgentTriggerUpdateRequest struct {
	Name            string                         `json:"name,omitempty"`
	Status          HostedAgentTriggerStatus       `json:"status,omitempty"`
	PromptTemplate  string                         `json:"prompt_template,omitempty"`
	Output          *HostedAgentTriggerOutputWrite `json:"output,omitempty"`
	SessionTemplate string                         `json:"session_template,omitempty"`
	BoundSessionID  string                         `json:"bound_session_id,omitempty"`
	Cron            *HostedAgentCreateCronConfig   `json:"cron,omitempty"`
}

// HostedAgentTriggerCreateResponse is returned on create.
// WebhookSecret is present for webhook triggers only and shown exactly once.
type HostedAgentTriggerCreateResponse struct {
	Trigger       *HostedAgentTrigger `json:"trigger,omitempty"`
	WebhookSecret string              `json:"webhook_secret,omitempty"`
}

// HostedAgentTriggerListOptions specifies optional list filters and pagination.
type HostedAgentTriggerListOptions struct {
	PageSize  int                      `url:"page_size,omitempty"`
	PageToken string                   `url:"page_token,omitempty"`
	Kind      HostedAgentTriggerKind   `url:"kind,omitempty"`
	Status    HostedAgentTriggerStatus `url:"status,omitempty"`
}

// HostedAgentTriggersListResponse is returned by List.
type HostedAgentTriggersListResponse struct {
	Triggers      []HostedAgentTrigger `json:"triggers"`
	NextPageToken string               `json:"next_page_token,omitempty"`
}

// HostedAgentTriggerRotateSecretOptions specifies optional rotation behaviour.
// A nil options pointer, or GracePeriodSeconds left nil, selects the server
// default (5 minutes). Pass a pointer to 0 to revoke the outgoing secret
// immediately; any positive value is a custom handoff window in seconds
// (server-enforced max, default 1 hour).
type HostedAgentTriggerRotateSecretOptions struct {
	// GracePeriodSeconds is how long the outgoing secret keeps verifying.
	// Nil omits the query parameter (server default). Non-nil 0 revokes now.
	GracePeriodSeconds *int `url:"grace_period_seconds,omitempty"`
}

// HostedAgentTriggerRotateSecretResponse is returned by RotateSecret.
// Exactly one of PreviousSecretExpiresAt and PreviousSecretRevoked is set, so a
// caller never has to infer the outcome from a missing field.
type HostedAgentTriggerRotateSecretResponse struct {
	WebhookSecret string `json:"webhook_secret,omitempty"`
	// PreviousSecretExpiresAt is when the outgoing secret stops verifying
	// deliveries. Nil when the secret was revoked on the rotate call.
	PreviousSecretExpiresAt *Timestamp `json:"previous_secret_expires_at,omitempty"`
	// PreviousSecretRevoked reports that the outgoing secret is already dead.
	PreviousSecretRevoked bool `json:"previous_secret_revoked,omitempty"`
}

// HostedAgentTriggerExecution is one row per firing.
type HostedAgentTriggerExecution struct {
	ExecutionID     string                            `json:"execution_id,omitempty"`
	TriggerID       string                            `json:"trigger_id,omitempty"`
	Status          HostedAgentTriggerExecutionStatus `json:"status,omitempty"`
	SessionID       string                            `json:"session_id,omitempty"`
	RunID           string                            `json:"run_id,omitempty"`
	FailureReason   string                            `json:"failure_reason,omitempty"`
	CreatedAt       Timestamp                         `json:"created_at,omitempty"`
	UpdatedAt       Timestamp                         `json:"updated_at,omitempty"`
	Payload         string                            `json:"payload,omitempty"`
	OutputText      string                            `json:"output_text,omitempty"`
	OutputTruncated bool                              `json:"output_truncated,omitempty"`
}

// HostedAgentTriggerExecutionListOptions specifies optional execution list filters.
type HostedAgentTriggerExecutionListOptions struct {
	PageSize  int                               `url:"page_size,omitempty"`
	PageToken string                            `url:"page_token,omitempty"`
	Status    HostedAgentTriggerExecutionStatus `url:"status,omitempty"`
}

// HostedAgentTriggerExecutionsListResponse is returned by ListExecutions.
type HostedAgentTriggerExecutionsListResponse struct {
	Executions    []HostedAgentTriggerExecution `json:"executions"`
	NextPageToken string                        `json:"next_page_token,omitempty"`
}

// HostedAgentTriggerCancelExecutionOptions specifies optional cancel behaviour.
type HostedAgentTriggerCancelExecutionOptions struct {
	// Force cancels even if the execution has not started a run yet (the
	// worker's own dispatch may still be in flight). Without this, that
	// case returns 409; the server-side reclaim sweep clears it
	// automatically within 15m regardless.
	Force bool `url:"force,omitempty"`
}

// HostedAgentWebhookProviderSignature describes how a provider signs deliveries.
type HostedAgentWebhookProviderSignature struct {
	Header string                            `json:"header,omitempty"`
	Scheme HostedAgentWebhookSignatureScheme `json:"scheme,omitempty"`
}

// HostedAgentWebhookProvider is a static registry entry for the create-trigger UI.
type HostedAgentWebhookProvider struct {
	Key         HostedAgentWebhookProviderKey        `json:"key,omitempty"`
	DisplayName string                               `json:"display_name,omitempty"`
	Description string                               `json:"description,omitempty"`
	DocsURL     string                               `json:"docs_url,omitempty"`
	PasteHint   string                               `json:"paste_hint,omitempty"`
	Signature   *HostedAgentWebhookProviderSignature `json:"signature,omitempty"`
}

// HostedAgentWebhookProvidersListResponse is returned by ListWebhookProviders.
type HostedAgentWebhookProvidersListResponse struct {
	Providers []HostedAgentWebhookProvider `json:"providers"`
}

// HostedAgentReusableSession is a paused session suitable for session_mode=reuse.
type HostedAgentReusableSession struct {
	SessionID   string                   `json:"session_id,omitempty"`
	Name        string                   `json:"name,omitempty"`
	AgentKind   HostedAgentKind          `json:"agent_kind,omitempty"`
	Status      HostedAgentSessionStatus `json:"status,omitempty"`
	CreatedAt   Timestamp                `json:"created_at,omitempty"`
	LastEventAt Timestamp                `json:"last_event_at,omitempty"`
}

// HostedAgentReusableSessionListOptions specifies pagination for reusable sessions.
type HostedAgentReusableSessionListOptions struct {
	PageSize  int    `url:"page_size,omitempty"`
	PageToken string `url:"page_token,omitempty"`
}

// HostedAgentReusableSessionsListResponse is returned by ListReusableSessions.
type HostedAgentReusableSessionsListResponse struct {
	Sessions      []HostedAgentReusableSession `json:"sessions"`
	NextPageToken string                       `json:"next_page_token,omitempty"`
}

type hostedAgentTriggerRoot struct {
	Trigger *HostedAgentTrigger `json:"trigger"`
}

type hostedAgentTriggerExecutionRoot struct {
	Execution *HostedAgentTriggerExecution `json:"execution"`
}

// List enumerates the calling team's triggers.
func (s *HostedAgentTriggersServiceOp) List(ctx context.Context, opt *HostedAgentTriggerListOptions) (*HostedAgentTriggersListResponse, *Response, error) {
	path, err := addOptions(hostedAgentTriggersBasePath, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentTriggersListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// Create creates a webhook or cron trigger.
// For webhook triggers, WebhookSecret is returned exactly once.
func (s *HostedAgentTriggersServiceOp) Create(ctx context.Context, create *HostedAgentTriggerCreateRequest) (*HostedAgentTriggerCreateResponse, *Response, error) {
	if create == nil {
		return nil, nil, NewArgError("create", "cannot be nil")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, hostedAgentTriggersBasePath, create)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentTriggerCreateResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// Get returns a single trigger by ID.
func (s *HostedAgentTriggersServiceOp) Get(ctx context.Context, triggerID string) (*HostedAgentTrigger, *Response, error) {
	if triggerID == "" {
		return nil, nil, errors.New("hosted agent triggers: trigger id is required")
	}
	path := fmt.Sprintf(hostedAgentTriggerByIDPath, triggerID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTriggerRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Trigger == nil {
		return nil, resp, errors.New("hosted agent triggers: get returned no trigger")
	}
	return root.Trigger, resp, nil
}

// Update partially updates, pauses, or re-enables a trigger.
func (s *HostedAgentTriggersServiceOp) Update(ctx context.Context, triggerID string, update *HostedAgentTriggerUpdateRequest) (*HostedAgentTrigger, *Response, error) {
	if triggerID == "" {
		return nil, nil, errors.New("hosted agent triggers: trigger id is required")
	}
	if update == nil {
		return nil, nil, NewArgError("update", "cannot be nil")
	}
	path := fmt.Sprintf(hostedAgentTriggerByIDPath, triggerID)
	req, err := s.client.NewRequest(ctx, http.MethodPatch, path, update)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTriggerRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Trigger == nil {
		return nil, resp, errors.New("hosted agent triggers: update returned no trigger")
	}
	return root.Trigger, resp, nil
}

// Delete soft-deletes a trigger. The API returns HTTP 204 on success.
func (s *HostedAgentTriggersServiceOp) Delete(ctx context.Context, triggerID string) (*Response, error) {
	if triggerID == "" {
		return nil, errors.New("hosted agent triggers: trigger id is required")
	}
	path := fmt.Sprintf(hostedAgentTriggerByIDPath, triggerID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, nil)
}

// RotateSecret issues a new webhook secret (webhook triggers only).
// By default (nil options, or GracePeriodSeconds left nil) the outgoing secret
// keeps verifying for the server default window (5 minutes) and
// PreviousSecretExpiresAt reports when it dies. Pass GracePeriodSeconds pointing
// at 0 to revoke it on this call, or at a positive value for a custom window.
func (s *HostedAgentTriggersServiceOp) RotateSecret(ctx context.Context, triggerID string, opt *HostedAgentTriggerRotateSecretOptions) (*HostedAgentTriggerRotateSecretResponse, *Response, error) {
	if triggerID == "" {
		return nil, nil, errors.New("hosted agent triggers: trigger id is required")
	}
	path := fmt.Sprintf(hostedAgentTriggerRotateSecretPath, triggerID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, struct{}{})
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentTriggerRotateSecretResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListExecutions returns a trigger's execution history (payload omitted from list items).
func (s *HostedAgentTriggersServiceOp) ListExecutions(ctx context.Context, triggerID string, opt *HostedAgentTriggerExecutionListOptions) (*HostedAgentTriggerExecutionsListResponse, *Response, error) {
	if triggerID == "" {
		return nil, nil, errors.New("hosted agent triggers: trigger id is required")
	}
	path := fmt.Sprintf(hostedAgentTriggerExecutionsPath, triggerID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentTriggerExecutionsListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetExecution returns a single execution, including its payload.
func (s *HostedAgentTriggersServiceOp) GetExecution(ctx context.Context, triggerID, executionID string) (*HostedAgentTriggerExecution, *Response, error) {
	if triggerID == "" {
		return nil, nil, errors.New("hosted agent triggers: trigger id is required")
	}
	if executionID == "" {
		return nil, nil, errors.New("hosted agent triggers: execution id is required")
	}
	path := fmt.Sprintf(hostedAgentTriggerExecutionByIDPath, triggerID, executionID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTriggerExecutionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Execution == nil {
		return nil, resp, errors.New("hosted agent triggers: get execution returned no execution")
	}
	return root.Execution, resp, nil
}

// Cancel ends a running execution: fails it and frees the trigger's
// in-flight slot. A fresh-mode session is destroyed outright; a reuse-mode
// session is only paused, since the API has no run-interrupt call -- the
// agent's turn may still be in flight server-side. Cancelling a
// non-running execution is a no-op that still succeeds. See
// HostedAgentTriggerCancelExecutionOptions.Force for the 409 this can
// return when the execution has not started a run yet.
func (s *HostedAgentTriggersServiceOp) Cancel(ctx context.Context, triggerID, executionID string, opt *HostedAgentTriggerCancelExecutionOptions) (*HostedAgentTriggerExecution, *Response, error) {
	if triggerID == "" {
		return nil, nil, errors.New("hosted agent triggers: trigger id is required")
	}
	if executionID == "" {
		return nil, nil, errors.New("hosted agent triggers: execution id is required")
	}
	path := fmt.Sprintf(hostedAgentTriggerExecutionCancelPath, triggerID, executionID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTriggerExecutionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Execution == nil {
		return nil, resp, errors.New("hosted agent triggers: cancel returned no execution")
	}
	return root.Execution, resp, nil
}

// GetBySession reverse-looks-up the trigger that produced or binds a session.
func (s *HostedAgentTriggersServiceOp) GetBySession(ctx context.Context, sessionID string) (*HostedAgentTrigger, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agent triggers: session id is required")
	}
	path := fmt.Sprintf(hostedAgentTriggerBySessionPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentTriggerRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Trigger == nil {
		return nil, resp, errors.New("hosted agent triggers: get by session returned no trigger")
	}
	return root.Trigger, resp, nil
}

// ListReusableSessions lists the team's PAUSED sessions for the reuse picker.
func (s *HostedAgentTriggersServiceOp) ListReusableSessions(ctx context.Context, opt *HostedAgentReusableSessionListOptions) (*HostedAgentReusableSessionsListResponse, *Response, error) {
	path, err := addOptions(hostedAgentReusableSessionsPath, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentReusableSessionsListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListWebhookProviders lists supported webhook providers for the create-trigger UI.
func (s *HostedAgentTriggersServiceOp) ListWebhookProviders(ctx context.Context) (*HostedAgentWebhookProvidersListResponse, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, hostedAgentWebhookProvidersPath, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentWebhookProvidersListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}
