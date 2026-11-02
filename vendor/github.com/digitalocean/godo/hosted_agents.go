package godo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	hostedAgentManifestMediaType  = "application/x-yaml"
	hostedAgentWorkspaceMediaType = "application/octet-stream"

	// hostedAgentsAPIPrefix is the public path prefix of the Harness Runtime API.
	// It was previously /v2/agents, which the edge still serves with identical
	// behaviour.
	hostedAgentsAPIPrefix = "/v2/harness-runtime"

	hostedAgentsSessionsBasePath                    = hostedAgentsAPIPrefix + "/sessions"
	hostedAgentPolicyValidatePath                   = hostedAgentsSessionsBasePath + "/policy/validate"
	hostedAgentSessionByIDPath                      = hostedAgentsSessionsBasePath + "/%s"
	hostedAgentSessionEventsPath                    = hostedAgentSessionByIDPath + "/events"
	hostedAgentSessionInputPath                     = hostedAgentSessionByIDPath + "/input"
	hostedAgentSessionRequestPath                   = hostedAgentSessionByIDPath + "/request"
	hostedAgentSessionHITLPath                      = hostedAgentSessionByIDPath + "/hitl/%s"
	hostedAgentSessionSandboxExecPath               = hostedAgentSessionByIDPath + "/sandbox/exec"
	hostedAgentSandboxSizesPath                     = hostedAgentsSessionsBasePath + "/sandbox/sizes"
	hostedAgentSessionPausePath                     = hostedAgentSessionByIDPath + "/pause"
	hostedAgentSessionResumePath                    = hostedAgentSessionByIDPath + "/resume"
	hostedAgentSessionWorkspaceUploadPath           = hostedAgentSessionByIDPath + "/workspace/upload"
	hostedAgentSessionWorkspaceDownloadPath         = hostedAgentSessionByIDPath + "/workspace/download"
	hostedAgentSessionWorkspaceTransfersPath        = hostedAgentSessionByIDPath + "/workspace/transfers"
	hostedAgentSessionWorkspaceTransferByIDPath     = hostedAgentSessionWorkspaceTransfersPath + "/%s"
	hostedAgentSessionWorkspaceTransferPartURLsPath = hostedAgentSessionWorkspaceTransferByIDPath + "/part-upload-urls"
	hostedAgentSessionWorkspaceTransferCommitPath   = hostedAgentSessionWorkspaceTransferByIDPath + "/commit"
	hostedAgentSessionWorkspaceTransferCancelPath   = hostedAgentSessionWorkspaceTransferByIDPath + "/cancel"

	// Team-scoped external-provider (e.g. GitHub) OAuth connect flow.
	hostedAgentProviderAuthPath     = hostedAgentsAPIPrefix + "/auth/%s"
	hostedAgentProviderAuthPollPath = hostedAgentProviderAuthPath + "/poll"

	// Team-scoped external-provider connection management (harness-api's proxy
	// of Action Gateway / tool-registry). Shares the /v2/harness-runtime/auth/{provider}
	// prefix with the connect flow above. The by-id path takes provider then id.
	hostedAgentProviderConnectionsPath    = hostedAgentProviderAuthPath + "/connections"
	hostedAgentProviderConnectionByIDPath = hostedAgentProviderConnectionsPath + "/%s"

	hostedAgentSessionCheckpointsPath        = hostedAgentSessionByIDPath + "/checkpoints"
	hostedAgentSessionCheckpointByIDPath     = hostedAgentSessionCheckpointsPath + "/%s"
	hostedAgentSessionCheckpointRollbackPath = hostedAgentSessionCheckpointByIDPath + "/rollback"
	hostedAgentSessionForkPath               = hostedAgentSessionByIDPath + "/fork"

	// Persistent workspaces: team-scoped sets of files that outlive sessions.
	hostedAgentWorkspacesBasePath = hostedAgentsAPIPrefix + "/workspaces"
	hostedAgentWorkspaceByIDPath  = hostedAgentWorkspacesBasePath + "/%s"

	// HostedAgentForkMaxCount is the v1 cap on children created by one fork call.
	HostedAgentForkMaxCount = 4

	workspaceContentSHA256Header = "X-Content-Sha256"
	workspaceIsArchiveHeader     = "X-Workspace-Is-Archive"
	workspaceSizeBytesHeader     = "X-Workspace-Size-Bytes"

	// sseLastEventIDHeader is the standard SSE resume cursor. The data-plane
	// events endpoint takes the cursor here rather than as a query parameter.
	sseLastEventIDHeader = "Last-Event-ID"

	// workspaceDownloadFooter is appended by OHS after a successful download
	// payload so integrity survives intermediaries that strip HTTP trailers
	// (e.g. Cloudflare). Format: DOWSSHA1 + 64 lowercase hex + '\n' = 73 bytes.
	workspaceDownloadFooterPrefix = "DOWSSHA1"
	workspaceDownloadFooterLen    = len(workspaceDownloadFooterPrefix) + 64 + 1
)

// HostedAgentsService exposes the DigitalOcean Hosted Agents session API
// (HarnessAPI from harness.proto). Routes live under /v2/harness-runtime/sessions.
type HostedAgentsService interface {
	// CreateSession provisions a session using the legacy JSON body (agent_kind,
	// repo_hint, idle_timeout_seconds). Prefer CreateSessionFromManifest for
	// Environment Spec YAML (or CreateSessionFromEnvironment for a saved
	// Environment).
	CreateSession(context.Context, *HostedAgentSessionCreateRequest) (*HostedAgentSession, *Response, error)
	// CreateSessionFromManifest uploads an environment spec YAML document
	// (Content-Type: application/x-yaml). Legacy agents.digitalocean.com/v1alpha1
	// Agent manifests are also accepted where the API still supports them.
	// For OpenAI sandbox-provider sessions (adapter codex-agentapi), pass
	// OpenAISessionID in opt so harness-api can persist openai_session_id for
	// attach correlation (?openai_session_id=). doctl resolves ${...}
	// placeholders client-side before calling this API; there is no
	// server-side variables map. The session is placed in the region of the
	// API host called (see HostedAgentsRegionalBaseURL); the YAML body carries
	// no region. The returned session reports Region and Location.
	CreateSessionFromManifest(context.Context, []byte, *HostedAgentManifestCreateOptions) (*HostedAgentSession, *Response, error)
	// CreateSessionFromEnvironment provisions a session from an existing
	// immutable Environment, referenced by config_id. The request uses
	// Content-Type: application/json with a {name, config_id} body (optional
	// resume_on_topoff, workspace_id and region); the environment's stored
	// manifest and declared credentials seed the session at create time. This
	// is the only create path that can attach a persistent workspace. The
	// Environment must live in the region of the API host called (see
	// HostedAgentsRegionalBaseURL); on any other region's host it is a 404.
	CreateSessionFromEnvironment(context.Context, *HostedAgentSessionFromEnvironmentRequest) (*HostedAgentSession, *Response, error)
	// CreateSessionFromConfig is an alias for CreateSessionFromEnvironment.
	// Prefer CreateSessionFromEnvironment; this name remains for compatibility.
	CreateSessionFromConfig(context.Context, *HostedAgentSessionFromConfigRequest) (*HostedAgentSession, *Response, error)
	// ValidatePolicy checks a manifest's tool-permission policy against the
	// agent's capabilities without creating a session. Manifest is the same YAML
	// document accepted by CreateSessionFromManifest (Content-Type:
	// application/x-yaml). Returns per-rule verdicts; OK is false when any strict
	// rule is unsupported or unverified (i.e. CreateSession would reject it).
	ValidatePolicy(context.Context, []byte) (*HostedAgentPolicyValidationResult, *Response, error)
	ListSessions(context.Context, *HostedAgentSessionListOptions) (*HostedAgentSessionsListResponse, *Response, error)
	GetSession(context.Context, string) (*HostedAgentSession, *Response, error)
	// UpdateSession patches session-scoped settings on an existing session and
	// returns the updated snapshot. Never touches the agent manifest or config.
	UpdateSession(context.Context, string, *HostedAgentSessionUpdateRequest) (*HostedAgentSession, *Response, error)
	DestroySession(context.Context, string) (*Response, error)
	PauseSession(context.Context, string) (*Response, error)
	ResumeSession(context.Context, string) (*Response, error)
	StreamSession(context.Context, string, *HostedAgentSessionStreamOptions) (*HostedAgentSessionStream, *Response, error)
	SendInput(context.Context, string, *HostedAgentSendInputRequest) (*HostedAgentSendInputResponse, *Response, error)
	RelayRequest(context.Context, string, *HostedAgentRelayRequest) (*HostedAgentRelayResponse, *Response, error)
	ResolveHITL(context.Context, string, string, *HostedAgentResolveHITLRequest) (*Response, error)

	// StartProviderAuth begins (or resumes) the team-scoped connect flow for an
	// external provider (e.g. "github"). The team is derived from the
	// authenticated principal; there is no request body.
	StartProviderAuth(context.Context, string) (*HostedAgentProviderAuthStart, *Response, error)
	// PollProviderAuth reports whether a pending connect link has been
	// authorized. pollURL is the PollURL returned by StartProviderAuth.
	PollProviderAuth(context.Context, string, string) (*HostedAgentProviderAuthPoll, *Response, error)

	// ListConnections lists the team's connections for an external provider
	// (e.g. "github"), scoped server-side to the caller's team. Paginated with
	// page/per_page (tool-registry defaults to 20 per page and caps at 100).
	ListConnections(context.Context, string, *HostedAgentConnectionListOptions) (*HostedAgentConnectionsListResponse, *Response, error)
	// CreateConnection creates (or resumes) a connection binding an actor to the
	// provider. A still-pending connection comes back with an Authorization the
	// user must complete in a browser; an already-active one has none.
	CreateConnection(context.Context, string, *HostedAgentConnectionCreateRequest) (*HostedAgentConnectionEnvelope, *Response, error)
	// GetConnection reads one connection by id, used to poll a pending
	// authorization to completion.
	GetConnection(context.Context, string, string) (*HostedAgentConnectionEnvelope, *Response, error)
	// DeleteConnection revokes one connection by id and returns its final state.
	DeleteConnection(context.Context, string, string) (*HostedAgentConnectionEnvelope, *Response, error)
	ExecInSandbox(context.Context, string, *HostedAgentSandboxExecRequest) (*HostedAgentSandboxExecResponse, *Response, error)
	// ListSandboxSizes returns the customer-selectable sandbox (microVM) size
	// catalog (GET /v2/harness-runtime/sessions/sandbox/sizes). Every slug is accepted by
	// CreateSession as spec.sandbox.sizeSlug. Ordered smallest-to-largest; no
	// pagination.
	ListSandboxSizes(context.Context) (*HostedAgentSandboxSizesResponse, *Response, error)
	UploadWorkspace(context.Context, string, *HostedAgentWorkspaceUploadRequest) (*HostedAgentWorkspaceUploadResponse, *Response, error)
	DownloadWorkspace(context.Context, string, *HostedAgentWorkspaceDownloadRequest) (*HostedAgentWorkspaceDownload, *Response, error)

	// Large-file (>~50 MiB) staged workspace transfer APIs. Streaming
	// UploadWorkspace / DownloadWorkspace remain for smaller payloads.
	CreateWorkspaceTransfer(context.Context, string, *HostedAgentWorkspaceTransferCreateRequest) (*HostedAgentWorkspaceTransfer, *Response, error)
	CreateWorkspaceTransferPartUploadURLs(context.Context, string, string, *HostedAgentWorkspaceTransferPartUploadURLsRequest) (*HostedAgentWorkspaceTransferPartUploadURLs, *Response, error)
	CommitWorkspaceTransfer(context.Context, string, string, *HostedAgentWorkspaceTransferCommitRequest) (*HostedAgentWorkspaceTransfer, *Response, error)
	GetWorkspaceTransfer(context.Context, string, string) (*HostedAgentWorkspaceTransfer, *Response, error)
	CancelWorkspaceTransfer(context.Context, string, string, *HostedAgentWorkspaceTransferCancelRequest) (*HostedAgentWorkspaceTransferCancelResponse, *Response, error)

	// Checkpoint / fork / rollback (session save points).
	CreateCheckpoint(context.Context, string, *HostedAgentCheckpointCreateRequest) (*HostedAgentCheckpoint, *Response, error)
	ListCheckpoints(context.Context, string, *HostedAgentCheckpointListOptions) (*HostedAgentCheckpointsListResponse, *Response, error)
	GetCheckpoint(context.Context, string, string) (*HostedAgentCheckpoint, *Response, error)
	DeleteCheckpoint(context.Context, string, string) (*HostedAgentCheckpointDeleteResponse, *Response, error)
	ForkSession(context.Context, string, *HostedAgentForkSessionRequest) (*HostedAgentForkSessionResponse, *Response, error)
	RollbackToCheckpoint(context.Context, string, string) (*HostedAgentSession, *Response, error)

	// Persistent workspaces (separate sets of files that outlive sessions, attached to a
	// session at create time). Routes live under /v2/harness-runtime/workspaces. These are
	// distinct from the session /workspace file transfer APIs above.
	CreateWorkspace(context.Context, *HostedAgentWorkspaceCreateRequest) (*HostedAgentWorkspace, *Response, error)
	ListWorkspaces(context.Context, *HostedAgentWorkspaceListOptions) (*HostedAgentWorkspacesListResponse, *Response, error)
	GetWorkspace(context.Context, string) (*HostedAgentWorkspace, *Response, error)
	DeleteWorkspace(context.Context, string) (*Response, error)

	// Environments (immutable team-scoped environment definitions). Routes
	// live under /v2/harness-runtime/configs. Prefer the Environment* methods; the
	// AgentConfig* names remain as compatible aliases.
	ListEnvironments(context.Context, *HostedEnvironmentListOptions) (*HostedEnvironmentsListResponse, *Response, error)
	GetEnvironment(context.Context, string) (*HostedEnvironment, *Response, error)
	CreateEnvironment(context.Context, *HostedEnvironmentCreateRequest) (*HostedEnvironment, *Response, error)
	DeleteEnvironment(context.Context, string) (*Response, error)
	ListEnvironmentSessions(context.Context, string, *HostedAgentSessionListOptions) (*HostedAgentSessionsListResponse, *Response, error)
	// Prefer ListEnvironments / GetEnvironment / CreateEnvironment /
	// DeleteEnvironment / ListEnvironmentSessions.
	ListAgentConfigs(context.Context, *HostedAgentConfigListOptions) (*HostedAgentConfigsListResponse, *Response, error)
	GetAgentConfig(context.Context, string) (*HostedAgentConfig, *Response, error)
	CreateAgentConfig(context.Context, *HostedAgentConfigCreateRequest) (*HostedAgentConfig, *Response, error)
	DeleteAgentConfig(context.Context, string) (*Response, error)
	ListAgentConfigSessions(context.Context, string, *HostedAgentSessionListOptions) (*HostedAgentSessionsListResponse, *Response, error)

	// Team custom templates (BYOA). Routes live under /v2/harness-runtime/templates.
	ListTemplates(context.Context, *HostedAgentTemplateListOptions) (*HostedAgentTemplatesListResponse, *Response, error)
	CreateTemplate(context.Context, *HostedAgentTemplateCreateRequest) (*HostedAgentTemplate, *Response, error)
	GetTemplate(context.Context, string) (*HostedAgentTemplate, *Response, error)
	UpdateTemplate(context.Context, string, *HostedAgentTemplateUpdateRequest) (*HostedAgentTemplate, *Response, error)
	DeleteTemplate(context.Context, string) (*HostedAgentTemplateDeleteResponse, *Response, error)
	ListTemplateBuilds(context.Context, string, *HostedAgentTemplateBuildListOptions) (*HostedAgentTemplateBuildsListResponse, *Response, error)
	GetTemplateBuild(context.Context, string, string) (*HostedAgentTemplateBuild, *Response, error)
	GetTemplateBuildLogs(context.Context, string, string) (*HostedAgentTemplateBuildLogs, *Response, error)
}

