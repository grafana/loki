package godo

import (
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
	microVMBasePath            = "v2/microvms"
	microVMCheckpointsBasePath = "v2/microvms/checkpoints"
	microVMOptionsPath         = "v2/microvms/options"
)

// MicroVMState represents the lifecycle state of a MicroVM.
type MicroVMState string

// Possible lifecycle states for a MicroVM.
const (
	MicroVMStateUnknown     = MicroVMState("unknown")
	MicroVMStateCreating    = MicroVMState("creating")
	MicroVMStateRunning     = MicroVMState("running")
	MicroVMStatePausing     = MicroVMState("pausing")
	MicroVMStatePaused      = MicroVMState("paused")
	MicroVMStateResuming    = MicroVMState("resuming")
	MicroVMStateTerminating = MicroVMState("terminating")
	MicroVMStateTerminated  = MicroVMState("terminated")
	MicroVMStateFailed      = MicroVMState("failed")
)

// MicroVMNetworking represents the networking mode of a MicroVM.
type MicroVMNetworking string

// Possible networking modes for a MicroVM.
const (
	MicroVMNetworkingUnknown = MicroVMNetworking("unknown")
	MicroVMNetworkingPublic  = MicroVMNetworking("public")
	MicroVMNetworkingVPC     = MicroVMNetworking("vpc")
)

// MicroVMHTTPProtocol represents the HTTP protocol option for a MicroVM.
type MicroVMHTTPProtocol string

// Possible HTTP protocol values for a MicroVM.
const (
	MicroVMHTTPProtocolHTTP  = MicroVMHTTPProtocol("http")
	MicroVMHTTPProtocolHTTP2 = MicroVMHTTPProtocol("http2")
)

// MicroVMURLStatus represents the lifecycle of a MicroVM URL.
type MicroVMURLStatus string

// Possible statuses for a MicroVM URL.
const (
	MicroVMURLStatusPending = MicroVMURLStatus("PENDING")
	MicroVMURLStatusActive  = MicroVMURLStatus("ACTIVE")
)

// MicroVMCheckpointStatus represents the status of a MicroVM checkpoint.
type MicroVMCheckpointStatus string

// Possible states for a MicroVM checkpoint.
const (
	MicroVMCheckpointStatusUnknown   = MicroVMCheckpointStatus("CHECKPOINT_UNKNOWN")
	MicroVMCheckpointStatusCreating  = MicroVMCheckpointStatus("CHECKPOINT_CREATING")
	MicroVMCheckpointStatusAvailable = MicroVMCheckpointStatus("CHECKPOINT_AVAILABLE")
	MicroVMCheckpointStatusFailed    = MicroVMCheckpointStatus("CHECKPOINT_FAILED")
	MicroVMCheckpointStatusDeleted   = MicroVMCheckpointStatus("CHECKPOINT_DELETED")
	MicroVMCheckpointStatusDeleting  = MicroVMCheckpointStatus("CHECKPOINT_DELETING")
)

// MicroVMsService is an interface for interfacing with the MicroVM
// endpoints of the DigitalOcean API.
// See: https://docs.digitalocean.com/reference/api/api-reference/#tag/MicroVMs
type MicroVMsService interface {
	List(ctx context.Context, opt *ListOptions) ([]MicroVM, *Response, error)
	ListByRegion(ctx context.Context, region string, opt *ListOptions) ([]MicroVM, *Response, error)
	ListByName(ctx context.Context, name string, opt *ListOptions) ([]MicroVM, *Response, error)
	ListByTag(ctx context.Context, tag string, opt *ListOptions) ([]MicroVM, *Response, error)
	ListFiltered(ctx context.Context, opt *ListMicroVMsOptions) ([]MicroVM, *Response, error)
	Get(ctx context.Context, id string) (*MicroVM, *Response, error)
	Create(ctx context.Context, createRequest *MicroVMCreateRequest) (*MicroVM, *Response, error)
	Pause(ctx context.Context, id string) (*MicroVM, *Response, error)
	Resume(ctx context.Context, id string) (*MicroVM, *Response, error)
	Delete(ctx context.Context, id string) (*Response, error)

	AddURL(ctx context.Context, microVMID string, createRequest *MicroVMURLCreateRequest) (*MicroVMURL, *Response, error)
	RemoveURL(ctx context.Context, microVMID, urlID string) (*Response, error)

	ListCheckpoints(ctx context.Context, opt *ListMicroVMCheckpointsOptions) ([]MicroVMCheckpoint, *Response, error)
	CreateCheckpoint(ctx context.Context, microVMID string, createRequest *MicroVMCheckpointCreateRequest) (*MicroVMCheckpoint, *Response, error)
	GetCheckpoint(ctx context.Context, id string) (*MicroVMCheckpoint, *Response, error)
	DeleteCheckpoint(ctx context.Context, id string) (*Response, error)

	GetCreateOptions(ctx context.Context) (*MicroVMCreateOptions, *Response, error)

	Exec(ctx context.Context, id string, execRequest *MicroVMExecRequest) (*MicroVMExecResult, *Response, error)
	ConsoleURL(id string, opt *MicroVMConsoleOptions) (string, error)

	UploadFile(ctx context.Context, id string, uploadRequest *MicroVMFileUploadRequest) (*MicroVMFileInfo, *Response, error)
	DownloadFile(ctx context.Context, id, path string) (*MicroVMFileDownload, *Response, error)
}

