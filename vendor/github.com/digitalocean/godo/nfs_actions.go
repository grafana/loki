package godo

import (
	"context"
	"fmt"
	"net/http"
)

// NfsActionsService is an interface for interacting with the NFS actions
// endpoints of the DigitalOcean API
// See: https://docs.digitalocean.com/reference/api/api-reference/#tag/NFS-Actions
type NfsActionsService interface {
	Resize(ctx context.Context, nfsShareId string, size uint64, region string) (*NfsAction, *Response, error)
	Snapshot(ctx context.Context, nfsShareId string, nfsSnapshotName string, region string) (*NfsAction, *Response, error)
	Attach(ctx context.Context, nfsShareId string, vpcID string, region string) (*NfsAction, *Response, error)
	Detach(ctx context.Context, nfsShareId string, vpcID string, region string) (*NfsAction, *Response, error)
}

// NfsActionsServiceOp handles communication with the NFS action related
// methods of the DigitalOcean API.
type NfsActionsServiceOp struct {
	client *Client
}

var _ NfsActionsService = &NfsActionsServiceOp{}

// NfsAction represents an NFS action
type NfsAction struct {
	ID           int        `json:"id"`
	Status       string     `json:"status"`
	Type         string     `json:"type"`
	StartedAt    *Timestamp `json:"started_at"`
	CompletedAt  *Timestamp `json:"completed_at"`
	ResourceID   string     `json:"resource_id"`
	ResourceType string     `json:"resource_type"`
	Region       *Region    `json:"region,omitempty"`
	RegionSlug   string     `json:"region_slug,omitempty"`
}

// nfsActionRoot represents the response wrapper for NFS actions
type nfsActionRoot struct {
	Event *NfsAction `json:"action"`
}

// NfsActionRequest represents a generic NFS action request
type NfsActionRequest struct {
	Type   string      `json:"type"`
	Region string      `json:"region"`
	Params interface{} `json:"params"`
}

// NfsResizeParams represents parameters for resizing an NFS share
type NfsResizeParams struct {
	SizeGib uint64 `json:"size_gib"`
}

// NfsSnapshotParams represents parameters for creating an NFS snapshot
type NfsSnapshotParams struct {
	Name string `json:"name"`
}

// NfsAttachParams represents parameters for attaching an NFS share to a VPC
type NfsAttachParams struct {
	VpcID string `json:"vpc_id"`
}

// NfsDetachParams represents parameters for detaching an NFS share from a VPC
type NfsDetachParams struct {
	VpcID string `json:"vpc_id"`
}

// Resize an NFS share
func (s *NfsActionsServiceOp) Resize(ctx context.Context, nfsShareId string, size uint64, region string) (*NfsAction, *Response, error) {
	request := &NfsActionRequest{
		Type: "resize",
		Params: &NfsResizeParams{
			SizeGib: size,
		},
	}

	return s.doAction(ctx, nfsShareId, request)
}

// Snapshot an NFS share
func (s *NfsActionsServiceOp) Snapshot(ctx context.Context, nfsShareId, nfsSnapshotName, region string) (*NfsAction, *Response, error) {
	request := &NfsActionRequest{
		Type: "snapshot",
		Params: &NfsSnapshotParams{
			Name: nfsSnapshotName,
		},
	}

	return s.doAction(ctx, nfsShareId, request)
}

// Attach an NFS share
func (s *NfsActionsServiceOp) Attach(ctx context.Context, nfsShareId, vpcID, region string) (*NfsAction, *Response, error) {
	request := &NfsActionRequest{
		Type: "attach",
		Params: &NfsAttachParams{
			VpcID: vpcID,
		},
	}

	return s.doAction(ctx, nfsShareId, request)
}

// Detach an NFS share
func (s *NfsActionsServiceOp) Detach(ctx context.Context, nfsShareId, vpcID, region string) (*NfsAction, *Response, error) {
	request := &NfsActionRequest{
		Type: "detach",
		Params: &NfsAttachParams{
			VpcID: vpcID,
		},
	}

	return s.doAction(ctx, nfsShareId, request)
}

func (s *NfsActionsServiceOp) doAction(ctx context.Context, nfsShareId string, request *NfsActionRequest) (*NfsAction, *Response, error) {
	if request == nil {
		return nil, nil, NewArgError("request", "request can't be nil")
	}

	path := nfsActionPath(nfsShareId)

	req, err := s.client.NewRequest(ctx, http.MethodPost, path, request)
	if err != nil {
		return nil, nil, err
	}

	root := new(nfsActionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Event, resp, err
}

func nfsActionPath(nfsID string) string {
	return fmt.Sprintf("v2/nfs/%v/actions", nfsID)
}
