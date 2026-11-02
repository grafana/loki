package godo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	signalsBasePath = "v1/signals"
	consentBasePath = "v1/consent"
)

// SignalsService is the MCP v1 client: GETs on /v1/signals plus consent
// enable/disable on /v1/consent (consent-gateway), and on-demand data
// deletion jobs on /v1/signals/deletions.
type SignalsService interface {
	ListConsents(context.Context) (*SignalsListConsentsResponse, *Response, error)
	ListConsentsWithOptions(context.Context, *SignalsListConsentsOptions) (*SignalsListConsentsResponse, *Response, error)
	GetAgentConsent(context.Context, string) (*SignalsAgentConsent, *Response, error)
	SetAgentConsent(context.Context, string, bool) (*SignalsConsentRecord, *Response, error)
	GetInferenceConsent(context.Context) (*SignalsConsentRecord, *Response, error)
	SetInferenceConsent(context.Context, bool) (*SignalsConsentRecord, *Response, error)
	ListAgentSessions(context.Context, string, *SignalsListAgentSessionsOptions) (*SignalsListAgentSessionsResponse, *Response, error)
	ListSessionDialogues(context.Context, string, *SignalsListDialoguesOptions) (*SignalsSessionDialoguesResponse, *Response, error)
	CreateExport(context.Context, *SignalsCreateExportRequest) (*SignalsExportJob, *Response, error)
	ListExports(context.Context, *SignalsListExportsOptions) (*SignalsListExportsResponse, *Response, error)
	GetExport(context.Context, string) (*SignalsExportJob, *Response, error)
	GetExportDownload(context.Context, string) (*SignalsExportDownload, *Response, error)
	GetExportOptions(context.Context) (*SignalsExportOptions, *Response, error)
	CreateDeletion(context.Context, *SignalsCreateDeletionRequest) (*SignalsDeletionJob, *Response, error)
	GetDeletion(context.Context, string) (*SignalsDeletionJob, *Response, error)
	ListDeletions(context.Context, *SignalsListDeletionsOptions) (*SignalsListDeletionsResponse, *Response, error)
}

// Consent sources. A consent row is keyed by (team, source, agent_id):
// agent rows carry an agent_id; inference consent is team-wide and has an
// empty agent_id.
const (
	// SignalsConsentSourceAgent is per-agent consent (default).
	SignalsConsentSourceAgent = "agent"
	// SignalsConsentSourceInference is team-level consent for serverless inference traffic.
	SignalsConsentSourceInference = "inference"
)

// Deletion types and job statuses used by the Signals deletion API.
const (
	// SignalsDeletionTypeAgent deletes Signals conversations for one Harness Runtime agent.
	SignalsDeletionTypeAgent = "agent"
	// SignalsDeletionTypeManagedAgent is deprecated; use SignalsDeletionTypeAgent.
	SignalsDeletionTypeManagedAgent = SignalsDeletionTypeAgent
	// SignalsDeletionTypeInference deletes Serverless Inference Signals conversations for the whole team.
	SignalsDeletionTypeInference = "inference"

	// SignalsDeletionStatusQueued means the job is waiting to be picked up.
	SignalsDeletionStatusQueued = "queued"
	// SignalsDeletionStatusRunning means the job is being processed.
	SignalsDeletionStatusRunning = "running"
	// SignalsDeletionStatusComplete means the job finished successfully.
	SignalsDeletionStatusComplete = "complete"
	// SignalsDeletionStatusFailed means the job failed; see ErrorMessage.
	SignalsDeletionStatusFailed = "failed"
)

// SignalsServiceOp communicates with the Signals consumption API.
type SignalsServiceOp struct {
	client *Client
}

var _ SignalsService = &SignalsServiceOp{}

// SignalsCursorPageOptions is shared limit/after paging.
type SignalsCursorPageOptions struct {
	Limit int    `url:"limit,omitempty"`
	After string `url:"after,omitempty"`
}