// MicroVMsServiceOp handles communication with the MicroVM related
// methods of the DigitalOcean API.
type MicroVMsServiceOp struct {
	client *Client
}

var _ MicroVMsService = &MicroVMsServiceOp{}

// MicroVMSize is the compute capacity reported on a MicroVM.
type MicroVMSize struct {
	CPU    uint32 `json:"cpu,omitempty"`
	Memory uint32 `json:"memory,omitempty"`
	Disk   uint64 `json:"disk,omitempty"`
}

// MicroVMSizeRequest is the compute capacity requested at create time.
// Disk is provisioned with the size and is not requested.
type MicroVMSizeRequest struct {
	CPU    uint32 `json:"cpu"`
	Memory uint32 `json:"memory"`
}

// MicroVMSource names what a MicroVM runs. Exactly one of OCIRef or
// CheckpointID must be set on create; the API rejects both or neither.
type MicroVMSource struct {
	OCIRef       string `json:"oci_ref,omitempty"`
	CheckpointID string `json:"checkpoint_id,omitempty"`
}

// MicroVMURL is one ingress URL attached to a MicroVM.
type MicroVMURL struct {
	// ID is the stable identifier of the URL. Use it to remove the URL with
	// MicroVMsService.RemoveURL.
	ID       string           `json:"id,omitempty"`
	Hostname string           `json:"hostname,omitempty"`
	Port     int              `json:"port,omitempty"`
	Default  bool             `json:"default,omitempty"`
	Status   MicroVMURLStatus `json:"status,omitempty"`
}

// MicroVMURLCreateRequest is the payload for adding a URL to a MicroVM.
type MicroVMURLCreateRequest struct {
	// Port is the guest port the URL routes to. It must be one of the
	// MicroVM's Ports. When zero, the API defaults it to the MicroVM's
	// HTTPPort.
	Port int `json:"port,omitempty"`
}

// MicroVM represents a DigitalOcean MicroVM.
type MicroVM struct {
	ID            string              `json:"id,omitempty"`
	Name          string              `json:"name,omitempty"`
	Region        string              `json:"region,omitempty"`
	State         MicroVMState        `json:"state,omitempty"`
	Size          *MicroVMSize        `json:"size,omitempty"`
	URLs          []MicroVMURL        `json:"urls,omitempty"`
	Ports         []uint32            `json:"ports,omitempty"`
	FailureReason string              `json:"failure_reason,omitempty"`
	Networking    MicroVMNetworking   `json:"networking,omitempty"`
	Source        *MicroVMSource      `json:"source,omitempty"`
	AutoPause     *AutoPauseConfig    `json:"auto_pause,omitempty"`
	AutoResume    *bool               `json:"auto_resume,omitempty"`
	HTTPProtocol  MicroVMHTTPProtocol `json:"http_protocol,omitempty"`
	Created       string              `json:"created_at,omitempty"`
	Tags          []string            `json:"tags,omitempty"`
	// EgressPolicy is the outbound allow list set at create, as supplied.
	// Nil when the MicroVM has no policy and egress is unrestricted.
	EgressPolicy *MicroVMEgressPolicy `json:"egress_policy,omitempty"`
}

// MicroVMEgressAction is what happens to a destination no egress rule matches.
type MicroVMEgressAction string

const (
	// MicroVMEgressActionDeny refuses unmatched destinations. It is the API default.
	MicroVMEgressActionDeny = MicroVMEgressAction("deny")
	// MicroVMEgressActionAllow permits unmatched destinations.
	MicroVMEgressActionAllow = MicroVMEgressAction("allow")
)

// MicroVMEgressPolicy is an outbound allow list for a MicroVM. A destination
// that matches any rule is allowed; DefaultAction decides every other one.
type MicroVMEgressPolicy struct {
	DefaultAction MicroVMEgressAction `json:"default_action,omitempty"`
	Rules         []MicroVMEgressRule `json:"rules,omitempty"`
}

