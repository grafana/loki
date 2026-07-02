package godo

import (
	"context"
	"fmt"
	"net/http"
)

const nfsBasePath = "v2/nfs"

const nfsSnapshotsBasePath = "v2/nfs/snapshots"

const nfsSharesBasePath = "v2/nfs/shares"

const nfsAccessPointsBasePath = "v2/nfs/access_points"

// NfsShareStatus represents the status of an NFS share.
type NfsShareStatus string

// Possible states for an NFS share.
const (
	NfsShareCreating = NfsShareStatus("CREATING")
	NfsShareActive   = NfsShareStatus("ACTIVE")
	NfsShareFailed   = NfsShareStatus("FAILED")
	NfsShareDeleted  = NfsShareStatus("DELETED")
)

// NfsSnapshotStatus represents the status of an NFS snapshot.
type NfsSnapshotStatus string

// Possible states for an NFS snapshot.
const (
	NfsSnapshotUnknown  = NfsSnapshotStatus("UNKNOWN")
	NfsSnapshotCreating = NfsSnapshotStatus("CREATING")
	NfsSnapshotActive   = NfsSnapshotStatus("ACTIVE")
	NfsSnapshotFailed   = NfsSnapshotStatus("FAILED")
	NfsSnapshotDeleted  = NfsSnapshotStatus("DELETED")
)

// NfsAccessPointStatus represents the status of an NFS access point.
type NfsAccessPointStatus string

// Possible states for an NFS access point.
const (
	NfsAccessPointCreating = NfsAccessPointStatus("ACCESS_POINT_CREATING")
	NfsAccessPointActive   = NfsAccessPointStatus("ACCESS_POINT_ACTIVE")
	NfsAccessPointFailed   = NfsAccessPointStatus("ACCESS_POINT_FAILED")
	NfsAccessPointDeleted  = NfsAccessPointStatus("ACCESS_POINT_DELETED")
)

// NfsAccessPolicyProtocol represents an allowed protocol in an NFS access policy.
type NfsAccessPolicyProtocol string

// Possible protocol values for an NFS access policy.
const (
	NfsAccessPolicyProtocolNFS  = NfsAccessPolicyProtocol("NFS")
	NfsAccessPolicyProtocolNFS4 = NfsAccessPolicyProtocol("NFS4")
)

// NfsSquashConfig represents the squash mode in an NFS access policy.
type NfsSquashConfig string

// Possible squash config values for an NFS access policy.
const (
	NfsSquashConfigNoSquash   = NfsSquashConfig("NO_SQUASH")
	NfsSquashConfigRootSquash = NfsSquashConfig("ROOT_SQUASH")
	NfsSquashConfigAllSquash  = NfsSquashConfig("ALL_SQUASH")
)

type NfsService interface {
	// List retrieves a list of NFS shares filtered by region
	List(ctx context.Context, opts *ListOptions, region string) ([]*Nfs, *Response, error)
	// Create creates a new NFS share with the provided configuration
	Create(ctx context.Context, nfsCreateRequest *NfsCreateRequest) (*Nfs, *Response, error)
	// Delete removes an NFS share by its ID and region
	Delete(ctx context.Context, nfsShareId string, region string) (*Response, error)
	// Get retrieves a specific NFS share by its ID and region
	Get(ctx context.Context, nfsShareId string, region string) (*Nfs, *Response, error)
	// List retrieves a list of NFS snapshots filtered by an optional share ID and region
	ListSnapshots(ctx context.Context, opts *ListOptions, nfsShareId string, region string) ([]*NfsSnapshot, *Response, error)
	// Get retrieves a specific NFS snapshot by its ID and region
	GetSnapshot(ctx context.Context, nfsSnapshotID string, region string) (*NfsSnapshot, *Response, error)
	// Delete removes an NFS snapshot by its ID and region
	DeleteSnapshot(ctx context.Context, nfsSnapshotID string, region string) (*Response, error)
	// CreateAccessPoint creates a new access point for a share.
	CreateAccessPoint(ctx context.Context, shareID string, createRequest *NfsCreateAccessPointRequest) (*NfsAccessPointActionResponse, *Response, error)
	// GetAccessPoint retrieves an NFS access point by ID.
	GetAccessPoint(ctx context.Context, accessPointID string) (*NfsAccessPoint, *Response, error)
	// ListAccessPoints retrieves access points for a given share.
	ListAccessPoints(ctx context.Context, shareID string, opts *NfsListAccessPointsOptions) ([]*NfsAccessPoint, *Response, error)
	// DeleteAccessPoint soft-deletes an NFS access point by ID.
	DeleteAccessPoint(ctx context.Context, accessPointID string) (*NfsAccessPointActionResponse, *Response, error)
}

