package godo

import (
	"context"
	"fmt"
	"net/http"
)

const microDropletBasePath = "v2/microdroplets/instances"

// MicroDropletState represents the lifecycle state of a MicroDroplet.
type MicroDropletState string

// Possible lifecycle states for a MicroDroplet.
const (
	MicroDropletStateUnknown     = MicroDropletState("unknown")
	MicroDropletStateCreating    = MicroDropletState("creating")
	MicroDropletStateRunning     = MicroDropletState("running")
	MicroDropletStatePausing     = MicroDropletState("pausing")
	MicroDropletStatePaused      = MicroDropletState("paused")
	MicroDropletStateResuming    = MicroDropletState("resuming")
	MicroDropletStateTerminating = MicroDropletState("terminating")
	MicroDropletStateTerminated  = MicroDropletState("terminated")
	MicroDropletStateFailed      = MicroDropletState("failed")
)

// MicroDropletNetworking represents the networking mode of a MicroDroplet.
type MicroDropletNetworking string

// Possible networking modes for a MicroDroplet.
const (
	MicroDropletNetworkingUnknown = MicroDropletNetworking("unknown")
	MicroDropletNetworkingPublic  = MicroDropletNetworking("public")
	MicroDropletNetworkingVPC     = MicroDropletNetworking("vpc")
)

// MicroDropletHTTPProtocol represents the HTTP protocol option for a MicroDroplet.
type MicroDropletHTTPProtocol string

// Possible HTTP protocol values for a MicroDroplet.
const (
	MicroDropletHTTPProtocolHTTP  = MicroDropletHTTPProtocol("http")
	MicroDropletHTTPProtocolHTTPS = MicroDropletHTTPProtocol("https")
	MicroDropletHTTPProtocolHTTP2 = MicroDropletHTTPProtocol("http2")
)

// MicroDropletCheckpointStatus represents the status of a MicroDroplet checkpoint.
type MicroDropletCheckpointStatus string

// Possible states for a MicroDroplet checkpoint.
const (
	MicroDropletCheckpointStatusUnknown   = MicroDropletCheckpointStatus("CHECKPOINT_UNKNOWN")
	MicroDropletCheckpointStatusCreating  = MicroDropletCheckpointStatus("CHECKPOINT_CREATING")
	MicroDropletCheckpointStatusAvailable = MicroDropletCheckpointStatus("CHECKPOINT_AVAILABLE")
	MicroDropletCheckpointStatusFailed    = MicroDropletCheckpointStatus("CHECKPOINT_FAILED")
	MicroDropletCheckpointStatusDeleted   = MicroDropletCheckpointStatus("CHECKPOINT_DELETED")
)

// MicroDropletsService is an interface for interfacing with the MicroDroplet
// endpoints of the DigitalOcean API.
// See: https://docs.digitalocean.com/reference/api/api-reference/#tag/MicroDroplets
type MicroDropletsService interface {
	List(ctx context.Context, opt *ListOptions) ([]MicroDroplet, *Response, error)
	ListByRegion(ctx context.Context, region string, opt *ListOptions) ([]MicroDroplet, *Response, error)
	ListByName(ctx context.Context, name string, opt *ListOptions) ([]MicroDroplet, *Response, error)
	Get(ctx context.Context, id string) (*MicroDroplet, *Response, error)
	Create(ctx context.Context, createRequest *MicroDropletCreateRequest) (*MicroDroplet, *Response, error)
	Pause(ctx context.Context, id string) (*MicroDroplet, *Response, error)
	Resume(ctx context.Context, id string) (*MicroDroplet, *Response, error)
	Delete(ctx context.Context, id string) (*Response, error)
	ListCheckpoints(ctx context.Context, id string, opt *ListOptions) ([]MicroDropletCheckpoint, *Response, error)
}

// MicroDropletsServiceOp handles communication with the MicroDroplet related
// methods of the DigitalOcean API.
type MicroDropletsServiceOp struct {
	client *Client
}

var _ MicroDropletsService = &MicroDropletsServiceOp{}