// MicroVMEgressRule is one allowed destination. Set exactly one of Domain
// (an exact host or a `*.example.com` wildcard) or IP (an exact address;
// CIDR ranges are not supported).
type MicroVMEgressRule struct {
	Domain string `json:"domain,omitempty"`
	IP     string `json:"ip,omitempty"`
}

// AutoPauseConfig configures MicroVM auto-pause behavior. IdleTimeout is
// a Go duration string (e.g. "5m", "30s") describing how long the MicroVM
// must be idle before it is paused.
type AutoPauseConfig struct {
	Enabled     *bool  `json:"enabled,omitempty"`
	IdleTimeout string `json:"idle_timeout,omitempty"`
}

// MicroVMCheckpoint represents a checkpoint of a MicroVM
// (persisted memory + disk state).
type MicroVMCheckpoint struct {
	ID          string `json:"id,omitempty"`
	MicroVMID   string `json:"microvm_id,omitempty"`
	MicroVMName string `json:"microvm_name,omitempty"`
	Name        string `json:"name,omitempty"`
	Region      string `json:"region,omitempty"`
	// Size is the guest shape this checkpoint restores at. Omitted when the
	// checkpoint does not record one; a create from it must then name a size.
	Size        *MicroVMSize            `json:"size,omitempty"`
	Status      MicroVMCheckpointStatus `json:"status,omitempty"`
	MemoryBytes uint64                  `json:"memory_bytes,omitempty"`
	DiskBytes   uint64                  `json:"disk_bytes,omitempty"`
	Created     string                  `json:"created_at,omitempty"`
}

// MicroVMCreateRequest represents a request to create a MicroVM.
// Size and Region may be omitted when restoring from a checkpoint so the API
// can inherit them from the checkpoint (api-v2 C6).
type MicroVMCreateRequest struct {
	Name         string              `json:"name"`
	Region       string              `json:"region,omitempty"`
	Size         *MicroVMSizeRequest `json:"size,omitempty"`
	Source       *MicroVMSource      `json:"source"`
	Networking   MicroVMNetworking   `json:"networking,omitempty"`
	VPCUUID      string              `json:"vpc_uuid,omitempty"`
	AutoPause    *AutoPauseConfig    `json:"auto_pause,omitempty"`
	AutoResume   *bool               `json:"auto_resume,omitempty"`
	HTTPPort     uint32              `json:"http_port,omitempty"`
	HTTPProtocol MicroVMHTTPProtocol `json:"http_protocol,omitempty"`
	Ports        []uint32            `json:"ports,omitempty"`
	Environment  map[string]string   `json:"environment,omitempty"`
	Tags         []string            `json:"tags,omitempty"`
	// Ingress controls whether the MicroVM is created with a default public
	// URL. It defaults to true when omitted (nil). Set it to false to create
	// the MicroVM without a default URL; URLs can be added later with
	// MicroVMsService.AddURL.
	Ingress *bool `json:"ingress,omitempty"`
	// EgressPolicy restricts where code in the MicroVM can connect. Nil leaves
	// egress unrestricted, or inherits the origin's policy on a checkpoint
	// restore. Only valid with public networking and fixed at create.
	EgressPolicy *MicroVMEgressPolicy `json:"egress_policy,omitempty"`
}

// MicroVMCheckpointCreateRequest represents a request to create a checkpoint.
type MicroVMCheckpointCreateRequest struct {
	Name string `json:"name,omitempty"`
}

// ListMicroVMCheckpointsOptions are the optional parameters for listing
// checkpoints. MicroVMID filters to checkpoints captured from that
// MicroVM; omit it to list all checkpoints for the team.
type ListMicroVMCheckpointsOptions struct {
	ListOptions
	MicroVMID string `url:"microvm_id,omitempty"`
}

// MicroVMCreateOptions is the response from GET /v2/microvms/options.
type MicroVMCreateOptions struct {
	DefaultRegion string                 `json:"default_region,omitempty"`
	Sizes         []MicroVMSizeOption    `json:"sizes,omitempty"`
	Features      []MicroVMFeatureOption `json:"features,omitempty"`
	AccountLimits *MicroVMAccountLimits  `json:"account_limits,omitempty"`
}

// MicroVMSizeOption is one supported size evaluated for the team. Regions
// holds the slugs where the team can create this size; a region it cannot use
// is absent rather than listed as unavailable.
type MicroVMSizeOption struct {
	CPU       uint32              `json:"cpu"`
	Memory    uint32              `json:"memory"`
	Disk      uint64              `json:"disk"`
	Available bool                `json:"available"`
	Regions   []string            `json:"regions"`
	Pricing   *MicroVMSizePricing `json:"pricing,omitempty"`
}