// NfsServiceOp handles communication with the NFS related methods of the
// DigitalOcean API.
type NfsServiceOp struct {
	client *Client
}

var _ NfsService = &NfsServiceOp{}

// Nfs represents a DigitalOcean NFS share
type Nfs struct {
	// ID is the unique identifier for the NFS share
	ID string `json:"id"`
	// Name is the human-readable name for the NFS share
	Name string `json:"name"`
	// SizeGib is the size of the NFS share in gibibytes
	SizeGib int `json:"size_gib"`
	// Region is the datacenter region where the NFS share is located
	Region string `json:"region"`
	// Status represents the current state of the NFS share
	Status NfsShareStatus `json:"status"`
	// CreatedAt is the timestamp when the NFS share was created
	CreatedAt string `json:"created_at"`
	// VpcIDs is a list of VPC IDs that have access to the NFS share
	VpcIDs []string `json:"vpc_ids"`
	// Host is the IP address of the NFS server accessible from the associated VPC
	Host string `json:"host"`
	// MountPath is the path at which the share will be available
	MountPath string `json:"mount_path"`
	//PerformanceTier is the performance tier of the NFS share
	PerformanceTier string `json:"performance_tier"`
	// AccessPoints is the list of access points configured for the NFS share.
	AccessPoints []*NfsAccessPoint `json:"access_points,omitempty"`
}

type NfsSnapshot struct {
	// ID is the unique identifier for the NFS snapshot
	ID string `json:"id"`
	// Name is the human-readable name for the NFS snapshot
	Name string `json:"name"`
	// SizeGib is the size of the NFS snapshot in gibibytes
	SizeGib int `json:"size_gib"`
	// Region is the datacenter region where the NFS snapshot is located
	Region string `json:"region"`
	// Status represents the current status of the NFS snapshot
	Status NfsSnapshotStatus `json:"status"`
	// CreatedAt is the timestamp when the NFS snapshot was created
	CreatedAt string `json:"created_at"`
	// ShareID is the unique identifier of the share from which this snapshot was created.
	ShareID string `json:"share_id"`
}

// NfsAccessPolicy represents the export policy of an NFS access point.
type NfsAccessPolicy struct {
	Anonuid                    uint64                    `json:"anonuid"`
	Anongid                    uint64                    `json:"anongid"`
	Protocols                  []NfsAccessPolicyProtocol `json:"protocols"`
	SquashConfig               NfsSquashConfig           `json:"squash_config"`
	IdentityEnforcementEnabled bool                      `json:"identity_enforcement_enabled"`
}

// NfsAccessPoint represents an NFS access point.
type NfsAccessPoint struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	ShareID      string               `json:"share_id"`
	Path         string               `json:"path"`
	Status       NfsAccessPointStatus `json:"status"`
	AccessPolicy NfsAccessPolicy      `json:"access_policy"`
	CreatedAt    string               `json:"created_at"`
	UpdatedAt    string               `json:"updated_at"`
	IsDefault    bool                 `json:"is_default"`
	VpcID        *string              `json:"vpc_id,omitempty"`
}

// NfsCreateRequest represents a request to create an NFS share.
type NfsCreateRequest struct {
	Name            string   `json:"name"`
	SizeGib         int      `json:"size_gib"`
	Region          string   `json:"region"`
	VpcIDs          []string `json:"vpc_ids,omitempty"`
	PerformanceTier string   `json:"performance_tier,omitempty"`
}