// HostedAgentsServiceOp handles communication with Hosted Agents session methods.
type HostedAgentsServiceOp struct {
	client *Client
}

var _ HostedAgentsService = &HostedAgentsServiceOp{}

// HostedAgentKind identifies the agent runtime provisioned for a session.
type HostedAgentKind string

const (
	HostedAgentKindUnspecified HostedAgentKind = "AGENT_KIND_UNSPECIFIED"
	HostedAgentKindClaudeCode  HostedAgentKind = "AGENT_KIND_CLAUDE_CODE"
	HostedAgentKindOpenCode    HostedAgentKind = "AGENT_KIND_OPENCODE"
	HostedAgentKindCodexCLI    HostedAgentKind = "AGENT_KIND_CODEX_CLI"
	HostedAgentKindCursorCLI   HostedAgentKind = "AGENT_KIND_CURSOR_CLI"
	// HostedAgentKindOpenAICodex is the OpenAI Agents API sandbox-provider kind
	// (adapter codex-agentapi). Attach bridges to OpenAI, not DO's SSE stream.
	HostedAgentKindOpenAICodex HostedAgentKind = "AGENT_KIND_OPENAI_CODEX"
	HostedAgentKindNone        HostedAgentKind = "AGENT_KIND_NONE"
	HostedAgentKindCustom      HostedAgentKind = "AGENT_KIND_CUSTOM"
)

// HostedAgentSessionStatus is the lifecycle status of a hosted agent session.
type HostedAgentSessionStatus string

const (
	HostedAgentSessionStatusUnspecified  HostedAgentSessionStatus = "SESSION_STATUS_UNSPECIFIED"
	HostedAgentSessionStatusProvisioning HostedAgentSessionStatus = "SESSION_STATUS_PROVISIONING"
	HostedAgentSessionStatusReady        HostedAgentSessionStatus = "SESSION_STATUS_READY"
	HostedAgentSessionStatusDetached     HostedAgentSessionStatus = "SESSION_STATUS_DETACHED"
	HostedAgentSessionStatusDestroying   HostedAgentSessionStatus = "SESSION_STATUS_DESTROYING"
	HostedAgentSessionStatusDestroyed    HostedAgentSessionStatus = "SESSION_STATUS_DESTROYED"
	HostedAgentSessionStatusFailed       HostedAgentSessionStatus = "SESSION_STATUS_FAILED"
	HostedAgentSessionStatusPaused       HostedAgentSessionStatus = "SESSION_STATUS_PAUSED"
)

// HostedAgentSessionPauseReason is why a paused session was paused. The API
// reserves the right to add reasons, so an unrecognized value is an opaque
// reason string rather than an error: compare against the constants below to
// recognize a reason, but pass anything else through as-is.
type HostedAgentSessionPauseReason string

const (
	// HostedAgentSessionPauseReasonManual is a pause requested through PauseSession.
	HostedAgentSessionPauseReasonManual HostedAgentSessionPauseReason = "manual"
	// HostedAgentSessionPauseReasonIdle is an automatic pause after inactivity.
	HostedAgentSessionPauseReasonIdle HostedAgentSessionPauseReason = "idle"
	// HostedAgentSessionPauseReasonZeroBalance is a pause by the prepayment gate.
	// It is the only reason the server auto-resumes from, and only for sessions
	// with ResumeOnTopoff set.
	HostedAgentSessionPauseReasonZeroBalance HostedAgentSessionPauseReason = "zero_balance"
	// Deprecated: Use HostedAgentSessionPauseReasonZeroBalance. Servers now return
	// "zero_balance"; this constant is kept for backward compatibility with older API responses.
	HostedAgentSessionPauseReasonLowBalance HostedAgentSessionPauseReason = "low_balance"
)

// HostedAgentProviderAuthState tracks OAuth authorization for an external provider.
type HostedAgentProviderAuthState string

const (
	HostedAgentProviderAuthStateUnspecified HostedAgentProviderAuthState = "PROVIDER_AUTH_STATE_UNSPECIFIED"
	HostedAgentProviderAuthStateNone        HostedAgentProviderAuthState = "PROVIDER_AUTH_STATE_NONE"
	HostedAgentProviderAuthStatePending     HostedAgentProviderAuthState = "PROVIDER_AUTH_STATE_PENDING"
	HostedAgentProviderAuthStateAuthorized  HostedAgentProviderAuthState = "PROVIDER_AUTH_STATE_AUTHORIZED"
	HostedAgentProviderAuthStateExpired     HostedAgentProviderAuthState = "PROVIDER_AUTH_STATE_EXPIRED"
)

// HostedAgentRunState is the lifecycle state of a single agent run.
type HostedAgentRunState string

const (
	HostedAgentRunStateUnspecified  HostedAgentRunState = "RUN_STATE_UNSPECIFIED"
	HostedAgentRunStateQueued       HostedAgentRunState = "RUN_STATE_QUEUED"
	HostedAgentRunStateRunning      HostedAgentRunState = "RUN_STATE_RUNNING"
	HostedAgentRunStateAwaitingHITL HostedAgentRunState = "RUN_STATE_AWAITING_HITL"
	HostedAgentRunStatePaused       HostedAgentRunState = "RUN_STATE_PAUSED"
	HostedAgentRunStateCompleted    HostedAgentRunState = "RUN_STATE_COMPLETED"
	HostedAgentRunStateFailed       HostedAgentRunState = "RUN_STATE_FAILED"
)

// HostedAgentHITLActionKind classifies a human-in-the-loop approval request.
type HostedAgentHITLActionKind string

const (
	HostedAgentHITLActionUnspecified               HostedAgentHITLActionKind = "HITL_ACTION_KIND_UNSPECIFIED"
	HostedAgentHITLActionBash                      HostedAgentHITLActionKind = "HITL_ACTION_BASH"
	HostedAgentHITLActionFileWriteOutsideWorkspace HostedAgentHITLActionKind = "HITL_ACTION_FILE_WRITE_OUTSIDE_WORKSPACE"
	HostedAgentHITLActionGitHubCommitPush          HostedAgentHITLActionKind = "HITL_ACTION_GITHUB_COMMIT_PUSH"
	HostedAgentHITLActionGitHubCreatePR            HostedAgentHITLActionKind = "HITL_ACTION_GITHUB_CREATE_PR"
	HostedAgentHITLActionGitHubBranchDelete        HostedAgentHITLActionKind = "HITL_ACTION_GITHUB_BRANCH_DELETE"
	HostedAgentHITLActionGitHubForcePush           HostedAgentHITLActionKind = "HITL_ACTION_GITHUB_FORCE_PUSH"
)