// MicroVMSizePricing is the USD price of one size.
type MicroVMSizePricing struct {
	PricePerHour  float64 `json:"price_per_hour"`
	PricePerMonth float64 `json:"price_per_month"`
}

// MicroVMFeatureOption is one product feature gate for the team.
type MicroVMFeatureOption struct {
	Name    string `json:"name,omitempty"`
	Enabled bool   `json:"enabled"`
}

// MicroVMAccountLimits are the team's effective MicroVM limits.
type MicroVMAccountLimits struct {
	MaxConcurrentRunning  uint64 `json:"max_concurrent_running,omitempty"`
	MaxTotalCount         uint64 `json:"max_total_count,omitempty"`
	MaxMemoryBytes        uint64 `json:"max_memory_bytes,omitempty"`
	MaxDiskBytes          uint64 `json:"max_disk_bytes,omitempty"`
	MaxIdleTimeoutSeconds uint64 `json:"max_idle_timeout_seconds,omitempty"`
}

// MicroVMExecRequest is a one-shot, non-PTY command run in a MicroVM's
// workload container. Argv must be non-empty (e.g. ["echo", "hi"]).
type MicroVMExecRequest struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd,omitempty"`
}

// MicroVMExecResult is the outcome of a one-shot exec. A non-zero ExitCode
// is not a client error — the API returns HTTP 200 with the exit code set.
// Truncated is true when the server clipped stdout/stderr to its output bound.
type MicroVMExecResult struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int32  `json:"exit_code"`
	Truncated bool   `json:"truncated,omitempty"`
}

// MicroVMConsoleOptions sets the initial PTY size for ConsoleURL.
// The server defaults to 24 rows by 80 columns when either is zero / unset.
type MicroVMConsoleOptions struct {
	Rows uint32
	Cols uint32
}

// Stable console WebSocket error.code values (server → client text frames).
const (
	MicroVMConsoleErrDialFailed  = "dial_failed"
	MicroVMConsoleErrStartFailed = "start_failed"
	MicroVMConsoleErrAgentError  = "agent_error"
)

// MicroVMConsoleControl is a decoded server → client text control frame.
// Exactly one of Error, Exit, or Status is set for a well-formed frame.
type MicroVMConsoleControl struct {
	Error  *MicroVMConsoleError  `json:"error,omitempty"`
	Exit   *MicroVMConsoleExit   `json:"exit,omitempty"`
	Status *MicroVMConsoleStatus `json:"status,omitempty"`
}

// MicroVMConsoleError is the payload of an {"error":...} control frame.
type MicroVMConsoleError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// MicroVMConsoleExit is the payload of an {"exit":...} control frame.
type MicroVMConsoleExit struct {
	Code int32 `json:"code"`
}

// MicroVMConsoleStatus is the payload of a {"status":...} control frame
// (e.g. state "resuming" while a paused MicroVM is auto-resumed).
type MicroVMConsoleStatus struct {
	State string `json:"state"`
}

// MicroVMFileUploadRequest is the input for UploadFile.
type MicroVMFileUploadRequest struct {
	// Path is the absolute destination inside the workload container. Its
	// parent directory must already exist. Required.
	Path string
	// Body is the raw file content. nil writes an empty file.
	Body io.Reader
	// Size is the exact number of bytes Body yields. The API requires
	// Content-Length and refuses chunked bodies, so Size must be set unless
	// Body is a *bytes.Buffer, *bytes.Reader, or *strings.Reader, whose
	// length is known. Must not exceed 64 MiB.
	Size int64
	// Mode is the octal permission string for the file, "0001" to "0777"
	// (e.g. "0600"). Empty means the API default of "0644". Setuid, setgid,
	// sticky, and "0000" are rejected by the API.
	Mode string
	// UID and GID set the file owner. Both or neither must be set; the
	// default is the workload container's configured user.
	UID *uint32
	GID *uint32
	// SHA256 is the optional lowercase hex digest of Body. When set, the API
	// verifies the content before replacing the target and returns 422
	// CHECKSUM_MISMATCH on a mismatch, leaving the target untouched.
	SHA256 string
}

// MicroVMFileInfo is the metadata of a file written into a MicroVM.
type MicroVMFileInfo struct {
	Path      string `json:"path"`
	SizeBytes uint64 `json:"size_bytes"`
	// SHA256 is the lowercase hex digest of the content as written.
	SHA256 string `json:"sha256"`
	// Mode is the octal permission string as written, e.g. "0644".
	Mode string `json:"mode"`
	UID  uint32 `json:"uid"`
	GID  uint32 `json:"gid"`
}