// SignalsListAgentSessionsOptions are query params for GET /agents/{id}/sessions.
type SignalsListAgentSessionsOptions struct {
	SignalsCursorPageOptions
	StartTime  *int64   `url:"start_time,omitempty"`
	EndTime    *int64   `url:"end_time,omitempty"`
	SignalType []string `url:"signal_type,omitempty"`
}

// SignalsListDialoguesOptions are query params for GET /sessions/{id}/dialogues.
type SignalsListDialoguesOptions struct {
	SignalsCursorPageOptions
	Before          string   `url:"before,omitempty"`
	ContinueSession bool     `url:"continue_session,omitempty"`
	SignalType      []string `url:"signal_type,omitempty"`
	StartTime       *int64   `url:"start_time,omitempty"`
	EndTime         *int64   `url:"end_time,omitempty"`
}

// SignalsListExportsOptions are query params for GET /exports.
type SignalsListExportsOptions struct {
	SignalsCursorPageOptions
	// AgentID is a deprecated alias for EnvironmentID. Whichever fields are set
	// are passed through on the wire; the API rejects conflicting values.
	AgentID       string `url:"agent_id,omitempty"`
	EnvironmentID string `url:"environment_id,omitempty"`
}

// SignalsPageInfo is Relay-style cursor paging from signals-api.
type SignalsPageInfo struct {
	HasNextPage bool   `json:"has_next_page"`
	HasPrevPage bool   `json:"has_prev_page,omitempty"`
	StartCursor string `json:"start_cursor,omitempty"`
	EndCursor   string `json:"end_cursor,omitempty"`
}