// NfsCreateAccessPointRequest represents a request to create an NFS access point.
type NfsCreateAccessPointRequest struct {
	Name         string          `json:"name"`
	Path         string          `json:"path"`
	AccessPolicy NfsAccessPolicy `json:"access_policy"`
	VpcID        string          `json:"vpc_id"`
}

// NfsListAccessPointsOptions represents filters for listing access points.
type NfsListAccessPointsOptions struct {
	Status NfsAccessPointStatus `url:"status,omitempty"`
}

// NfsAccessPointActionResponse represents an access point mutation response.
type NfsAccessPointActionResponse struct {
	AccessPoint *NfsAccessPoint `json:"access_point"`
	Action      *NfsAction      `json:"action"`
}

// nfsRoot represents a response from the DigitalOcean API
type nfsRoot struct {
	Share *Nfs `json:"share"`
}

// nfsListRoot represents a response from the DigitalOcean API
type nfsListRoot struct {
	Shares []*Nfs `json:"shares,omitempty"`
	Links  *Links `json:"links,omitempty"`
	Meta   *Meta  `json:"meta"`
}

// nfsSnapshotRoot represents a response from the DigitalOcean API
type nfsSnapshotRoot struct {
	Snapshot *NfsSnapshot `json:"snapshot"`
}

// nfsSnapshotListRoot represents a response from the DigitalOcean API
type nfsSnapshotListRoot struct {
	Snapshots []*NfsSnapshot `json:"snapshots,omitempty"`
	Links     *Links         `json:"links,omitempty"`
	Meta      *Meta          `json:"meta"`
}

// nfsAccessPointRoot represents a response from the DigitalOcean API.
type nfsAccessPointRoot struct {
	AccessPoint *NfsAccessPoint `json:"access_point"`
}

// nfsAccessPointListRoot represents a response from the DigitalOcean API.
type nfsAccessPointListRoot struct {
	AccessPoints []*NfsAccessPoint `json:"access_points,omitempty"`
}

// nfsAccessPointActionRoot represents a response from access point mutation APIs.
type nfsAccessPointActionRoot struct {
	AccessPoint *NfsAccessPoint `json:"access_point"`
	Action      *NfsAction      `json:"action"`
}

// nfsOptions represents the query param options for NFS operations
type nfsOptions struct {
	// Region is the datacenter region where the NFS share/shapshot is located
	Region string `url:"region"`
	// ShareID is the unique identifier of the share from which this snapshot was created.
	ShareID string `url:"share_id,omitempty"`
}

