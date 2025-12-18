package godo

import (
	"context"
	"fmt"
	"net/http"
)

const nfsBasePath = "v2/nfs"

const nfsSnapshotsBasePath = "v2/nfs/snapshots"

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

// NfsCreateRequest represents a request to create an NFS share.
type NfsCreateRequest struct {
	Name    string   `json:"name"`
	SizeGib int      `json:"size_gib"`
	Region  string   `json:"region"`
	VpcIDs  []string `json:"vpc_ids,omitempty"`
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
	if region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
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
	if region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
	}

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
	if region == "" {
		return nil, NewArgError("region", "cannot be empty")
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
	if region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
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
	if region == "" {
		return nil, nil, NewArgError("region", "cannot be empty")
	}

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
	if region == "" {
		return nil, NewArgError("region", "cannot be empty")
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