// SignalsAgentConsent is GET /v1/consent/{environment_id} (consent-gateway).
// enabled is the only customer-facing consent toggle; allowed is an internal
// check-endpoint concept and is not returned on this public route.
type SignalsAgentConsent struct {
	TeamID        int64  `json:"team_id"`
	Source        string `json:"source,omitempty"`
	AgentID       string `json:"agent_id"`
	EnvironmentID string `json:"environment_id,omitempty"`
	Enabled       bool   `json:"enabled"`
	ID            int64  `json:"id,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

// SignalsConsentRecord is a consents row as returned by PUT /v1/consent/{environment_id}
// and PUT /v1/consent?source=inference. Source is "agent" or "inference";
// AgentID is empty for inference (team-level) rows.
type SignalsConsentRecord struct {
	ID            int64  `json:"id"`
	TeamID        int64  `json:"team_id"`
	Source        string `json:"source,omitempty"`
	AgentID       string `json:"agent_id"`
	EnvironmentID string `json:"environment_id,omitempty"`
	Enabled       bool   `json:"enabled"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type signalsSetConsentRequest struct {
	Enabled bool `json:"enabled"`
}

type signalsSetConsentRoot struct {
	Consent *SignalsConsentRecord `json:"consent"`
}

// SignalsListConsentsOptions filters GET /v1/consent.
type SignalsListConsentsOptions struct {
	// Source restricts the listing to one consent source ("agent" or
	// "inference"). Empty returns all sources.
	Source string `url:"source,omitempty"`
}

// SignalsListConsentsResponse is GET /v1/consent (consent-gateway).
// Source is echoed back when the listing was filtered by source.
type SignalsListConsentsResponse struct {
	TeamID   int64                  `json:"team_id"`
	Source   string                 `json:"source,omitempty"`
	Consents []SignalsConsentRecord `json:"consents"`
}

// SignalsCreateExportRequest is the body for POST /v1/signals/exports.
// AgentID is a deprecated alias for EnvironmentID. Whichever fields are set are
// passed through on the wire; the API rejects conflicting values.
type SignalsCreateExportRequest struct {
	AgentID       string `json:"agent_id,omitempty"`
	EnvironmentID string `json:"environment_id,omitempty"`
	// SessionIDs optionally narrows the export to a session cohort (OR within
	// the list). Omit or leave empty to export all sessions owned by the team.
	SessionIDs []string `json:"session_ids,omitempty"`
	SignalType []string `json:"signal_type,omitempty"`
	StartTime  *int64   `json:"start_time,omitempty"`
	EndTime    *int64   `json:"end_time,omitempty"`
}

// SignalsCreateDeletionRequest is the body for POST /v1/signals/deletions.
// JSON keys match CreateDeletionRequest in signals-api OpenAPI:
// required type + team_id; environment_id/agent_id only for agent (omit for inference).
// AgentID is a deprecated alias for EnvironmentID. Whichever fields are set are
// passed through on the wire; the API rejects conflicting values.
type SignalsCreateDeletionRequest struct {
	// Type is SignalsDeletionTypeAgent or SignalsDeletionTypeInference.
	// Required by the server (omitting type is 400). godo also requires it so a
	// missing EnvironmentID can never be mistaken for a team-wide inference deletion.
	Type string `json:"type"`
	// TeamID is the numeric team id; it must match the authenticated team.
	TeamID int64 `json:"team_id"`
	// AgentID is a deprecated alias for EnvironmentID.
	AgentID string `json:"agent_id,omitempty"`
	// EnvironmentID is the Harness Runtime Environment Config ID.
	// Required for agent and must be empty for inference (team-wide only).
	// Omitted from the JSON body when empty.
	EnvironmentID string `json:"environment_id,omitempty"`
}

// SignalsDeletionJob is one deletion job (POST/GET /v1/signals/deletions...).
// Wire shape matches DeletionJob: team_id, deletion_id, type, agent_id
// (omitted for inference), status, error_message, created_at, started_at,
// completed_at. Timestamps are Unix seconds UTC. Status/Type are plain strings
// so new server-side values keep decoding.
type SignalsDeletionJob struct {
	TeamID        int64   `json:"team_id"`
	DeletionID    string  `json:"deletion_id"`
	Type          string  `json:"type"`
	AgentID       string  `json:"agent_id,omitempty"`
	EnvironmentID string  `json:"environment_id,omitempty"`
	Status        string  `json:"status"`
	ErrorMessage  *string `json:"error_message"`
	CreatedAt     int64   `json:"created_at"`
	StartedAt     *int64  `json:"started_at"`
	CompletedAt   *int64  `json:"completed_at"`
}

// SignalsListDeletionsOptions are query params for GET /v1/signals/deletions.
type SignalsListDeletionsOptions struct {
	SignalsCursorPageOptions
}

// SignalsDeletionEdge is one edge in a deletion list.
type SignalsDeletionEdge struct {
	Cursor string             `json:"cursor"`
	Node   SignalsDeletionJob `json:"node"`
}

// SignalsListDeletionsResponse is GET /v1/signals/deletions.
type SignalsListDeletionsResponse struct {
	Edges    []SignalsDeletionEdge `json:"edges"`
	PageInfo SignalsPageInfo       `json:"page_info"`
}

// SignalsSession is a session list node.
type SignalsSession struct {
	SessionID       string `json:"session_id"`
	TotalTurns      int    `json:"total_turns"`
	StartedAt       string `json:"started_at"`
	DurationSeconds int    `json:"duration_seconds"`
	SignalCount     int    `json:"signal_count"`
}

// SignalsSessionEdge is one edge in a session list.
type SignalsSessionEdge struct {
	Cursor string         `json:"cursor"`
	Node   SignalsSession `json:"node"`
}

// SignalsListAgentSessionsResponse is GET /agents/{environment_id}/sessions.
type SignalsListAgentSessionsResponse struct {
	Edges    []SignalsSessionEdge `json:"edges"`
	PageInfo SignalsPageInfo      `json:"page_info"`
}

// SignalsDialogue is one user/assistant turn.
type SignalsDialogue struct {
	ID          string            `json:"id"`
	RunID       string            `json:"run_id"`
	CreatedAt   string            `json:"created_at"`
	Sequence    int               `json:"sequence"`
	UserMessage string            `json:"user_message"`
	Steps       []json.RawMessage `json:"steps"`
	RunStatus   string            `json:"run_status"`
	FailureCode *string           `json:"failure_code,omitempty"`
}

// SignalsInstance is one detector hit on a dialogue.
type SignalsInstance struct {
	SignalType string  `json:"signal_type"`
	Label      string  `json:"label"`
	RunUUID    string  `json:"run_uuid"`
	StepIndex  int     `json:"step_index"`
	Confidence float64 `json:"confidence"`
	Snippet    *string `json:"snippet,omitempty"`
	Layer      string  `json:"layer"`
	Category   string  `json:"category"`
}

// SignalsSessionDialogue is a dialogue plus session-scoped chips.
type SignalsSessionDialogue struct {
	SignalsDialogue
	Signals []SignalsInstance `json:"signals,omitempty"`
}

// SignalsSessionDialogueEdge is one edge in a session dialogue list.
type SignalsSessionDialogueEdge struct {
	Cursor string                 `json:"cursor"`
	Node   SignalsSessionDialogue `json:"node"`
}

// SignalsSessionDialoguesResponse is GET /sessions/{session_id}/dialogues.
type SignalsSessionDialoguesResponse struct {
	SessionID string                       `json:"session_id"`
	Edges     []SignalsSessionDialogueEdge `json:"edges"`
	PageInfo  SignalsPageInfo              `json:"page_info"`
}

// SignalsExportFilters is the snapshot stored on an export job.
type SignalsExportFilters struct {
	SessionIDs     []string `json:"session_ids,omitempty"`
	SignalType     []string `json:"signal_type,omitempty"`
	SignalCategory string   `json:"signal_category,omitempty"`
	SignalLayer    string   `json:"signal_layer,omitempty"`
	Concerning     *bool    `json:"concerning,omitempty"`
	StartTime      *int64   `json:"start_time,omitempty"`
	EndTime        *int64   `json:"end_time,omitempty"`
}

// SignalsExportJob is GET /exports/{export_id} (and list nodes).
type SignalsExportJob struct {
	ExportID      string               `json:"export_id"`
	AgentID       string               `json:"agent_id,omitempty"`
	EnvironmentID string               `json:"environment_id,omitempty"`
	Status        string               `json:"status"`
	Filters       SignalsExportFilters `json:"filters"`
	CreatedAt     int64                `json:"created_at"`
	CompletedAt   *int64               `json:"completed_at,omitempty"`
	ErrorMessage  *string              `json:"error_message,omitempty"`
	ExpiresAt     *int64               `json:"expires_at,omitempty"`
}

func signalsHasEnvironmentOrAgentID(agentID, environmentID string) bool {
	return strings.TrimSpace(agentID) != "" || strings.TrimSpace(environmentID) != ""
}

// SignalsExportEdge is one edge in an export list.
type SignalsExportEdge struct {
	Cursor string           `json:"cursor"`
	Node   SignalsExportJob `json:"node"`
}

// SignalsListExportsResponse is GET /exports.
type SignalsListExportsResponse struct {
	Edges    []SignalsExportEdge `json:"edges"`
	PageInfo SignalsPageInfo     `json:"page_info"`
}

// SignalsExportDownload is GET /exports/{export_id}/download.
type SignalsExportDownload struct {
	DownloadURL string `json:"download_url"`
	ExpiresAt   int64  `json:"expires_at"`
}

// SignalsExportOptions is GET /exports/options.
type SignalsExportOptions struct {
	Filters SignalsExportFilterOptions `json:"filters"`
}

// SignalsExportFilterOptions is the static detector catalog.
type SignalsExportFilterOptions struct {
	SignalType []string `json:"signal_type"`
}

func (s *SignalsServiceOp) get(ctx context.Context, path string, out interface{}) (*Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, out)
}