// MicroVMFileDownload is the streaming result of DownloadFile. Callers must
// read Body to EOF and Close it. Body verifies the content against SHA256 as
// it is read: the Read that reaches EOF returns ErrMicroVMFileChecksum instead
// of io.EOF when the digest does not match.
type MicroVMFileDownload struct {
	Body io.ReadCloser
	// SizeBytes is the content length announced by the API, or -1 when it
	// was not sent.
	SizeBytes int64
	// SHA256 is the lowercase hex digest announced in X-Checksum-Sha256,
	// describing the file as it was when the transfer began.
	SHA256 string
}

// ErrMicroVMFileChecksum is returned by MicroVMFileDownload.Body when the
// bytes read do not hash to the digest the API announced.
var ErrMicroVMFileChecksum = errors.New("microvm file download: sha256 mismatch")

// String returns a human-readable description of a MicroVMFileInfo.
func (f MicroVMFileInfo) String() string {
	return Stringify(f)
}

// String returns a human-readable description of a MicroVM.
func (m MicroVM) String() string {
	return Stringify(m)
}

// URN returns the MicroVM ID in a valid DO API URN form.
func (m MicroVM) URN() string {
	return ToURN("MicroVM", m.ID)
}

// String returns a human-readable description of a MicroVMCheckpoint.
func (c MicroVMCheckpoint) String() string {
	return Stringify(c)
}

// String returns a human-readable description of a MicroVMCreateRequest.
func (r MicroVMCreateRequest) String() string {
	return Stringify(r)
}

type microVMRoot struct {
	MicroVM *MicroVM `json:"microvm"`
}

type microVMURLRoot struct {
	URL *MicroVMURL `json:"url"`
}

type microVMsRoot struct {
	MicroVMs []MicroVM `json:"microvms"`
	Links    *Links    `json:"links"`
	Meta     *Meta     `json:"meta"`
}

type microVMCheckpointRoot struct {
	Checkpoint *MicroVMCheckpoint `json:"checkpoint"`
}

type microVMCheckpointsRoot struct {
	Checkpoints []MicroVMCheckpoint `json:"checkpoints"`
	Links       *Links              `json:"links"`
	Meta        *Meta               `json:"meta"`
}

// ListMicroVMsOptions filters GET /v2/microvms. Pagination lives on the
// embedded ListOptions. Region, Name, and TagName are omitted when empty
// and combine: the API ANDs whichever are set.
type ListMicroVMsOptions struct {
	ListOptions
	Region  string `url:"region,omitempty"`
	Name    string `url:"name,omitempty"`
	TagName string `url:"tag_name,omitempty"`
}

// List lists all MicroVMs, with optional pagination.
func (s *MicroVMsServiceOp) List(ctx context.Context, opt *ListOptions) ([]MicroVM, *Response, error) {
	var filtered *ListMicroVMsOptions
	if opt != nil {
		filtered = &ListMicroVMsOptions{ListOptions: *opt}
	}
	return s.ListFiltered(ctx, filtered)
}

// ListByRegion lists MicroVMs filtered by region slug, with optional pagination.
func (s *MicroVMsServiceOp) ListByRegion(ctx context.Context, region string, opt *ListOptions) ([]MicroVM, *Response, error) {
	if region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
	}
	filtered := &ListMicroVMsOptions{Region: region}
	if opt != nil {
		filtered.ListOptions = *opt
	}
	return s.ListFiltered(ctx, filtered)
}

// ListByName lists MicroVMs filtered by exact name match, with optional pagination.
func (s *MicroVMsServiceOp) ListByName(ctx context.Context, name string, opt *ListOptions) ([]MicroVM, *Response, error) {
	if name == "" {
		return nil, nil, NewArgError("name", "cannot be empty")
	}
	filtered := &ListMicroVMsOptions{Name: name}
	if opt != nil {
		filtered.ListOptions = *opt
	}
	return s.ListFiltered(ctx, filtered)
}

// ListByTag lists MicroVMs that carry the given resource tag. A tag that
// matches nothing returns an empty list rather than an error.
func (s *MicroVMsServiceOp) ListByTag(ctx context.Context, tag string, opt *ListOptions) ([]MicroVM, *Response, error) {
	if tag == "" {
		return nil, nil, NewArgError("tag", "cannot be empty")
	}
	filtered := &ListMicroVMsOptions{TagName: tag}
	if opt != nil {
		filtered.ListOptions = *opt
	}
	return s.ListFiltered(ctx, filtered)
}

// ListFiltered lists MicroVMs using any combination of region, name, and tag.
func (s *MicroVMsServiceOp) ListFiltered(ctx context.Context, opt *ListMicroVMsOptions) ([]MicroVM, *Response, error) {
	path := microVMBasePath
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microVMsRoot)
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

	return root.MicroVMs, resp, nil
}