// HostedAgentHITLOutcome is the user's decision on a HITL request.
type HostedAgentHITLOutcome string

const (
	HostedAgentHITLOutcomeUnspecified HostedAgentHITLOutcome = "HITL_OUTCOME_UNSPECIFIED"
	HostedAgentHITLOutcomeApprove     HostedAgentHITLOutcome = "HITL_OUTCOME_APPROVE"
	HostedAgentHITLOutcomeReject      HostedAgentHITLOutcome = "HITL_OUTCOME_REJECT"
	HostedAgentHITLOutcomeDefer       HostedAgentHITLOutcome = "HITL_OUTCOME_DEFER"
)

// HostedAgentResolutionSource records where a HITL decision originated.
type HostedAgentResolutionSource string

const (
	HostedAgentResolutionSourceUnspecified     HostedAgentResolutionSource = "RESOLUTION_SOURCE_UNSPECIFIED"
	HostedAgentResolutionSourceInlineKeystroke HostedAgentResolutionSource = "RESOLUTION_SOURCE_INLINE_KEYSTROKE"
	HostedAgentResolutionSourceOutOfBand       HostedAgentResolutionSource = "RESOLUTION_SOURCE_OUT_OF_BAND"
)

// HostedAgentRunFailureCode classifies a failed run.
type HostedAgentRunFailureCode string

const (
	HostedAgentRunFailureCodeUnspecified    HostedAgentRunFailureCode = "RUN_FAILURE_CODE_UNSPECIFIED"
	HostedAgentRunFailureCodeModelError     HostedAgentRunFailureCode = "RUN_FAILURE_CODE_MODEL_ERROR"
	HostedAgentRunFailureCodeModelTimeout   HostedAgentRunFailureCode = "RUN_FAILURE_CODE_MODEL_TIMEOUT"
	HostedAgentRunFailureCodeToolError      HostedAgentRunFailureCode = "RUN_FAILURE_CODE_TOOL_ERROR"
	HostedAgentRunFailureCodeSandboxLost    HostedAgentRunFailureCode = "RUN_FAILURE_CODE_SANDBOX_LOST"
	HostedAgentRunFailureCodeHITLRejected   HostedAgentRunFailureCode = "RUN_FAILURE_CODE_HITL_REJECTED"
	HostedAgentRunFailureCodeBudgetExceeded HostedAgentRunFailureCode = "RUN_FAILURE_CODE_BUDGET_EXCEEDED"
	HostedAgentRunFailureCodeInternal       HostedAgentRunFailureCode = "RUN_FAILURE_CODE_INTERNAL"
)

// HostedAgentEventKind is the SSE event discriminator for session stream
// payloads. The values are the canonical SPI event type names (dot-separated)
// emitted on the wire's `type` field — NOT proto enum names. They mirror the
// spi.EventType constants owned by the hosted-agents stack.
type HostedAgentEventKind string

const (
	HostedAgentEventKindUnspecified          HostedAgentEventKind = ""
	HostedAgentEventKindRunStarted           HostedAgentEventKind = "run.started"
	HostedAgentEventKindTokenChunk           HostedAgentEventKind = "run.token_delta"
	HostedAgentEventKindToolCallStarted      HostedAgentEventKind = "run.tool_call_started"
	HostedAgentEventKindToolCallCompleted    HostedAgentEventKind = "run.tool_call_completed"
	HostedAgentEventKindHITLRequested        HostedAgentEventKind = "run.human_input_requested"
	HostedAgentEventKindHITLResolved         HostedAgentEventKind = "run.human_input_received"
	HostedAgentEventKindRunCompleted         HostedAgentEventKind = "run.completed"
	HostedAgentEventKindRunFailed            HostedAgentEventKind = "run.failed"
	HostedAgentEventKindRunPaused            HostedAgentEventKind = "run.paused"
	HostedAgentEventKindRunResumed           HostedAgentEventKind = "run.resumed"
	HostedAgentEventKindSessionUpdated       HostedAgentEventKind = "session.updated"
	HostedAgentEventKindRunStateCheckpointed HostedAgentEventKind = "run.state_checkpointed"
	HostedAgentEventKindRunHandoff           HostedAgentEventKind = "run.handoff"
	HostedAgentEventKindRunUsageRecorded     HostedAgentEventKind = "run.usage_recorded"
	HostedAgentEventKindRunSandboxAllocated  HostedAgentEventKind = "run.sandbox_allocated"
	HostedAgentEventKindRunSandboxReleased   HostedAgentEventKind = "run.sandbox_released"
	HostedAgentEventKindRunCostAccrued       HostedAgentEventKind = "run.cost_accrued"
	HostedAgentEventKindRunLog               HostedAgentEventKind = "run.log"

	// HostedAgentEventKindStreamState is a transport control frame, not an agent
	// event: it reports the health of the SSE connection itself. Only the
	// data-plane events endpoint emits it. It arrives in the same envelope as an
	// event, but only SessionID, At and Payload are meaningful — decode Payload
	// with HostedAgentStreamState. Renderers should skip it rather than display
	// it as session activity.
	HostedAgentEventKindStreamState HostedAgentEventKind = "stream.state"
)

// HostedAgentStreamState is the payload of a HostedAgentEventKindStreamState
// frame: the current health of the SSE connection.
type HostedAgentStreamState struct {
	State  HostedAgentStreamStateValue `json:"state"`
	Cursor string                      `json:"cursor,omitempty"`
}

// HostedAgentStreamStateValue enumerates the stream health states.
type HostedAgentStreamStateValue string

const (
	// HostedAgentStreamStateLive means events are being delivered contiguously.
	HostedAgentStreamStateLive HostedAgentStreamStateValue = "live"
	// HostedAgentStreamStateCatchingUp means the connection is replaying recent
	// history before it joins the live tail.
	HostedAgentStreamStateCatchingUp HostedAgentStreamStateValue = "catching_up"
	// HostedAgentStreamStateDegraded means delivery continues with reduced
	// guarantees (the server fell back to polling).
	HostedAgentStreamStateDegraded HostedAgentStreamStateValue = "degraded"
	// HostedAgentStreamStateSuperseded means a newer connection from the same
	// device took over. The server closes this stream; the client should stop
	// rather than reconnect, or the two connections will evict each other.
	HostedAgentStreamStateSuperseded HostedAgentStreamStateValue = "superseded"
)

// HostedAgentSessionOriginProduct identifies the product workflow that created
// a session. Wire values match harness SessionOrigin.product.
type HostedAgentSessionOriginProduct string

const (
	HostedAgentSessionOriginProductDirect     HostedAgentSessionOriginProduct = "direct"
	HostedAgentSessionOriginProductSimulation HostedAgentSessionOriginProduct = "simulation"
	HostedAgentSessionOriginProductEvaluation HostedAgentSessionOriginProduct = "evaluation"
)

// HostedAgentSessionOriginRequest is the product-provenance claim on create.
// Omission creates a verified direct session. Simulation and evaluation require
// resource_id; direct forbids it. verified is server-assigned and must not be
// sent on create (see HostedAgentSessionOrigin).
type HostedAgentSessionOriginRequest struct {
	Product    HostedAgentSessionOriginProduct `json:"product"`
	ResourceID string                          `json:"resource_id,omitempty"`
}

// HostedAgentSessionOrigin is server-returned product-workflow provenance.
// Verified is true for direct sessions and for product claims established
// through a trusted adapter.
type HostedAgentSessionOrigin struct {
	Product    HostedAgentSessionOriginProduct `json:"product"`
	ResourceID string                          `json:"resource_id,omitempty"`
	Verified   bool                            `json:"verified"`
}

// HostedAgentSession is a provisioned hosted-agent sandbox session.
type HostedAgentSession struct {
	SessionID    string                                  `json:"session_id"`
	Name         string                                  `json:"name,omitempty"`
	AgentKind    HostedAgentKind                         `json:"agent_kind"`
	Status       HostedAgentSessionStatus                `json:"status"`
	CreatedAt    Timestamp                               `json:"created_at"`
	LastEventAt  Timestamp                               `json:"last_event_at"`
	RepoHint     string                                  `json:"repo_hint,omitempty"`
	ProviderAuth map[string]HostedAgentProviderAuthState `json:"provider_auth,omitempty"`
	// Origin is present for newly created sessions (including direct). Older
	// sessions may omit it.
	Origin *HostedAgentSessionOrigin `json:"origin,omitempty"`
	// PauseReason qualifies Status when it is SESSION_STATUS_PAUSED, and is
	// omitted otherwise. Unrecognized values are opaque, not errors — see
	// HostedAgentSessionPauseReason.
	PauseReason HostedAgentSessionPauseReason `json:"pause_reason,omitempty"`
	// SandboxID is the sandbox identity for support / debugging. Informational;
	// omitted until a sandbox is allocated.
	SandboxID string `json:"sandbox_id,omitempty"`
	// ResumeOnTopoff is the consent to auto-resume this session when a
	// prepayment balance is restored. Set at create (manifest query param or
	// config JSON body), toggled afterwards with UpdateSession, and returned on
	// get/list so clients can confirm enrollment. Never inherited by forks.
	// Omitted when false.
	ResumeOnTopoff bool `json:"resume_on_topoff,omitempty"`
	// WorkspaceID is the persistent workspace this session attaches (see
	// HostedAgentWorkspace). Set at create and returned on get/list; omitted
	// when the session has no persistent workspace.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// OpenAISessionID is the OpenAI Agents session id (sess_…) linked to this DO
	// sandbox for AGENT_KIND_OPENAI_CODEX. Used by attach to bridge to OpenAI;
	// omitempty for other agent kinds.
	OpenAISessionID string `json:"openai_session_id,omitempty"`
	// OpenAIEnvironmentID is captured from the resolved CODEX_ENVIRONMENT_ID
	// guest env value at create. Non-secret correlation metadata.
	OpenAIEnvironmentID string `json:"openai_environment_id,omitempty"`
	// ParentSessionID is set on forked child sessions; empty/omitted for roots.
	ParentSessionID string `json:"parent_session_id,omitempty"`
	// ForkID is a branch label on forked sessions; empty/omitted for roots.
	ForkID string `json:"fork_id,omitempty"`
	// ConfigID is the durable Environment UUID the session was created from
	// (inline manifests persist an environment too, named after the session).
	// Returned on create/get/list; omitted for sessions created before
	// environment references were stored. Historically called Agent Config.
	ConfigID string `json:"config_id,omitempty"`
	// Warnings carries non-fatal create-time advisories from the server
	// (manifest parse + policy fidelity). Populated on the create response
	// only; omitted on get/list.
	Warnings []string `json:"warnings,omitempty"`
	// Region is the DO region slug the session is placed in, e.g. "ric1".
	// Stamped once at create from the region that served the request and never
	// recomputed. Returned on create/get/list for teams on the regional routing
	// rollout; omitted otherwise, and for sessions created before the field
	// existed, which are all ric1.
	Region string `json:"region,omitempty"`
	// Location is the session's canonical URL from the create response's
	// Location header, on the host of the region that owns it, e.g.
	// https://api-ric1.digitalocean.com/v2/agents/sessions/{id}. Point follow-up
	// calls at that host (HostedAgentsRegionalBaseURL(session.Region) gives the
	// base URL). Populated only by the create methods, only when the server
	// sent the header (teams on the regional routing rollout); never on the
	// wire.
	Location string `json:"-"`
}