// ListConsents returns all consent records (every source) for the
// authenticated team. Uses GET /v1/consent on consent-gateway.
func (s *SignalsServiceOp) ListConsents(ctx context.Context) (*SignalsListConsentsResponse, *Response, error) {
	return s.ListConsentsWithOptions(ctx, nil)
}

// ListConsentsWithOptions returns consent records for the authenticated team,
// optionally filtered by source. Uses GET /v1/consent[?source=...].
func (s *SignalsServiceOp) ListConsentsWithOptions(ctx context.Context, opts *SignalsListConsentsOptions) (*SignalsListConsentsResponse, *Response, error) {
	path, err := addOptions(consentBasePath, opts)
	if err != nil {
		return nil, nil, err
	}
	root := new(SignalsListConsentsResponse)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetInferenceConsent returns the team-level consent for serverless inference
// traffic. There is at most one such row per team; when none exists the
// returned record has Enabled=false and ID=0 (default deny), mirroring
// GetAgentConsent. Uses GET /v1/consent?source=inference.
func (s *SignalsServiceOp) GetInferenceConsent(ctx context.Context) (*SignalsConsentRecord, *Response, error) {
	list, resp, err := s.ListConsentsWithOptions(ctx, &SignalsListConsentsOptions{Source: SignalsConsentSourceInference})
	if err != nil {
		return nil, resp, err
	}
	for i := range list.Consents {
		if list.Consents[i].AgentID == "" {
			return &list.Consents[i], resp, nil
		}
	}
	return &SignalsConsentRecord{
		TeamID:  list.TeamID,
		Source:  SignalsConsentSourceInference,
		Enabled: false,
	}, resp, nil
}

// SetInferenceConsent enables or disables Signals collection for all
// serverless inference traffic of the authenticated team
// (PUT /v1/consent?source=inference). Requires IAM signals:update.
func (s *SignalsServiceOp) SetInferenceConsent(ctx context.Context, enabled bool) (*SignalsConsentRecord, *Response, error) {
	path, err := addOptions(consentBasePath, &SignalsListConsentsOptions{Source: SignalsConsentSourceInference})
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, &signalsSetConsentRequest{Enabled: enabled})
	if err != nil {
		return nil, nil, err
	}
	root := new(signalsSetConsentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Consent, resp, nil
}

// GetAgentConsent returns collection consent for one environment (default deny if no row).
// Uses GET /v1/consent/{environment_id} on consent-gateway (same public path as Cloud UI / SetAgentConsent).
func (s *SignalsServiceOp) GetAgentConsent(ctx context.Context, agentID string) (*SignalsAgentConsent, *Response, error) {
	path := fmt.Sprintf("%s/%s", consentBasePath, agentID)
	root := new(SignalsAgentConsent)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// SetAgentConsent enables or disables collection for one agent (PUT /v1/consent/{id}).
// Requires IAM signals:update. Oceanus routes this to consent-gateway, not signals-api.
func (s *SignalsServiceOp) SetAgentConsent(ctx context.Context, agentID string, enabled bool) (*SignalsConsentRecord, *Response, error) {
	path := fmt.Sprintf("%s/%s", consentBasePath, agentID)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, &signalsSetConsentRequest{Enabled: enabled})
	if err != nil {
		return nil, nil, err
	}
	root := new(signalsSetConsentRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root.Consent, resp, nil
}

// ListAgentSessions lists sessions for an agent.
func (s *SignalsServiceOp) ListAgentSessions(ctx context.Context, agentID string, opts *SignalsListAgentSessionsOptions) (*SignalsListAgentSessionsResponse, *Response, error) {
	path := fmt.Sprintf("%s/agents/%s/sessions", signalsBasePath, agentID)
	path, err := addOptions(path, opts)
	if err != nil {
		return nil, nil, err
	}
	root := new(SignalsListAgentSessionsResponse)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListSessionDialogues lists replay dialogues for a session.
func (s *SignalsServiceOp) ListSessionDialogues(ctx context.Context, sessionID string, opts *SignalsListDialoguesOptions) (*SignalsSessionDialoguesResponse, *Response, error) {
	path := fmt.Sprintf("%s/sessions/%s/dialogues", signalsBasePath, sessionID)
	path, err := addOptions(path, opts)
	if err != nil {
		return nil, nil, err
	}
	root := new(SignalsSessionDialoguesResponse)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CreateExport starts a bulk signal export job (POST /v1/signals/exports).
// The create is idempotent: if an active export already exists for the same
// parameters the server returns it instead of creating a duplicate.
func (s *SignalsServiceOp) CreateExport(ctx context.Context, body *SignalsCreateExportRequest) (*SignalsExportJob, *Response, error) {
	if body == nil {
		return nil, nil, fmt.Errorf("signals: create export request is required")
	}
	if !signalsHasEnvironmentOrAgentID(body.AgentID, body.EnvironmentID) {
		return nil, nil, fmt.Errorf("signals: environment_id is required")
	}
	reqBody := *body
	reqBody.AgentID = strings.TrimSpace(body.AgentID)
	reqBody.EnvironmentID = strings.TrimSpace(body.EnvironmentID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, signalsBasePath+"/exports", &reqBody)
	if err != nil {
		return nil, nil, err
	}
	root := new(SignalsExportJob)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListExports lists export jobs (no download URLs).
func (s *SignalsServiceOp) ListExports(ctx context.Context, opts *SignalsListExportsOptions) (*SignalsListExportsResponse, *Response, error) {
	var wireOpts *SignalsListExportsOptions
	if opts != nil {
		cp := *opts
		cp.AgentID = strings.TrimSpace(opts.AgentID)
		cp.EnvironmentID = strings.TrimSpace(opts.EnvironmentID)
		wireOpts = &cp
	}
	path, err := addOptions(signalsBasePath+"/exports", wireOpts)
	if err != nil {
		return nil, nil, err
	}
	root := new(SignalsListExportsResponse)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetExport polls one export job.
func (s *SignalsServiceOp) GetExport(ctx context.Context, exportID string) (*SignalsExportJob, *Response, error) {
	path := fmt.Sprintf("%s/exports/%s", signalsBasePath, exportID)
	root := new(SignalsExportJob)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetExportDownload returns a 15-minute Spaces presign for a completed export.
func (s *SignalsServiceOp) GetExportDownload(ctx context.Context, exportID string) (*SignalsExportDownload, *Response, error) {
	path := fmt.Sprintf("%s/exports/%s/download", signalsBasePath, exportID)
	root := new(SignalsExportDownload)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetExportOptions returns the static signal_type catalog.
func (s *SignalsServiceOp) GetExportOptions(ctx context.Context) (*SignalsExportOptions, *Response, error) {
	root := new(SignalsExportOptions)
	resp, err := s.get(ctx, signalsBasePath+"/exports/options", root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CreateDeletion starts an on-demand data deletion job (POST /v1/signals/deletions).
//
// Deletion is permanent and cannot be undone. On create, matching
// conversations are hidden from Signals reads immediately and collection for
// that scope is turned off (per-agent consent for SignalsDeletionTypeAgent;
// team-wide inference consent for SignalsDeletionTypeInference). Inference
// deletions remove Serverless Inference Signals conversations for the whole
// team that were collected at or before the deletion request. For agent
// deletions, EnvironmentID is the Harness Runtime Environment Config ID.
// Deletion continues asynchronously. Earlier export files are not deleted.
// Aggregated Insights/metrics summaries may still remain and are not the same
// as recoverable conversation content.
//
// For agent deletions, wait until the job is complete before enabling Signals
// for that environment again.
//
// Status codes from the server:
//   - 202 — new job created (queued)
//   - 200 — active job for the same fingerprint reused (queued/running)
//   - 400 — invalid body (type/environment_id rules, unknown fields, bad UUID)
//   - 401 — missing team auth
//   - 403 — body team_id does not match authenticated team
//   - 404 — environment unknown for this team
//   - 429 — too many active deletion jobs for this team (inflight cap)
//   - 500 — internal error
//
// Check Response.StatusCode to tell 200 vs 202 apart. For
// SignalsDeletionTypeAgent EnvironmentID (or deprecated AgentID) is required;
// for SignalsDeletionTypeInference it must be empty (and is omitted from JSON).
//
// If the client retries requests (for example one built with
// WithRetryAndBackoffs), a repeated POST while the first job is still active
// returns that same job, but a repeat after the first job has finished starts
// a new job. A terminal failed job keeps matching conversations hidden and
// collection off; create again to retry.
func (s *SignalsServiceOp) CreateDeletion(ctx context.Context, body *SignalsCreateDeletionRequest) (*SignalsDeletionJob, *Response, error) {
	if body == nil {
		return nil, nil, fmt.Errorf("signals: create deletion request is required")
	}
	if body.TeamID <= 0 {
		return nil, nil, fmt.Errorf("signals: team_id is required")
	}
	typ := strings.TrimSpace(body.Type)
	hasID := signalsHasEnvironmentOrAgentID(body.AgentID, body.EnvironmentID)
	switch typ {
	case SignalsDeletionTypeAgent:
		if !hasID {
			return nil, nil, fmt.Errorf("signals: environment_id is required for agent")
		}
	case SignalsDeletionTypeInference:
		if hasID {
			return nil, nil, fmt.Errorf("signals: environment_id must not be set for inference")
		}
	default:
		return nil, nil, fmt.Errorf("signals: type must be agent or inference")
	}
	reqBody := &SignalsCreateDeletionRequest{
		Type:          typ,
		TeamID:        body.TeamID,
		AgentID:       strings.TrimSpace(body.AgentID),
		EnvironmentID: strings.TrimSpace(body.EnvironmentID),
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, signalsBasePath+"/deletions", reqBody)
	if err != nil {
		return nil, nil, err
	}
	root := new(SignalsDeletionJob)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetDeletion returns one deletion job (GET /v1/signals/deletions/{deletion_id}).
// The server returns 404 both for unknown ids and for jobs owned by another team
// (and for path values that are not storable, e.g. "." / "..").
func (s *SignalsServiceOp) GetDeletion(ctx context.Context, deletionID string) (*SignalsDeletionJob, *Response, error) {
	trimmed := strings.TrimSpace(deletionID)
	if trimmed == "" {
		return nil, nil, fmt.Errorf("signals: deletion_id is required")
	}
	// "." and ".." are not changed by url.PathEscape and would be cleaned into
	// the list route, so reject them.
	if trimmed == "." || trimmed == ".." {
		return nil, nil, fmt.Errorf("signals: deletion_id is invalid")
	}
	path := signalsBasePath + "/deletions/" + url.PathEscape(trimmed)
	root := new(SignalsDeletionJob)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListDeletions lists deletion jobs for the team, newest first
// (GET /v1/signals/deletions). Limit and After are passed through. When Limit
// is 0 the server uses its default page size (20) and it caps larger values at 100.
// Invalid After cursors return 400.
func (s *SignalsServiceOp) ListDeletions(ctx context.Context, opts *SignalsListDeletionsOptions) (*SignalsListDeletionsResponse, *Response, error) {
	path, err := addOptions(signalsBasePath+"/deletions", opts)
	if err != nil {
		return nil, nil, err
	}
	root := new(SignalsListDeletionsResponse)
	resp, err := s.get(ctx, path, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}