// Get retrieves a MicroVM by its ID.
func (s *MicroVMsServiceOp) Get(ctx context.Context, id string) (*MicroVM, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", microVMBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microVMRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.MicroVM, resp, nil
}

// Create provisions a new MicroVM with the provided configuration.
func (s *MicroVMsServiceOp) Create(ctx context.Context, createRequest *MicroVMCreateRequest) (*MicroVM, *Response, error) {
	if createRequest == nil {
		return nil, nil, NewArgError("createRequest", "cannot be nil")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, microVMBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(microVMRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.MicroVM, resp, nil
}

// Pause synchronously transitions a RUNNING MicroVM to PAUSED. It blocks
// until the platform has durably paused the MicroVM and returns the
// updated resource. The call is idempotent: pausing a MicroVM that is
// already PAUSED returns the current MicroVM with no side effects.
func (s *MicroVMsServiceOp) Pause(ctx context.Context, id string) (*MicroVM, *Response, error) {
	return s.doTransition(ctx, id, "pause")
}

// Resume synchronously transitions a PAUSED MicroVM to RUNNING. It blocks
// until the platform has durably resumed the MicroVM and returns the
// updated resource. The call is idempotent: resuming a MicroVM that is
// already RUNNING returns the current MicroVM with no side effects.
func (s *MicroVMsServiceOp) Resume(ctx context.Context, id string) (*MicroVM, *Response, error) {
	return s.doTransition(ctx, id, "resume")
}

// doTransition posts an empty body to a MicroVM transition sub-resource
// (e.g. /pause, /resume) and decodes the returned MicroVM.
func (s *MicroVMsServiceOp) doTransition(ctx context.Context, id, action string) (*MicroVM, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/%s", microVMBasePath, id, action)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microVMRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.MicroVM, resp, nil
}

// Delete removes a MicroVM by its ID. The DigitalOcean API returns a 204
// on success and does not include a response body.
func (s *MicroVMsServiceOp) Delete(ctx context.Context, id string) (*Response, error) {
	if id == "" {
		return nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", microVMBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}

// AddURL mints a new public ingress URL for a running or paused MicroVM. The
// URL routes to createRequest.Port, which must be one of the MicroVM's Ports;
// when Port is zero the API defaults it to the MicroVM's HTTPPort. The call
// returns once the URL is recorded; its Status is PENDING until the route is
// published. A MicroVM can have at most 10 URLs, the default one included.
// createRequest may be nil to add a URL on the default port.
func (s *MicroVMsServiceOp) AddURL(ctx context.Context, microVMID string, createRequest *MicroVMURLCreateRequest) (*MicroVMURL, *Response, error) {
	if microVMID == "" {
		return nil, nil, NewArgError("microVMID", "cannot be empty")
	}
	if createRequest == nil {
		createRequest = &MicroVMURLCreateRequest{}
	}

	path := fmt.Sprintf("%s/%s/urls", microVMBasePath, microVMID)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(microVMURLRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.URL, resp, nil
}

// RemoveURL removes a URL from a running or paused MicroVM, the default one
// included. The hostname stops resolving and is not reused. Removing the last
// URL leaves the MicroVM as if it had been created with Ingress set to false.
// The DigitalOcean API returns a 204 on success with no response body.
func (s *MicroVMsServiceOp) RemoveURL(ctx context.Context, microVMID, urlID string) (*Response, error) {
	if microVMID == "" {
		return nil, NewArgError("microVMID", "cannot be empty")
	}
	if urlID == "" {
		return nil, NewArgError("urlID", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/urls/%s", microVMBasePath, microVMID, urlID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}

// ListCheckpoints lists checkpoints for the authenticated team. Pass a
// MicroVMID on opt to filter to checkpoints captured from that MicroVM.
func (s *MicroVMsServiceOp) ListCheckpoints(ctx context.Context, opt *ListMicroVMCheckpointsOptions) ([]MicroVMCheckpoint, *Response, error) {
	path := microVMCheckpointsBasePath
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microVMCheckpointsRoot)
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

	return root.Checkpoints, resp, nil
}

// CreateCheckpoint starts an asynchronous checkpoint of a running MicroVM.
func (s *MicroVMsServiceOp) CreateCheckpoint(ctx context.Context, microVMID string, createRequest *MicroVMCheckpointCreateRequest) (*MicroVMCheckpoint, *Response, error) {
	if microVMID == "" {
		return nil, nil, NewArgError("microVMID", "cannot be empty")
	}
	if createRequest == nil {
		createRequest = &MicroVMCheckpointCreateRequest{}
	}

	path := fmt.Sprintf("%s/%s/checkpoints", microVMBasePath, microVMID)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(microVMCheckpointRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Checkpoint, resp, nil
}

// GetCheckpoint retrieves a checkpoint by its ID.
func (s *MicroVMsServiceOp) GetCheckpoint(ctx context.Context, id string) (*MicroVMCheckpoint, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", microVMCheckpointsBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microVMCheckpointRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Checkpoint, resp, nil
}

// DeleteCheckpoint releases the state stored by a checkpoint. Returns a 204
// on success with no response body.
func (s *MicroVMsServiceOp) DeleteCheckpoint(ctx context.Context, id string) (*Response, error) {
	if id == "" {
		return nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", microVMCheckpointsBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}

// GetCreateOptions returns the sizes, the regions each one can be created in,
// the feature gates, and the account limits available to the authenticated
// team when creating a MicroVM.
func (s *MicroVMsServiceOp) GetCreateOptions(ctx context.Context) (*MicroVMCreateOptions, *Response, error) {
	req, err := s.client.NewRequest(ctx, http.MethodGet, microVMOptionsPath, nil)
	if err != nil {
		return nil, nil, err
	}

	opts := new(MicroVMCreateOptions)
	resp, err := s.client.Do(ctx, req, opts)
	if err != nil {
		return nil, resp, err
	}

	return opts, resp, nil
}

// Exec runs a one-shot, non-PTY command in the MicroVM's workload container
// and returns its stdout, stderr, and exit code. Requires the exec_pty feature
// gate (see GetCreateOptions Features). A paused MicroVM is auto-resumed.
// A non-zero ExitCode is not a Go error — the API returns HTTP 200.
func (s *MicroVMsServiceOp) Exec(ctx context.Context, id string, execRequest *MicroVMExecRequest) (*MicroVMExecResult, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}
	if execRequest == nil {
		return nil, nil, NewArgError("execRequest", "cannot be nil")
	}
	if len(execRequest.Argv) == 0 {
		return nil, nil, NewArgError("execRequest.Argv", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/exec", microVMBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, execRequest)
	if err != nil {
		return nil, nil, err
	}

	result := new(MicroVMExecResult)
	resp, err := s.client.Do(ctx, req, result)
	if err != nil {
		return nil, resp, err
	}

	return result, resp, nil
}

// ConsoleURL builds the WebSocket URL for an interactive PTY console on the
// MicroVM (GET /v2/microvms/{id}/console). godo does not dial WebSockets; the
// caller supplies its own client. The bearer token lives on godo's HTTP
// transport and is not attached to requests built here, so the caller must set
// Authorization: Bearer <token> on the WebSocket handshake. Binary frames are
// raw terminal I/O; text frames carry resize (client→server) and error/exit/
// status control (server→client) — see MarshalMicroVMConsoleResize and
// ParseMicroVMConsoleControl.
func (s *MicroVMsServiceOp) ConsoleURL(id string, opt *MicroVMConsoleOptions) (string, error) {
	if id == "" {
		return "", NewArgError("id", "cannot be empty")
	}
	if s.client == nil || s.client.BaseURL == nil {
		return "", NewArgError("client.BaseURL", "cannot be nil")
	}

	u, err := s.client.BaseURL.Parse(fmt.Sprintf("%s/%s/console", microVMBasePath, id))
	if err != nil {
		return "", err
	}

	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}

	if opt != nil {
		q := url.Values{}
		if opt.Rows > 0 {
			q.Set("rows", strconv.FormatUint(uint64(opt.Rows), 10))
		}
		if opt.Cols > 0 {
			q.Set("cols", strconv.FormatUint(uint64(opt.Cols), 10))
		}
		u.RawQuery = q.Encode()
	}

	return u.String(), nil
}

// MarshalMicroVMConsoleResize encodes a client → server text frame that
// resizes the console PTY: {"resize":{"rows":N,"cols":M}}.
func MarshalMicroVMConsoleResize(rows, cols uint32) ([]byte, error) {
	return json.Marshal(struct {
		Resize struct {
			Rows uint32 `json:"rows"`
			Cols uint32 `json:"cols"`
		} `json:"resize"`
	}{Resize: struct {
		Rows uint32 `json:"rows"`
		Cols uint32 `json:"cols"`
	}{Rows: rows, Cols: cols}})
}

// ParseMicroVMConsoleControl decodes a server → client text control frame
// (error, exit, or status). Binary frames are raw PTY stdout and should not
// be passed here. Returns an error if the payload is not valid JSON or does
// not contain any recognized control field.
func ParseMicroVMConsoleControl(data []byte) (*MicroVMConsoleControl, error) {
	ctrl := new(MicroVMConsoleControl)
	if err := json.Unmarshal(data, ctrl); err != nil {
		return nil, err
	}
	if ctrl.Error == nil && ctrl.Exit == nil && ctrl.Status == nil {
		return nil, fmt.Errorf("microvm console: unrecognized control frame")
	}
	return ctrl, nil
}

// UploadFile writes a single file into the MicroVM's workload container
// (PUT /v2/microvms/{id}/files), replacing any existing regular file
// atomically. Requires the file_transfer feature gate (see GetCreateOptions
// Features). A paused MicroVM is auto-resumed. The returned MicroVMFileInfo
// carries the SHA-256 of the content as written; pass uploadRequest.SHA256 to
// have the API verify it before the target is replaced.
func (s *MicroVMsServiceOp) UploadFile(ctx context.Context, id string, uploadRequest *MicroVMFileUploadRequest) (*MicroVMFileInfo, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}
	if uploadRequest == nil {
		return nil, nil, NewArgError("uploadRequest", "cannot be nil")
	}
	if uploadRequest.Path == "" {
		return nil, nil, NewArgError("uploadRequest.Path", "cannot be empty")
	}
	if uploadRequest.Size < 0 {
		return nil, nil, NewArgError("uploadRequest.Size", "cannot be negative")
	}
	if (uploadRequest.UID == nil) != (uploadRequest.GID == nil) {
		return nil, nil, NewArgError("uploadRequest.UID/GID", "must be set together")
	}

	u, err := s.client.BaseURL.Parse(fmt.Sprintf("%s/%s/files", microVMBasePath, id))
	if err != nil {
		return nil, nil, err
	}
	q := url.Values{}
	q.Set("path", uploadRequest.Path)
	if uploadRequest.Mode != "" {
		q.Set("mode", uploadRequest.Mode)
	}
	if uploadRequest.UID != nil {
		q.Set("uid", strconv.FormatUint(uint64(*uploadRequest.UID), 10))
		q.Set("gid", strconv.FormatUint(uint64(*uploadRequest.GID), 10))
	}
	if uploadRequest.SHA256 != "" {
		q.Set("sha256", uploadRequest.SHA256)
	}
	u.RawQuery = q.Encode()

	body := uploadRequest.Body
	if body == nil {
		body = http.NoBody
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), body)
	if err != nil {
		return nil, nil, err
	}
	// The API requires Content-Length and answers 411 to a chunked body.
	// http.NewRequest fills ContentLength for in-memory readers; anything
	// else needs the caller's Size.
	if uploadRequest.Size > 0 {
		req.ContentLength = uploadRequest.Size
	} else if req.ContentLength == 0 && req.Body != http.NoBody {
		return nil, nil, NewArgError("uploadRequest.Size", "required when Body length is not known")
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Accept", mediaType)
	req.Header.Set("User-Agent", s.client.UserAgent)
	for k, v := range s.client.headers {
		req.Header.Add(k, v)
	}

	info := new(MicroVMFileInfo)
	resp, err := s.client.Do(ctx, req, info)
	if err != nil {
		return nil, resp, err
	}
	return info, resp, nil
}

// DownloadFile streams a single regular file out of the MicroVM's workload
// container (GET /v2/microvms/{id}/files). Requires the file_transfer feature
// gate. A paused MicroVM is auto-resumed; files over 64 MiB are refused. On
// success the caller owns Body and must read it to EOF and Close it; Body
// verifies the content against the announced SHA-256 as it is read.
func (s *MicroVMsServiceOp) DownloadFile(ctx context.Context, id, path string) (*MicroVMFileDownload, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}
	if path == "" {
		return nil, nil, NewArgError("path", "cannot be empty")
	}

	q := url.Values{}
	q.Set("path", path)
	reqPath := fmt.Sprintf("%s/%s/files?%s", microVMBasePath, id, q.Encode())

	req, err := s.client.NewRequest(ctx, http.MethodGet, reqPath, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := s.client.DoStream(ctx, req)
	if err != nil {
		return nil, resp, err
	}

	out := &MicroVMFileDownload{
		SizeBytes: resp.ContentLength,
		SHA256:    strings.ToLower(strings.TrimSpace(resp.Header.Get("X-Checksum-Sha256"))),
	}
	out.Body = &microVMFileBody{body: resp.Body, hasher: sha256.New(), want: out.SHA256}
	return out, resp, nil
}

// microVMFileBody hashes a download as it is read and reports a mismatch
// against the announced digest in place of io.EOF.
type microVMFileBody struct {
	body   io.ReadCloser
	hasher hash.Hash
	want   string
}

func (b *microVMFileBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.hasher.Write(p[:n])
	}
	if err == io.EOF && b.want != "" && hex.EncodeToString(b.hasher.Sum(nil)) != b.want {
		return n, ErrMicroVMFileChecksum
	}
	return n, err
}

func (b *microVMFileBody) Close() error {
	return b.body.Close()
}