// HostedAgentsRegionalBaseURL returns the base URL of the API host for one
// region, e.g. "https://api-mem1.digitalocean.com/" for "mem1", in the form
// SetBaseURL accepts. Every MARS resource lives in exactly one region, and the
// API host chosen is what places a create and what finds a resource: a session
// created on api-mem1 is placed in mem1 and is a 404 on any other region's
// host. The region-less default (api.digitalocean.com) places in and serves
// ric1. There is no redirect, forwarding or fan-out between regions. An empty
// region returns the region-less default. Example:
//
//	client, _ := godo.New(httpClient, godo.SetBaseURL(godo.HostedAgentsRegionalBaseURL("mem1")))
//	sess, _, _ := client.HostedAgents.CreateSessionFromEnvironment(ctx, &godo.HostedAgentSessionFromEnvironmentRequest{
//		Name: "review-bot", ConfigID: cfg.ID, Region: "mem1", // body region matches the host
//	})
//	// sess.Location is on api-mem1; use the same client for every follow-up.
func HostedAgentsRegionalBaseURL(region string) string {
	region = strings.TrimSpace(strings.ToLower(region))
	if region == "" {
		return defaultBaseURL
	}
	return "https://api-" + region + ".digitalocean.com/"
}

// HostedAgentRun represents a single execution within a session.
type HostedAgentRun struct {
	RunID                string              `json:"run_id"`
	SessionID            string              `json:"session_id"`
	State                HostedAgentRunState `json:"state"`
	StartedAt            Timestamp           `json:"started_at"`
	EndedAt              *Timestamp          `json:"ended_at,omitempty"`
	CumulativeCostMicros int64               `json:"cumulative_cost_micros"`
}

// HostedAgentHITLRequest is a pending human-in-the-loop approval.
type HostedAgentHITLRequest struct {
	RequestID string                    `json:"request_id"`
	SessionID string                    `json:"session_id"`
	RunID     string                    `json:"run_id"`
	Action    HostedAgentHITLActionKind `json:"action"`
	Details   map[string]interface{}    `json:"details,omitempty"`
	Workdir   string                    `json:"workdir,omitempty"`
	Deadline  *Timestamp                `json:"deadline,omitempty"`
}

// HostedAgentHITLDecision records a resolved HITL request.
type HostedAgentHITLDecision struct {
	RequestID string                 `json:"request_id"`
	Outcome   HostedAgentHITLOutcome `json:"outcome"`
	Actor     string                 `json:"actor,omitempty"`
	At        Timestamp              `json:"at"`
	Reason    string                 `json:"reason,omitempty"`
}

// HostedAgentEvent is one SSE payload from a session stream (see StreamSession).
//
// The server serializes the SPI canonical event envelope, whose JSON shape
// differs from this struct's field names: the discriminator is `type` (not
// `kind`), the per-kind body is `data` (not `payload`), and the timestamp is
// `timestamp` (not `at`). UnmarshalJSON maps that wire shape onto these fields,
// so callers read Kind/Payload/At directly.
type HostedAgentEvent struct {
	EventID   string
	SessionID string
	RunID     string
	Seq       uint64
	At        Timestamp
	Kind      HostedAgentEventKind
	Payload   json.RawMessage

	// SourceEventID is the native event id from the agent runtime before
	// canonical mapping (Event.source_event_id). Empty when the runtime does
	// not supply stable ids or the server does not forward it.
	SourceEventID string
	// SourceEventType is the native event type label from the agent runtime
	// (e.g. codex's "item/agentMessage/delta"). Empty when not forwarded.
	SourceEventType string
	// SourceRaw is the exact native event bytes the in-sandbox adapter
	// captured before canonical mapping (Event.source_raw) — for codex, one
	// JSON-RPC frame as read off the app-server transport. Only present when
	// the stream was opened with HostedAgentSessionStreamOptions.IncludeRaw
	// and the server retained the bytes; base64 on the wire (JSON []byte).
	SourceRaw []byte
}

// hostedAgentEventWire is the on-the-wire SPI canonical event envelope.
type hostedAgentEventWire struct {
	EventID         string               `json:"event_id"`
	RunID           string               `json:"run_id"`
	SessionID       string               `json:"session_id"`
	Timestamp       Timestamp            `json:"timestamp"`
	Seq             uint64               `json:"seq"`
	SourceEventID   string               `json:"source_event_id,omitempty"`
	SourceEventType string               `json:"source_event_type,omitempty"`
	SourceRaw       []byte               `json:"source_raw,omitempty"`
	Type            HostedAgentEventKind `json:"type"`
	Data            json.RawMessage      `json:"data"`
}

// UnmarshalJSON decodes the SPI canonical event wire shape.
func (e *HostedAgentEvent) UnmarshalJSON(b []byte) error {
	var w hostedAgentEventWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	e.EventID = w.EventID
	e.RunID = w.RunID
	e.SessionID = w.SessionID
	e.Seq = w.Seq
	e.At = w.Timestamp
	e.Kind = w.Type
	e.Payload = w.Data
	e.SourceEventID = w.SourceEventID
	e.SourceEventType = w.SourceEventType
	e.SourceRaw = w.SourceRaw
	return nil
}

// HostedAgentSessionCreateRequest is the body for POST /v2/harness-runtime/sessions.
type HostedAgentSessionCreateRequest struct {
	AgentKind          HostedAgentKind `json:"agent_kind"`
	RepoHint           string          `json:"repo_hint,omitempty"`
	IdleTimeoutSeconds int64           `json:"idle_timeout_seconds,omitempty"`
	// Origin claims product-workflow provenance. Omit for a verified direct
	// session. Simulation/evaluation require resource_id.
	Origin *HostedAgentSessionOriginRequest `json:"origin,omitempty"`
}

// HostedAgentSessionFromConfigRequest creates a session from an existing
// immutable Environment (POST /v2/harness-runtime/sessions with a JSON body). Name is
// required and becomes the new session name; ConfigID must be the UUID of an
// Environment owned by the caller's team.
// Prefer HostedAgentSessionFromEnvironmentRequest; this name remains for
// compatibility. The JSON field remains "config_id" on the wire.
type HostedAgentSessionFromConfigRequest struct {
	Name     string `json:"name"`
	ConfigID string `json:"config_id"`
	// ResumeOnTopoff opts the new session in to automatic resumption when the
	// team's prepayment balance is restored. Session-scoped, not
	// environment-scoped: the referenced Environment never confers it, so one
	// shared Environment cannot enrol every session created from it. Omitted
	// when false, which leaves the server default (also false).
	ResumeOnTopoff bool `json:"resume_on_topoff,omitempty"`
	// WorkspaceID attaches an existing persistent workspace (see
	// HostedAgentWorkspace) to the new session. This is the only create path
	// that can attach one; CreateSessionFromManifest cannot. Session-scoped, not
	// environment-scoped: the referenced Environment never names a workspace, so
	// one shared Environment can be used with a different workspace per session.
	// Omitted when empty, which creates the session without one.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// Region is an optional placement check, not a routing instruction. The
	// region a session is placed in is decided by the API host the request
	// reaches: the region-less host (api.digitalocean.com) places in the default
	// region, ric1, and a regional host (api-<region>.digitalocean.com, see
	// HostedAgentsRegionalBaseURL) places in that region. When Region is set it
	// must equal the region of the host called, or the create fails with a 4xx
	// whose message starts with "region_mismatch"; nothing is re-placed or
	// forwarded. Omitted when empty. Only honored for teams on the regional
	// routing rollout; ignored otherwise.
	Region string `json:"region,omitempty"`
}

// HostedAgentSessionFromEnvironmentRequest is the advertised alias for
// HostedAgentSessionFromConfigRequest.
type HostedAgentSessionFromEnvironmentRequest = HostedAgentSessionFromConfigRequest

// HostedAgentSessionUpdateRequest is the body for PATCH
// /v2/harness-runtime/sessions/{session_id}: an allowlisted subset of session-scoped
// settings, never an agents.yaml or Agent Config mutation. The server rejects
// unknown top-level keys, and rejects an empty object, so every field is a
// pointer: nil omits it and leaves the server-side value untouched.
type HostedAgentSessionUpdateRequest struct {
	// ResumeOnTopoff sets whether the session opts in to automatic resumption
	// once the team's prepayment balance is restored after a low-balance pause.
	// Nil leaves the current consent unchanged. Revoking takes effect
	// immediately for later sweeps; granting it does not by itself resume a
	// session that is already paused.
	ResumeOnTopoff *bool `json:"resume_on_topoff,omitempty"`
}

// empty reports whether the request would serialize to {}, which the server
// rejects with a 400. Checked client-side so a no-op call fails locally with a
// useful message instead of spending a round-trip to earn a certain error.
func (r *HostedAgentSessionUpdateRequest) empty() bool {
	return r == nil || r.ResumeOnTopoff == nil
}

// HostedAgentManifestCreateOptions configures CreateSessionFromManifest.
// OpenAISessionID is sent as the openai_session_id query parameter (not in the
// YAML body). harness-api persists it for AGENT_KIND_OPENAI_CODEX attach
// correlation. See docs/design/openai-sandbox-provider.md.
//
// A persistent workspace cannot be attached when creating from a manifest. To
// use one, save the manifest as an Environment and create the session with
// CreateSessionFromEnvironment (or CreateSessionFromConfig) and WorkspaceID.
type HostedAgentManifestCreateOptions struct {
	OpenAISessionID string `url:"openai_session_id,omitempty"`
	// ResumeOnTopoff opts the new session in to automatic resumption when the
	// team's prepayment balance is restored after a low-balance pause. It is a
	// query parameter rather than an agents.yaml field because that document is
	// immutable and shared through Agent Configs, while spending consent has to
	// stay per-session. Omitted when false (the server default).
	ResumeOnTopoff bool `url:"resume_on_topoff,omitempty"`
}

// HostedAgentSessionListOptions specifies optional list filters.
type HostedAgentSessionListOptions struct {
	PageToken string                   `url:"page_token,omitempty"`
	PageSize  int                      `url:"page_size,omitempty"`
	Status    HostedAgentSessionStatus `url:"status,omitempty"`
	Name      string                   `url:"name,omitempty"`
	// ParentSessionID lists child (forked) sessions of the given parent.
	ParentSessionID string `url:"parent_session_id,omitempty"`
}