// MicroDroplet represents a DigitalOcean MicroDroplet.
type MicroDroplet struct {
	ID         string                 `json:"id,omitempty"`
	Name       string                 `json:"name,omitempty"`
	Region     string                 `json:"region,omitempty"`
	State      MicroDropletState      `json:"state,omitempty"`
	Size       string                 `json:"size,omitempty"`
	Networking MicroDropletNetworking `json:"networking,omitempty"`
	Image      string                 `json:"image,omitempty"`
	Endpoint   string                 `json:"endpoint,omitempty"`
	AutoPause  *AutoPauseConfig       `json:"auto_pause,omitempty"`
	AutoResume *bool                  `json:"auto_resume,omitempty"`
	Created    string                 `json:"created_at,omitempty"`
}

// AutoPauseConfig configures MicroDroplet auto-pause behavior. IdleTimeout is
// a Go duration string (e.g. "5m", "30s") describing how long the MicroDroplet
// must be idle before it is paused.
type AutoPauseConfig struct {
	Enabled     *bool  `json:"enabled,omitempty"`
	IdleTimeout string `json:"idle_timeout,omitempty"`
}

// MicroDropletCheckpoint represents a checkpoint of a MicroDroplet
// (persisted memory + disk state), captured automatically when the
// MicroDroplet is paused.
type MicroDropletCheckpoint struct {
	ID             string                       `json:"id,omitempty"`
	MicroDropletID string                       `json:"micro_droplet_id,omitempty"`
	Status         MicroDropletCheckpointStatus `json:"status,omitempty"`
	Name           string                       `json:"name,omitempty"`
	MemoryBytes    uint64                       `json:"memory_bytes,omitempty"`
	DiskBytes      uint64                       `json:"disk_bytes,omitempty"`
	Created        string                       `json:"created_at,omitempty"`
}

// MicroDropletCreateRequest represents a request to create a MicroDroplet.
type MicroDropletCreateRequest struct {
	Name         string                   `json:"name"`
	Region       string                   `json:"region"`
	Size         string                   `json:"size"`
	Image        string                   `json:"image"`
	Networking   MicroDropletNetworking   `json:"networking,omitempty"`
	VPCUUID      string                   `json:"vpc_uuid,omitempty"`
	AutoPause    *AutoPauseConfig         `json:"auto_pause,omitempty"`
	AutoResume   *bool                    `json:"auto_resume,omitempty"`
	HTTPPort     uint32                   `json:"http_port,omitempty"`
	HTTPProtocol MicroDropletHTTPProtocol `json:"http_protocol,omitempty"`
	Environment  map[string]string        `json:"environment,omitempty"`
	Tags         []string                 `json:"tags,omitempty"`
}

// String returns a human-readable description of a MicroDroplet.
func (m MicroDroplet) String() string {
	return Stringify(m)
}

// URN returns the MicroDroplet ID in a valid DO API URN form.
func (m MicroDroplet) URN() string {
	return ToURN("MicroDroplet", m.ID)
}

// String returns a human-readable description of a MicroDropletCheckpoint.
func (c MicroDropletCheckpoint) String() string {
	return Stringify(c)
}

// String returns a human-readable description of a MicroDropletCreateRequest.
func (r MicroDropletCreateRequest) String() string {
	return Stringify(r)
}

type microDropletRoot struct {
	MicroDroplet *MicroDroplet `json:"micro_droplet"`
}

type microDropletsRoot struct {
	MicroDroplets []MicroDroplet `json:"micro_droplets"`
	Links         *Links         `json:"links"`
	Meta          *Meta          `json:"meta"`
}

type microDropletCheckpointsRoot struct {
	Checkpoints []MicroDropletCheckpoint `json:"checkpoints"`
	Links       *Links                   `json:"links"`
	Meta        *Meta                    `json:"meta"`
}

// listMicroDropletOptions holds MicroDroplet-specific list filters that are
// not part of the shared ListOptions.
type listMicroDropletOptions struct {
	Region string `url:"region,omitempty"`
	Name   string `url:"name,omitempty"`
}