// Create creates a new NFS share.
func (s *NfsServiceOp) Create(ctx context.Context, createRequest *NfsCreateRequest) (*Nfs, *Response, error) {
	if createRequest == nil {
		return nil, nil, NewArgError("createRequest", "cannot be nil")
	}

	if createRequest.SizeGib < 50 {
		return nil, nil, NewArgError("size_gib", "it cannot be less than 50Gib")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, nfsBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Share, resp, nil
}

// Get retrieves an NFS share by ID and region.
func (s *NfsServiceOp) Get(ctx context.Context, nfsShareId string, region string) (*Nfs, *Response, error) {
	if nfsShareId == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", nfsBasePath, nfsShareId)

	getOpts := &nfsOptions{Region: region}
	path, err := addOptions(path, getOpts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Share, resp, nil
}

// List returns a list of NFS shares.
func (s *NfsServiceOp) List(ctx context.Context, opts *ListOptions, region string) ([]*Nfs, *Response, error) {
	path, err := addOptions(nfsBasePath, opts)
	if err != nil {
		return nil, nil, err
	}

	listOpts := &nfsOptions{Region: region}
	path, err = addOptions(path, listOpts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsListRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	if root.Links != nil {
		resp.Links = root.Links
	}
	if root.Meta != nil {
		resp.Meta = root.Meta
	}

	return root.Shares, resp, nil
}

// Delete deletes an NFS share by ID and region.
func (s *NfsServiceOp) Delete(ctx context.Context, nfsShareId string, region string) (*Response, error) {
	if nfsShareId == "" {
		return nil, NewArgError("id", "cannot be empty")
	}
	path := fmt.Sprintf("%s/%s", nfsBasePath, nfsShareId)

	deleteOpts := &nfsOptions{Region: region}
	path, err := addOptions(path, deleteOpts)
	if err != nil {
		return nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(ctx, req, nil)
	if err != nil {
		return resp, err
	}

	return resp, nil
}

// Get retrieves an NFS snapshot by ID and region.
func (s *NfsServiceOp) GetSnapshot(ctx context.Context, nfsSnapshotID string, region string) (*NfsSnapshot, *Response, error) {
	if nfsSnapshotID == "" {
		return nil, nil, NewArgError("snapshotID", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", nfsSnapshotsBasePath, nfsSnapshotID)

	getOpts := &nfsOptions{Region: region}
	path, err := addOptions(path, getOpts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsSnapshotRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Snapshot, resp, nil
}

// List returns a list of NFS snapshots.
func (s *NfsServiceOp) ListSnapshots(ctx context.Context, opts *ListOptions, nfsShareId, region string) ([]*NfsSnapshot, *Response, error) {

	path, err := addOptions(nfsSnapshotsBasePath, opts)
	if err != nil {
		return nil, nil, err
	}

	listOpts := &nfsOptions{Region: region, ShareID: nfsShareId}
	path, err = addOptions(path, listOpts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsSnapshotListRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	if root.Links != nil {
		resp.Links = root.Links
	}
	if root.Meta != nil {
		resp.Meta = root.Meta
	}

	return root.Snapshots, resp, nil
}

// Delete deletes an NFS snapshot by ID and region.
func (s *NfsServiceOp) DeleteSnapshot(ctx context.Context, nfsSnapshotID string, region string) (*Response, error) {
	if nfsSnapshotID == "" {
		return nil, NewArgError("snapshotID", "cannot be empty")
	}
	path := fmt.Sprintf("%s/%s", nfsSnapshotsBasePath, nfsSnapshotID)

	deleteOpts := &nfsOptions{Region: region}
	path, err := addOptions(path, deleteOpts)
	if err != nil {
		return nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(ctx, req, nil)
	if err != nil {
		return resp, err
	}

	return resp, nil
}

// CreateAccessPoint creates a new access point for a share.
func (s *NfsServiceOp) CreateAccessPoint(ctx context.Context, shareID string, createRequest *NfsCreateAccessPointRequest) (*NfsAccessPointActionResponse, *Response, error) {
	if shareID == "" {
		return nil, nil, NewArgError("shareID", "cannot be empty")
	}
	if createRequest == nil {
		return nil, nil, NewArgError("createRequest", "cannot be nil")
	}
	if createRequest.VpcID == "" {
		return nil, nil, NewArgError("vpc_id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/access_points", nfsSharesBasePath, shareID)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsAccessPointActionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return &NfsAccessPointActionResponse{AccessPoint: root.AccessPoint, Action: root.Action}, resp, nil
}

// GetAccessPoint retrieves an NFS access point by ID.
func (s *NfsServiceOp) GetAccessPoint(ctx context.Context, accessPointID string) (*NfsAccessPoint, *Response, error) {
	if accessPointID == "" {
		return nil, nil, NewArgError("accessPointID", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", nfsAccessPointsBasePath, accessPointID)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsAccessPointRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AccessPoint, resp, nil
}

// ListAccessPoints returns all access points for a share.
func (s *NfsServiceOp) ListAccessPoints(ctx context.Context, shareID string, opts *NfsListAccessPointsOptions) ([]*NfsAccessPoint, *Response, error) {
	if shareID == "" {
		return nil, nil, NewArgError("shareID", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s/access_points", nfsSharesBasePath, shareID)
	path, err := addOptions(path, opts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsAccessPointListRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AccessPoints, resp, nil
}

// DeleteAccessPoint soft-deletes an NFS access point by ID.
func (s *NfsServiceOp) DeleteAccessPoint(ctx context.Context, accessPointID string) (*NfsAccessPointActionResponse, *Response, error) {
	if accessPointID == "" {
		return nil, nil, NewArgError("accessPointID", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", nfsAccessPointsBasePath, accessPointID)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsAccessPointActionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return &NfsAccessPointActionResponse{AccessPoint: root.AccessPoint, Action: root.Action}, resp, nil
}