// HostedAgentSessionsListResponse is returned by GET /v2/harness-runtime/sessions.
type HostedAgentSessionsListResponse struct {
	Sessions      []HostedAgentSession `json:"sessions"`
	NextPageToken string               `json:"next_page_token"`
}

// HostedAgentSessionStreamOptions configures the session SSE stream.
//
// ReplayOnly selects between the two modes StreamSession can open on the events
// endpoint (see StreamSession); Before and Limit page backwards through history
// within the replay-only mode.
type HostedAgentSessionStreamOptions struct {
	// ReplayFrom is the resume cursor: the id of the last event the caller
	// already rendered. On the live stream it is sent as Last-Event-ID; on a
	// ReplayOnly read it is sent as the replay_from query parameter.
	ReplayFrom string
	// ReplayOnly reads the stored event history and ends the stream at the last
	// stored event instead of holding the connection open for live events.
	ReplayOnly bool

	// Before turns the request into a single backward page of durable
	// history: the events strictly older than this event id, oldest-first,
	// after which the stream closes without live events. Set it to the
	// oldest event id already held. Requires ReplayOnly, since a live
	// attach cannot start in the past.
	//
	// A cursorless replay only covers the newest window of a session's
	// history (the server's replay budget), so walking backwards from its
	// oldest event is how older history is read. HasMore reports whether
	// the walk can continue.
	Before string

	// Limit caps the events in one history page. Only meaningful with
	// Before. Zero leaves the server's default (200); the server also caps
	// any request at its replay budget.
	Limit int

	// IncludeRaw asks the server to include each event's native source bytes
	// (HostedAgentEvent.SourceRaw) alongside the canonical payload. Raw
	// payloads meaningfully fatten every event, so this is opt-in; consumers
	// that don't translate native protocols should leave it off.
	IncludeRaw bool
}

// HostedAgentSendInputRequest is the body for POST .../input.
type HostedAgentSendInputRequest struct {
	Text string `json:"text"`

	// SourceRaw optionally carries the client's exact native protocol frame
	// this input was extracted from — for codex, the TUI's turn/start
	// JSON-RPC message with its full params. The in-sandbox adapter uses it
	// as the template for the turn it drives, so client intent beyond plain
	// text (input items, model, effort, approval policy, ...) survives the
	// text reduction. Only meaningful when the caller speaks the session's
	// own agent protocol; Text stays required either way. Inbound
	// counterpart of HostedAgentEvent.SourceRaw. Base64 on the wire per the
	// proto bytes JSON mapping — encoding/json does that for []byte.
	SourceRaw []byte `json:"source_raw,omitempty"`
}

// HostedAgentSendInputResponse is returned by POST .../input.
type HostedAgentSendInputResponse struct {
	RunID string `json:"run_id"`
}

// HostedAgentRelayRequest is the body for POST .../request: one native
// agent-protocol request frame, forwarded to the session's agent verbatim.
//
// Where SendInput carries the one message with a canonical meaning ("the user
// said something"), this carries everything else a client that speaks the
// session's own protocol needs to ask — for codex, the requests behind
// interrupts, slash commands, and model pickers. The control plane never
// parses the frame; only the in-sandbox adapter decides what is safe to
// forward.
type HostedAgentRelayRequest struct {
	// SourceRaw is the caller's native protocol request frame — for codex, a
	// single JSON-RPC request object carrying the caller's own id. Named to
	// match SendInput's field: both mean "my own frame, verbatim". Base64 on
	// the wire per the proto bytes JSON mapping.
	SourceRaw []byte `json:"source_raw"`
}

// HostedAgentRelayResponse is the reply to POST .../request.
type HostedAgentRelayResponse struct {
	// SourceRaw is the agent's reply frame, addressed to the caller's own
	// request id but otherwise verbatim. A protocol-level failure (a JSON-RPC
	// error object) is a normal reply and arrives here rather than as an HTTP
	// error.
	//
	// Empty means the in-sandbox adapter declined to forward the method.
	// Callers must answer their own caller on that case rather than waiting
	// for something that will not come.
	SourceRaw []byte `json:"source_raw,omitempty"`
}

// HostedAgentResolveHITLRequest is the body for POST .../hitl/{requestID}.
type HostedAgentResolveHITLRequest struct {
	Outcome HostedAgentHITLOutcome      `json:"outcome"`
	Reason  string                      `json:"reason,omitempty"`
	Source  HostedAgentResolutionSource `json:"source,omitempty"`

	// SourceRaw is the client's reply in the agent's own protocol, forwarded
	// to the in-sandbox agent untouched. It carries what Outcome cannot: an
	// elicitation's content, a tool's requested input, a scope beyond this one
	// call. Outcome stays required alongside it — it is what the audit trail
	// records, and what the agent falls back to when this is absent.
	SourceRaw []byte `json:"source_raw,omitempty"`

	// Content is a structured answer for a gated request that asks for DATA
	// rather than just a verdict — an MCP *form* elicitation sends a
	// requestedSchema and expects an object matching it back (e.g.
	// {"site_url": "https://acme.atlassian.net"} for a Jira connection). Its
	// keys are the requesting server's, never fixed here. Omitted for plain
	// approvals, where Outcome alone is the whole answer. Mirrors
	// harness-api's resolveHITLBody.Content; forwarded to the run and never
	// persisted (form content can carry user-entered secrets).
	Content map[string]any `json:"content,omitempty"`
}

// HostedAgentProviderAuthStart is returned by POST /v2/harness-runtime/auth/{provider}.
// Status is "pending" when the user must still authorize in a browser
// (ConnectURL/PollURL/VerificationCode are set), or "success" when the team is
// already connected (those fields are empty). It is a free-form connect-flow
// status, distinct from the HostedAgentProviderAuthState session field. The
// authorization handle is never exposed: tokens are exchanged server-side at
// session time.
type HostedAgentProviderAuthStart struct {
	Provider         string     `json:"provider"`
	Status           string     `json:"status"`
	ConnectURL       string     `json:"connect_url,omitempty"`
	PollURL          string     `json:"poll_url,omitempty"`
	VerificationCode string     `json:"verification_code,omitempty"`
	ExpiresAt        *Timestamp `json:"expires_at,omitempty"`
}

// HostedAgentProviderAuthPoll is returned by GET
// /v2/harness-runtime/auth/{provider}/poll. It reports only whether authorization has
// completed ("pending" or "success"); no secret is returned.
type HostedAgentProviderAuthPoll struct {
	Provider  string     `json:"provider"`
	Status    string     `json:"status"`
	ExpiresAt *Timestamp `json:"expires_at,omitempty"`
}

// HostedAgentConnection is one external-provider connection as published by
// harness-api's connection-management proxy. ActorID (user_id on the wire) is
// the actor identifier a manifest's secret `actor:` binds to — not a DO user
// id. OwningUserID is the DO user that created the connection. No token or
// authorization handle is ever exposed on this surface. DeletionProtected
// reports that an agent environment depends on the connection, so an
// ordinary DeleteConnection will be refused; the API sends it on every
// connection, protected or not.
type HostedAgentConnection struct {
	ID                  string                      `json:"id"`
	Provider            string                      `json:"provider"`
	ProviderDisplayName string                      `json:"provider_display_name,omitempty"`
	ActorID             string                      `json:"user_id"`
	Status              string                      `json:"status"`
	OwningUserID        string                      `json:"owning_user_id,omitempty"`
	DeletionProtected   bool                        `json:"deletion_protected"`
	CreatedAt           *Timestamp                  `json:"created_at,omitempty"`
	UpdatedAt           *Timestamp                  `json:"updated_at,omitempty"`
	OAuth               *HostedAgentConnectionOAuth `json:"oauth,omitempty"`
}

// HostedAgentConnectionOAuth carries a connection's granted OAuth detail. Absent
// on connections that hold no OAuth grant (e.g. API-key connections).
type HostedAgentConnectionOAuth struct {
	Scopes    []string   `json:"scopes"`
	GrantedAt *Timestamp `json:"granted_at,omitempty"`
}

// HostedAgentConnectionAuthorization is the interactive step a still-pending
// connection needs. Omitted once the connection is active.
type HostedAgentConnectionAuthorization struct {
	Status           string     `json:"status,omitempty"`
	ConnectURL       string     `json:"connect_url,omitempty"`
	VerificationCode string     `json:"verification_code,omitempty"`
	ExpiresAt        *Timestamp `json:"expires_at,omitempty"`
}

// HostedAgentConnectionEnvelope is a single connection plus any authorization
// still outstanding — the shape CreateConnection, GetConnection, and
// DeleteConnection all return.
type HostedAgentConnectionEnvelope struct {
	Connection    HostedAgentConnection               `json:"connection"`
	Authorization *HostedAgentConnectionAuthorization `json:"authorization,omitempty"`
}

// HostedAgentConnectionPagination is the page descriptor for a connection list.
type HostedAgentConnectionPagination struct {
	Page    int32 `json:"page"`
	PerPage int32 `json:"per_page"`
	Total   int32 `json:"total"`
}

// HostedAgentConnectionsListResponse is one page of connections.
type HostedAgentConnectionsListResponse struct {
	Connections []HostedAgentConnection         `json:"connections"`
	Pagination  HostedAgentConnectionPagination `json:"pagination"`
}

// HostedAgentConnectionListOptions specifies optional connection list filters.
// ActorID filters to a single actor (user_id on the wire); Status filters by
// connection status. Page/PerPage are forwarded to tool-registry, which
// defaults to 20 per page and caps at 100.
type HostedAgentConnectionListOptions struct {
	ActorID string `url:"user_id,omitempty"`
	Status  string `url:"status,omitempty"`
	Page    int    `url:"page,omitempty"`
	PerPage int    `url:"per_page,omitempty"`
}

// HostedAgentConnectionCreateRequest is the body for POST .../connections.
// ActorID (user_id on the wire) names the actor the connection acts for and is
// required. Scopes is optional; omitted requests the provider's full configured
// scope set.
type HostedAgentConnectionCreateRequest struct {
	ActorID string   `json:"user_id"`
	Scopes  []string `json:"scopes,omitempty"`
}

// HostedAgentSandboxExecRequest is the body for POST .../sandbox/exec.
type HostedAgentSandboxExecRequest struct {
	Argv           []string `json:"argv"`
	Workdir        string   `json:"workdir,omitempty"`
	TimeoutSeconds int64    `json:"timeout_seconds,omitempty"`
}

// HostedAgentSandboxExecResponse is returned by POST .../sandbox/exec.
type HostedAgentSandboxExecResponse struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

// HostedAgentSandboxSize is a customer-selectable sandbox (microVM) size from
// GET /v2/harness-runtime/sessions/sandbox/sizes. Submit Slug as spec.sandbox.sizeSlug
// on CreateSession; VCPUs, MemoryMB, and the price fields are for display.
type HostedAgentSandboxSize struct {
	Slug             string  `json:"slug,omitempty"`
	VCPUs            int     `json:"vcpus,omitempty"`
	MemoryMB         int     `json:"memory_mb,omitempty"`
	PricePerHourUSD  float64 `json:"price_per_hour_usd,omitempty"`
	PricePerMonthUSD float64 `json:"price_per_month_usd,omitempty"`
}