// List lists all MicroDroplets, with optional pagination.
func (s *MicroDropletsServiceOp) List(ctx context.Context, opt *ListOptions) ([]MicroDroplet, *Response, error) {
	return s.list(ctx, opt, nil)
}

// ListByRegion lists MicroDroplets filtered by region slug, with optional pagination.
func (s *MicroDropletsServiceOp) ListByRegion(ctx context.Context, region string, opt *ListOptions) ([]MicroDroplet, *Response, error) {
	if region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
	}
	return s.list(ctx, opt, &listMicroDropletOptions{Region: region})
}

// ListByName lists MicroDroplets filtered by exact name match, with optional pagination.
func (s *MicroDropletsServiceOp) ListByName(ctx context.Context, name string, opt *ListOptions) ([]MicroDroplet, *Response, error) {
	if name == "" {
		return nil, nil, NewArgError("name", "cannot be empty")
	}
	return s.list(ctx, opt, &listMicroDropletOptions{Name: name})
}

func (s *MicroDropletsServiceOp) list(ctx context.Context, opt *ListOptions, listOpt *listMicroDropletOptions) ([]MicroDroplet, *Response, error) {
	path := microDropletBasePath
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	path, err = addOptions(path, listOpt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microDropletsRoot)
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

	return root.MicroDroplets, resp, nil
}

// Get retrieves a MicroDroplet by its ID.
func (s *MicroDropletsServiceOp) Get(ctx context.Context, id string) (*MicroDroplet, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", microDropletBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microDropletRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.MicroDroplet, resp, nil
}

// Create provisions a new MicroDroplet with the provided configuration.
func (s *MicroDropletsServiceOp) Create(ctx context.Context, createRequest *MicroDropletCreateRequest) (*MicroDroplet, *Response, error) {
	if createRequest == nil {
		return nil, nil, NewArgError("createRequest", "cannot be nil")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, microDropletBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(microDropletRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.MicroDroplet, resp, nil
}

// Pause synchronously transitions a RUNNING MicroDroplet to PAUSED. It blocks
// until the platform has durably paused the MicroDroplet and returns the
// updated resource. The call is idempotent: pausing a MicroDroplet that is
// already PAUSED returns the current MicroDroplet with no side effects.
func (s *MicroDropletsServiceOp) Pause(ctx context.Context, id string) (*MicroDroplet, *Response, error) {
	return s.doTransition(ctx, id, "pause")
}

// Resume synchronously transitions a PAUSED MicroDroplet to RUNNING. It blocks
// until the platform has durably resumed the MicroDroplet and returns the
// updated resource. The call is idempotent: resuming a MicroDroplet that is
// already RUNNING returns the current MicroDroplet with no side effects.
func (s *MicroDropletsServiceOp) Resume(ctx context.Context, id string) (*MicroDroplet, *Response, error) {
	return s.doTransition(ctx, id, "resume")
}

// doTransition posts an empty body to a MicroDroplet transition sub-resource
// (e.g. /pause, /resume) and decodes the returned MicroDroplet.
func (s *MicroDropletsServiceOp) doTransition(ctx context.Context, id, action string) (*MicroDroplet, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/%s", microDropletBasePath, id, action)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microDropletRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.MicroDroplet, resp, nil
}

// Delete removes a MicroDroplet by its ID. The DigitalOcean API returns a 204
// on success and does not include a response body.
func (s *MicroDropletsServiceOp) Delete(ctx context.Context, id string) (*Response, error) {
	if id == "" {
		return nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", microDropletBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}

// ListCheckpoints lists checkpoints that belong to a MicroDroplet.
// Checkpoints are captured automatically by DigitalOcean when a MicroDroplet
// is paused; each one preserves the memory and disk state required to resume.
func (s *MicroDropletsServiceOp) ListCheckpoints(ctx context.Context, id string, opt *ListOptions) ([]MicroDropletCheckpoint, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/checkpoints", microDropletBasePath, id)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microDropletCheckpointsRoot)
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