// HostedAgentSandboxSizesResponse is returned by ListSandboxSizes.
type HostedAgentSandboxSizesResponse struct {
	// Sizes is ordered smallest-to-largest.
	Sizes []HostedAgentSandboxSize `json:"sizes"`
}

// HostedAgentWorkspaceUploadRequest is the input for UploadWorkspace.
type HostedAgentWorkspaceUploadRequest struct {
	// Path is the destination resolved inside the workspace root (/workspace). Required.
	Path string
	// IsArchive indicates Body is a tar archive to extract at Path.
	IsArchive bool
	// ContentSHA256 is an optional hex SHA-256 digest of the payload, forwarded to the guest for verification.
	ContentSHA256 string
	// Body is the raw file or tar bytes to upload. Required.
	Body io.Reader
}

// HostedAgentWorkspaceUploadResponse is returned by UploadWorkspace.
type HostedAgentWorkspaceUploadResponse struct {
	Path         string `json:"path"`
	BytesWritten int64  `json:"bytes_written"`
}

// HostedAgentWorkspaceDownloadRequest is the input for DownloadWorkspace.
type HostedAgentWorkspaceDownloadRequest struct {
	// Path is the source resolved inside the workspace root (/workspace). Required.
	Path string
	// AsArchive tar-streams the directory at Path when true.
	AsArchive bool
}

// HostedAgentWorkspaceDownload is the streaming result of DownloadWorkspace.
// Body strips the trailing integrity footer and verifies SHA-256 of the
// payload: read it to EOF and then Close it. A missing, invalid, or mismatched
// footer is an error. An HTTP X-Content-Sha256 trailer may still be present
// but is best-effort only; the body footer is the source of truth.
type HostedAgentWorkspaceDownload struct {
	Body io.ReadCloser
	// IsArchive is true when the payload is a tar stream.
	IsArchive bool
	// SizeBytes is the X-Workspace-Size-Bytes hint (0 when unknown); payload
	// size only (excludes the integrity footer). Not a Content-Length.
	SizeBytes int64
}

// HostedAgentWorkspaceTransferDirection is the direction of a staged transfer.
type HostedAgentWorkspaceTransferDirection string

const (
	HostedAgentWorkspaceTransferDirectionUpload   HostedAgentWorkspaceTransferDirection = "upload"
	HostedAgentWorkspaceTransferDirectionDownload HostedAgentWorkspaceTransferDirection = "download"
)

// HostedAgentWorkspaceTransferStatus is the status of a staged transfer.
type HostedAgentWorkspaceTransferStatus string

const (
	HostedAgentWorkspaceTransferStatusPending    HostedAgentWorkspaceTransferStatus = "pending"
	HostedAgentWorkspaceTransferStatusInProgress HostedAgentWorkspaceTransferStatus = "in_progress"
	HostedAgentWorkspaceTransferStatusCompleted  HostedAgentWorkspaceTransferStatus = "completed"
	HostedAgentWorkspaceTransferStatusFailed     HostedAgentWorkspaceTransferStatus = "failed"
)

// HostedAgentWorkspaceTransferCreateRequest starts a large upload or download.
// Upload uses IsArchive; download uses AsArchive (per OHS HTTP contract).
type HostedAgentWorkspaceTransferCreateRequest struct {
	Direction HostedAgentWorkspaceTransferDirection `json:"direction"`
	Path      string                                `json:"path"`
	IsArchive bool                                  `json:"is_archive,omitempty"`
	AsArchive bool                                  `json:"as_archive,omitempty"`
	SizeBytes int64                                 `json:"size_bytes,omitempty"`
	SHA256    string                                `json:"sha256,omitempty"`
}

// HostedAgentWorkspaceTransfer is returned by create/commit/get transfer calls.
type HostedAgentWorkspaceTransfer struct {
	TransferID   string                                `json:"transfer_id"`
	Direction    HostedAgentWorkspaceTransferDirection `json:"direction,omitempty"`
	Status       HostedAgentWorkspaceTransferStatus    `json:"status"`
	UploadID     string                                `json:"upload_id,omitempty"`
	PartSize     int64                                 `json:"part_size,omitempty"`
	ExpiresAt    *Timestamp                            `json:"expires_at,omitempty"`
	SizeBytes    int64                                 `json:"size_bytes,omitempty"`
	BytesWritten int64                                 `json:"bytes_written,omitempty"`
	SHA256       string                                `json:"sha256,omitempty"`
	DownloadURL  string                                `json:"download_url,omitempty"`
	ErrorMessage string                                `json:"error_message,omitempty"`
}

// HostedAgentWorkspaceTransferPartUploadURLsRequest requests presigned URLs for
// one or more upload parts. PartNumbers are 1-based.
type HostedAgentWorkspaceTransferPartUploadURLsRequest struct {
	PartNumbers []int `json:"part_numbers"`
}

// HostedAgentWorkspaceTransferPartUploadURL is one entry in a part-upload-urls response.
// PUT the part bytes directly to UploadURL (not through OHS).
type HostedAgentWorkspaceTransferPartUploadURL struct {
	PartNumber int    `json:"part_number"`
	UploadURL  string `json:"upload_url"`
}

// HostedAgentWorkspaceTransferPartUploadURLs is returned by CreateWorkspaceTransferPartUploadURLs.
type HostedAgentWorkspaceTransferPartUploadURLs struct {
	PartURLs []HostedAgentWorkspaceTransferPartUploadURL `json:"part_urls"`
}

// HostedAgentWorkspaceTransferCommitRequest finalizes an upload after all parts are PUT.
type HostedAgentWorkspaceTransferCommitRequest struct {
	SHA256 string `json:"sha256,omitempty"`
}

// HostedAgentWorkspaceTransferCancelRequest aborts an in-flight transfer.
type HostedAgentWorkspaceTransferCancelRequest struct {
	Reason string `json:"reason,omitempty"`
}

// HostedAgentWorkspaceTransferCancelResponse is returned by CancelWorkspaceTransfer.
type HostedAgentWorkspaceTransferCancelResponse struct {
	TransferID string                             `json:"transfer_id"`
	Aborted    bool                               `json:"aborted"`
	Status     HostedAgentWorkspaceTransferStatus `json:"status"`
}

type hostedAgentSessionRoot struct {
	Session *HostedAgentSession `json:"session"`
}

// CreateSession provisions a new hosted agent session.
func (s *HostedAgentsServiceOp) CreateSession(ctx context.Context, create *HostedAgentSessionCreateRequest) (*HostedAgentSession, *Response, error) {
	if create == nil {
		return nil, nil, errors.New("hosted agents: create request is required")
	}
	if create.AgentKind == "" || create.AgentKind == HostedAgentKindUnspecified {
		return nil, nil, errors.New("hosted agents: agent_kind is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, hostedAgentsSessionsBasePath, create)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentSessionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Session == nil {
		return nil, resp, errors.New("hosted agents: create session returned no session")
	}
	root.Session.Location = resp.Header.Get("Location")
	return root.Session, resp, nil
}

func (s *HostedAgentsServiceOp) CreateSessionFromManifest(ctx context.Context, manifest []byte, opt *HostedAgentManifestCreateOptions) (*HostedAgentSession, *Response, error) {
	if len(bytes.TrimSpace(manifest)) == 0 {
		return nil, nil, errors.New("hosted agents: manifest is required")
	}
	path, err := addOptions(hostedAgentsSessionsBasePath, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.newCreateSessionPostRequest(ctx, path, bytes.NewReader(manifest), hostedAgentManifestMediaType)
	if err != nil {
		return nil, nil, err
	}
	return s.doCreateSession(ctx, req)
}

// CreateSessionFromEnvironment provisions a session from a durable Environment.
// The JSON {name, config_id} body selects the environment-backed create path on
// the server, which reloads the Environment's stored manifest and resolves its
// declared credentials into the new session.
func (s *HostedAgentsServiceOp) CreateSessionFromEnvironment(ctx context.Context, create *HostedAgentSessionFromEnvironmentRequest) (*HostedAgentSession, *Response, error) {
	return s.CreateSessionFromConfig(ctx, create)
}

// CreateSessionFromConfig provisions a session from a durable Environment.
// Prefer CreateSessionFromEnvironment; this name remains for compatibility.
// The JSON {name, config_id} body selects the environment-backed create path on
// the server, which reloads the Environment's stored manifest and resolves its
// declared credentials into the new session.
func (s *HostedAgentsServiceOp) CreateSessionFromConfig(ctx context.Context, create *HostedAgentSessionFromConfigRequest) (*HostedAgentSession, *Response, error) {
	if create == nil {
		return nil, nil, errors.New("hosted agents: create request is required")
	}
	if create.Name == "" {
		return nil, nil, errors.New("hosted agents: name is required")
	}
	if create.ConfigID == "" {
		return nil, nil, errors.New("hosted agents: config_id is required")
	}
	req, err := s.client.NewRequest(ctx, http.MethodPost, hostedAgentsSessionsBasePath, create)
	if err != nil {
		return nil, nil, err
	}
	return s.doCreateSession(ctx, req)
}

func (s *HostedAgentsServiceOp) doCreateSession(ctx context.Context, req *http.Request) (*HostedAgentSession, *Response, error) {
	root := new(hostedAgentSessionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Session == nil {
		return nil, resp, errors.New("hosted agents: create session returned no session")
	}
	root.Session.Location = resp.Header.Get("Location")
	return root.Session, resp, nil
}

func (s *HostedAgentsServiceOp) newCreateSessionPostRequest(ctx context.Context, path string, body io.Reader, contentType string) (*http.Request, error) {
	u, err := s.client.BaseURL.Parse(path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", mediaType)
	req.Header.Set("User-Agent", s.client.UserAgent)
	return req, nil
}

// ListSessions returns sessions visible to the caller's team.
//
// The server omits simulation and evaluation sessions from the list so customer
// surfaces stay free of product-owned internal runs. GetSession by session_id
// still returns those sessions for the owning product workflow.
func (s *HostedAgentsServiceOp) ListSessions(ctx context.Context, opt *HostedAgentSessionListOptions) (*HostedAgentSessionsListResponse, *Response, error) {
	path, err := addOptions(hostedAgentsSessionsBasePath, opt)
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

// GetSession returns a single session by ID.
func (s *HostedAgentsServiceOp) GetSession(ctx context.Context, sessionID string) (*HostedAgentSession, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	path := fmt.Sprintf(hostedAgentSessionByIDPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentSessionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Session == nil {
		return nil, resp, errors.New("hosted agents: get session returned no session")
	}
	return root.Session, resp, nil
}

// UpdateSession patches session-scoped settings and returns the updated
// session. Only the fields set on update are sent; the rest are left alone.
// A session in a terminal state (destroyed, failed) cannot be patched and the
// API answers 409.
func (s *HostedAgentsServiceOp) UpdateSession(ctx context.Context, sessionID string, update *HostedAgentSessionUpdateRequest) (*HostedAgentSession, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if update.empty() {
		return nil, nil, errors.New("hosted agents: update request must set at least one field")
	}
	path := fmt.Sprintf(hostedAgentSessionByIDPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPatch, path, update)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentSessionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Session == nil {
		return nil, resp, errors.New("hosted agents: update session returned no session")
	}
	return root.Session, resp, nil
}

// DestroySession tears down a session. The API returns HTTP 204 on success.
func (s *HostedAgentsServiceOp) DestroySession(ctx context.Context, sessionID string) (*Response, error) {
	if sessionID == "" {
		return nil, errors.New("hosted agents: session id is required")
	}
	path := fmt.Sprintf(hostedAgentSessionByIDPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, nil)
}

func (s *HostedAgentsServiceOp) PauseSession(ctx context.Context, sessionID string) (*Response, error) {
	if sessionID == "" {
		return nil, errors.New("hosted agents: session id is required")
	}
	path := fmt.Sprintf(hostedAgentSessionPausePath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, struct{}{})
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, nil)
}

func (s *HostedAgentsServiceOp) ResumeSession(ctx context.Context, sessionID string) (*Response, error) {
	if sessionID == "" {
		return nil, errors.New("hosted agents: session id is required")
	}
	path := fmt.Sprintf(hostedAgentSessionResumePath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, struct{}{})
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, nil)
}

// StreamSession opens the SSE stream for a session. Callers MUST Close the stream.
//
// Both reads are served by the data plane at GET .../sessions/{id}/events; the
// two modes differ in where the stream starts and whether it ends:
//
//   - Live (the default) delivers forward-only from the moment of attach,
//     preceded by whatever recent history the server decides to replay, and
//     holds the connection open. ReplayFrom is sent as the standard
//     Last-Event-ID header.
//   - ReplayOnly adds ?replay_only=true: the server writes the session's stored
//     event history and then ends the stream, so the read terminates on its own.
//     ReplayFrom is sent as the replay_from query parameter, since it is an
//     explicit pagination cursor here rather than a resume hint. Before and
//     Limit page backwards from an event id within this mode; see
//     HostedAgentSessionStreamOptions.Before and HasMore.
//
// Both carry HostedAgentEventKindStreamState control frames (see
// HostedAgentStreamState). A replay-only read reports catching_up and then
// simply ends; it never reaches live.
func (s *HostedAgentsServiceOp) StreamSession(ctx context.Context, sessionID string, opt *HostedAgentSessionStreamOptions) (*HostedAgentSessionStream, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	replayOnly := opt != nil && opt.ReplayOnly
	cursor := ""
	before := ""
	limit := 0
	includeRaw := false
	if opt != nil {
		// The server answers this combination with a 400; rejecting it here
		// spends no round trip to learn the same thing.
		if opt.Before != "" && !opt.ReplayOnly {
			return nil, nil, errors.New("hosted agents: before requires replay only")
		}
		cursor = opt.ReplayFrom
		before = opt.Before
		limit = opt.Limit
		includeRaw = opt.IncludeRaw
	}

	path := fmt.Sprintf(hostedAgentSessionEventsPath, sessionID)
	q := url.Values{}
	if replayOnly {
		q.Set("replay_only", "true")
		if cursor != "" {
			q.Set("replay_from", cursor)
		}
		if before != "" {
			q.Set("before", before)
		}
		if limit > 0 {
			q.Set("limit", strconv.Itoa(limit))
		}
	}
	if includeRaw {
		q.Set("include_raw", "true")
	}
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	if !replayOnly && cursor != "" {
		req.Header.Set(sseLastEventIDHeader, cursor)
	}

	resp, err := s.client.DoStream(ctx, req)
	if err != nil {
		return nil, resp, err
	}
	stream := &HostedAgentSessionStream{
		raw:  NewSSEReader(resp.Body),
		body: resp.Body,
	}
	stream.raw.OnComment = stream.observeComment
	return stream, resp, nil
}

// SendInput enqueues user text for the in-sandbox agent runtime.
func (s *HostedAgentsServiceOp) SendInput(ctx context.Context, sessionID string, input *HostedAgentSendInputRequest) (*HostedAgentSendInputResponse, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if input == nil {
		return nil, nil, errors.New("hosted agents: input is required")
	}
	if strings.TrimSpace(input.Text) == "" {
		return nil, nil, errors.New("hosted agents: text is required")
	}
	path := fmt.Sprintf(hostedAgentSessionInputPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentSendInputResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// RelayRequest forwards one native agent-protocol request frame to the
// session's agent and returns its reply verbatim. Blocks on the agent, so it
// is slower than the other session calls; an empty reply means the in-sandbox
// adapter declined the method.
func (s *HostedAgentsServiceOp) RelayRequest(ctx context.Context, sessionID string, body *HostedAgentRelayRequest) (*HostedAgentRelayResponse, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if body == nil || len(body.SourceRaw) == 0 {
		return nil, nil, errors.New("hosted agents: source_raw is required")
	}
	path := fmt.Sprintf(hostedAgentSessionRequestPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentRelayResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ResolveHITL submits a human-in-the-loop decision. The API returns HTTP 204 on success.
func (s *HostedAgentsServiceOp) ResolveHITL(ctx context.Context, sessionID, requestID string, body *HostedAgentResolveHITLRequest) (*Response, error) {
	if sessionID == "" {
		return nil, errors.New("hosted agents: session id is required")
	}
	if requestID == "" {
		return nil, errors.New("hosted agents: request id is required")
	}
	if body == nil {
		return nil, errors.New("hosted agents: resolve request is required")
	}
	if body.Outcome == "" || body.Outcome == HostedAgentHITLOutcomeUnspecified {
		return nil, errors.New("hosted agents: outcome is required")
	}
	path := fmt.Sprintf(hostedAgentSessionHITLPath, sessionID, requestID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	return s.client.Do(ctx, req, nil)
}

// StartProviderAuth begins (or resumes) the team-scoped connect flow for an
// external provider. The server derives the team from the authenticated
// principal, so the POST carries an empty JSON object body.
func (s *HostedAgentsServiceOp) StartProviderAuth(ctx context.Context, provider string) (*HostedAgentProviderAuthStart, *Response, error) {
	if provider == "" {
		return nil, nil, errors.New("hosted agents: provider is required")
	}
	path := fmt.Sprintf(hostedAgentProviderAuthPath, provider)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, struct{}{})
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentProviderAuthStart)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// PollProviderAuth checks whether a pending connect link has been authorized.
// pollURL is the PollURL returned by StartProviderAuth; it is forwarded to the
// server as the poll_url query parameter.
func (s *HostedAgentsServiceOp) PollProviderAuth(ctx context.Context, provider, pollURL string) (*HostedAgentProviderAuthPoll, *Response, error) {
	if provider == "" {
		return nil, nil, errors.New("hosted agents: provider is required")
	}
	if pollURL == "" {
		return nil, nil, errors.New("hosted agents: poll url is required")
	}
	path := fmt.Sprintf(hostedAgentProviderAuthPollPath, provider)
	q := url.Values{}
	q.Set("poll_url", pollURL)
	path += "?" + q.Encode()
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentProviderAuthPoll)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListConnections lists the team's connections for an external provider.
func (s *HostedAgentsServiceOp) ListConnections(ctx context.Context, provider string, opt *HostedAgentConnectionListOptions) (*HostedAgentConnectionsListResponse, *Response, error) {
	if provider == "" {
		return nil, nil, errors.New("hosted agents: provider is required")
	}
	path := fmt.Sprintf(hostedAgentProviderConnectionsPath, provider)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentConnectionsListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CreateConnection creates (or resumes) a connection binding an actor to the
// provider.
func (s *HostedAgentsServiceOp) CreateConnection(ctx context.Context, provider string, body *HostedAgentConnectionCreateRequest) (*HostedAgentConnectionEnvelope, *Response, error) {
	if provider == "" {
		return nil, nil, errors.New("hosted agents: provider is required")
	}
	if body == nil || body.ActorID == "" {
		return nil, nil, errors.New("hosted agents: actor is required")
	}
	path := fmt.Sprintf(hostedAgentProviderConnectionsPath, provider)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentConnectionEnvelope)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetConnection reads one connection by id.
func (s *HostedAgentsServiceOp) GetConnection(ctx context.Context, provider, id string) (*HostedAgentConnectionEnvelope, *Response, error) {
	if provider == "" {
		return nil, nil, errors.New("hosted agents: provider is required")
	}
	if id == "" {
		return nil, nil, errors.New("hosted agents: connection id is required")
	}
	path := fmt.Sprintf(hostedAgentProviderConnectionByIDPath, provider, id)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentConnectionEnvelope)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// DeleteConnection revokes one connection by id.
func (s *HostedAgentsServiceOp) DeleteConnection(ctx context.Context, provider, id string) (*HostedAgentConnectionEnvelope, *Response, error) {
	if provider == "" {
		return nil, nil, errors.New("hosted agents: provider is required")
	}
	if id == "" {
		return nil, nil, errors.New("hosted agents: connection id is required")
	}
	path := fmt.Sprintf(hostedAgentProviderConnectionByIDPath, provider, id)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentConnectionEnvelope)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ExecInSandbox runs a command inside the session sandbox.
func (s *HostedAgentsServiceOp) ExecInSandbox(ctx context.Context, sessionID string, body *HostedAgentSandboxExecRequest) (*HostedAgentSandboxExecResponse, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if body == nil {
		return nil, nil, errors.New("hosted agents: exec request is required")
	}
	if len(body.Argv) == 0 {
		return nil, nil, errors.New("hosted agents: argv is required")
	}
	path := fmt.Sprintf(hostedAgentSessionSandboxExecPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentSandboxExecResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListSandboxSizes returns the customer-selectable sandbox (microVM) size
// catalog. The set is the single source of truth shared with create-time
// validation; every returned slug is accepted as spec.sandbox.sizeSlug.
// Sizes are ordered smallest-to-largest. The catalog is static and independent
// of team/session; there is no pagination.
func (s *HostedAgentsServiceOp) ListSandboxSizes(ctx context.Context) (*HostedAgentSandboxSizesResponse, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, hostedAgentSandboxSizesPath, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentSandboxSizesResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// UploadWorkspace streams a file (or tar archive) into the session workspace.
func (s *HostedAgentsServiceOp) UploadWorkspace(ctx context.Context, sessionID string, input *HostedAgentWorkspaceUploadRequest) (*HostedAgentWorkspaceUploadResponse, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if input == nil {
		return nil, nil, errors.New("hosted agents: upload request is required")
	}
	if input.Path == "" {
		return nil, nil, errors.New("hosted agents: path is required")
	}
	if input.Body == nil {
		return nil, nil, errors.New("hosted agents: body is required")
	}

	path := fmt.Sprintf(hostedAgentSessionWorkspaceUploadPath, sessionID)
	u, err := s.client.BaseURL.Parse(path)
	if err != nil {
		return nil, nil, err
	}
	q := url.Values{}
	q.Set("path", input.Path)
	if input.IsArchive {
		q.Set("is_archive", "true")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), input.Body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", hostedAgentWorkspaceMediaType)
	req.Header.Set("Accept", mediaType)
	req.Header.Set("User-Agent", s.client.UserAgent)
	if input.ContentSHA256 != "" {
		req.Header.Set(workspaceContentSHA256Header, input.ContentSHA256)
	}

	root := new(HostedAgentWorkspaceUploadResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// DownloadWorkspace streams a file (or tar archive) out of the session
// workspace. Callers MUST read the returned Body to EOF and then Close it;
// the body strips the trailing integrity footer and verifies SHA-256 of the
// payload.
func (s *HostedAgentsServiceOp) DownloadWorkspace(ctx context.Context, sessionID string, input *HostedAgentWorkspaceDownloadRequest) (*HostedAgentWorkspaceDownload, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if input == nil {
		return nil, nil, errors.New("hosted agents: download request is required")
	}
	if input.Path == "" {
		return nil, nil, errors.New("hosted agents: path is required")
	}

	path := fmt.Sprintf(hostedAgentSessionWorkspaceDownloadPath, sessionID)
	q := url.Values{}
	q.Set("path", input.Path)
	if input.AsArchive {
		q.Set("as_archive", "true")
	}
	path += "?" + q.Encode()

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", hostedAgentWorkspaceMediaType)

	resp, err := s.client.DoStream(ctx, req)
	if err != nil {
		return nil, resp, err
	}

	out := &HostedAgentWorkspaceDownload{
		Body: &workspaceDownloadBody{
			body:   resp.Body,
			hasher: sha256.New(),
		},
	}
	if archive, perr := strconv.ParseBool(strings.TrimSpace(resp.Header.Get(workspaceIsArchiveHeader))); perr == nil {
		out.IsArchive = archive
	}
	if hint := strings.TrimSpace(resp.Header.Get(workspaceSizeBytesHeader)); hint != "" {
		if n, perr := strconv.ParseInt(hint, 10, 64); perr == nil {
			out.SizeBytes = n
		}
	}
	return out, resp, nil
}

// CreateWorkspaceTransfer starts a large-file staged upload or download.
// For uploads, response includes part_size for client-side chunking.
func (s *HostedAgentsServiceOp) CreateWorkspaceTransfer(ctx context.Context, sessionID string, input *HostedAgentWorkspaceTransferCreateRequest) (*HostedAgentWorkspaceTransfer, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if input == nil {
		return nil, nil, errors.New("hosted agents: transfer create request is required")
	}
	if input.Direction == "" {
		return nil, nil, errors.New("hosted agents: direction is required")
	}
	if input.Path == "" {
		return nil, nil, errors.New("hosted agents: path is required")
	}

	path := fmt.Sprintf(hostedAgentSessionWorkspaceTransfersPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentWorkspaceTransfer)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CreateWorkspaceTransferPartUploadURLs returns presigned URLs to PUT one or more
// upload parts. Upload only. Part numbers start at 1. If a URL expires, request
// that part_number again.
func (s *HostedAgentsServiceOp) CreateWorkspaceTransferPartUploadURLs(ctx context.Context, sessionID, transferID string, input *HostedAgentWorkspaceTransferPartUploadURLsRequest) (*HostedAgentWorkspaceTransferPartUploadURLs, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if transferID == "" {
		return nil, nil, errors.New("hosted agents: transfer id is required")
	}
	if input == nil {
		return nil, nil, errors.New("hosted agents: part upload URLs request is required")
	}
	if len(input.PartNumbers) == 0 {
		return nil, nil, errors.New("hosted agents: part_numbers must not be empty")
	}
	for _, n := range input.PartNumbers {
		if n < 1 {
			return nil, nil, errors.New("hosted agents: part_numbers must all be >= 1")
		}
	}

	path := fmt.Sprintf(hostedAgentSessionWorkspaceTransferPartURLsPath, sessionID, transferID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentWorkspaceTransferPartUploadURLs)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CommitWorkspaceTransfer finalizes an upload after all parts have been PUT and
// starts applying the file into the workspace. Poll GetWorkspaceTransfer afterward.
func (s *HostedAgentsServiceOp) CommitWorkspaceTransfer(ctx context.Context, sessionID, transferID string, input *HostedAgentWorkspaceTransferCommitRequest) (*HostedAgentWorkspaceTransfer, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if transferID == "" {
		return nil, nil, errors.New("hosted agents: transfer id is required")
	}
	if input == nil {
		input = &HostedAgentWorkspaceTransferCommitRequest{}
	}

	path := fmt.Sprintf(hostedAgentSessionWorkspaceTransferCommitPath, sessionID, transferID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentWorkspaceTransfer)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetWorkspaceTransfer polls transfer status. On a completed download, DownloadURL
// and SHA256 are set; verify SHA256(file) == SHA256 after fetching DownloadURL.
// This path has no DOWSSHA1 body footer.
func (s *HostedAgentsServiceOp) GetWorkspaceTransfer(ctx context.Context, sessionID, transferID string) (*HostedAgentWorkspaceTransfer, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if transferID == "" {
		return nil, nil, errors.New("hosted agents: transfer id is required")
	}

	path := fmt.Sprintf(hostedAgentSessionWorkspaceTransferByIDPath, sessionID, transferID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentWorkspaceTransfer)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CancelWorkspaceTransfer aborts an in-flight transfer. Idempotent.
func (s *HostedAgentsServiceOp) CancelWorkspaceTransfer(ctx context.Context, sessionID, transferID string, input *HostedAgentWorkspaceTransferCancelRequest) (*HostedAgentWorkspaceTransferCancelResponse, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if transferID == "" {
		return nil, nil, errors.New("hosted agents: transfer id is required")
	}
	if input == nil {
		input = &HostedAgentWorkspaceTransferCancelRequest{}
	}

	path := fmt.Sprintf(hostedAgentSessionWorkspaceTransferCancelPath, sessionID, transferID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, input)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentWorkspaceTransferCancelResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// workspaceDownloadBody holds back the trailing integrity footer while
// streaming, hashes the payload, and verifies the footer at EOF.
type workspaceDownloadBody struct {
	body     io.ReadCloser
	hasher   hash.Hash
	pending  []byte
	scratch  []byte
	sawEOF   bool
	verified bool
	verr     error
}

func (b *workspaceDownloadBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.verified {
		if b.verr != nil {
			return 0, b.verr
		}
		return 0, io.EOF
	}

	for {
		if overflow := len(b.pending) - workspaceDownloadFooterLen; overflow > 0 {
			n := overflow
			if n > len(p) {
				n = len(p)
			}
			copy(p, b.pending[:n])
			b.hasher.Write(b.pending[:n])
			b.pending = b.pending[n:]
			return n, nil
		}

		if b.sawEOF {
			if err := b.verifyFooter(); err != nil {
				return 0, err
			}
			return 0, io.EOF
		}

		if b.scratch == nil {
			b.scratch = make([]byte, 32*1024)
		}
		nr, err := b.body.Read(b.scratch)
		if nr > 0 {
			b.pending = append(b.pending, b.scratch[:nr]...)
		}
		if errors.Is(err, io.EOF) {
			b.sawEOF = true
			continue
		}
		if err != nil {
			return 0, err
		}
	}
}

func (b *workspaceDownloadBody) verifyFooter() error {
	if b.verified {
		return b.verr
	}
	b.verified = true

	if len(b.pending) != workspaceDownloadFooterLen {
		b.verr = errors.New("hosted agents: missing or truncated workspace download integrity footer")
		return b.verr
	}
	want, ok := parseWorkspaceDownloadFooter(b.pending)
	if !ok {
		b.verr = errors.New("hosted agents: invalid workspace download integrity footer")
		return b.verr
	}
	got := hex.EncodeToString(b.hasher.Sum(nil))
	if !strings.EqualFold(want, got) {
		b.verr = fmt.Errorf("hosted agents: workspace download checksum mismatch: want %q, got %q", want, got)
		return b.verr
	}
	b.pending = nil
	return nil
}

// parseWorkspaceDownloadFooter validates DOWSSHA1<64-hex>\n and returns the digest.
func parseWorkspaceDownloadFooter(footer []byte) (string, bool) {
	if len(footer) != workspaceDownloadFooterLen {
		return "", false
	}
	if string(footer[:len(workspaceDownloadFooterPrefix)]) != workspaceDownloadFooterPrefix {
		return "", false
	}
	if footer[workspaceDownloadFooterLen-1] != '\n' {
		return "", false
	}
	digest := string(footer[len(workspaceDownloadFooterPrefix) : workspaceDownloadFooterLen-1])
	for i := 0; i < len(digest); i++ {
		c := digest[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return "", false
		}
	}
	return digest, true
}

func (b *workspaceDownloadBody) Close() error {
	if cerr := b.body.Close(); cerr != nil {
		return cerr
	}
	if b.verified {
		return b.verr
	}
	return nil
}

// HostedAgentSessionStream is a typed iterator over a session SSE stream.
type HostedAgentSessionStream struct {
	raw     *SSEReader
	body    io.ReadCloser
	current HostedAgentEvent
	err     error
	done    bool
	hasMore bool
}

// hasMoreComment is the SSE comment a history page (see
// HostedAgentSessionStreamOptions.Before) ends with, reporting whether events
// older than the page remain.
const hasMoreComment = "has_more="

// observeComment records the has_more trailer of a history page. Wired as the
// reader's OnComment hook, so it runs while Next drains the stream.
func (s *HostedAgentSessionStream) observeComment(comment []byte) {
	value, ok := strings.CutPrefix(string(comment), hasMoreComment)
	if !ok {
		return
	}
	s.hasMore = value == "true"
}

// HasMore reports whether the history page just read has older events behind
// it. Only meaningful once a stream opened with
// HostedAgentSessionStreamOptions.Before has been drained to its end: the
// trailer arrives after the last event.
func (s *HostedAgentSessionStream) HasMore() bool { return s.hasMore }

// Next advances to the next event. Returns false on EOF or error.
func (s *HostedAgentSessionStream) Next() bool {
	for {
		if s.done || s.err != nil {
			return false
		}
		ev, err := s.raw.Next()
		if errors.Is(err, io.EOF) {
			s.done = true
			return false
		}
		if err != nil {
			s.err = err
			return false
		}
		if len(ev.Data) == 0 {
			continue
		}
		var event HostedAgentEvent
		if err := json.Unmarshal(ev.Data, &event); err != nil {
			s.err = err
			return false
		}
		if event.EventID == "" && ev.ID != "" {
			event.EventID = ev.ID
		}
		if event.Kind == "" && ev.Event != "" {
			event.Kind = HostedAgentEventKind(ev.Event)
		}
		s.current = event
		return true
	}
}

// Current returns the most recent event produced by Next.
func (s *HostedAgentSessionStream) Current() HostedAgentEvent { return s.current }

// Err returns any non-EOF error encountered during iteration.
func (s *HostedAgentSessionStream) Err() error { return s.err }

// Close releases the underlying HTTP response body. Always call Close.
func (s *HostedAgentSessionStream) Close() error {
	if s.body == nil {
		return nil
	}
	return s.body.Close()
}
