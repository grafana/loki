package godo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// HostedAgentCheckpointStatus is the lifecycle status of a session checkpoint.
type HostedAgentCheckpointStatus string

const (
	HostedAgentCheckpointStatusPending HostedAgentCheckpointStatus = "PENDING"
	HostedAgentCheckpointStatusReady   HostedAgentCheckpointStatus = "READY"
	HostedAgentCheckpointStatusFailed  HostedAgentCheckpointStatus = "FAILED"
)

// HostedAgentCheckpointKind distinguishes user-requested vs automatic checkpoints.
type HostedAgentCheckpointKind string

const (
	HostedAgentCheckpointKindExplicit HostedAgentCheckpointKind = "explicit"
	HostedAgentCheckpointKindImplicit HostedAgentCheckpointKind = "implicit"
)

// HostedAgentCheckpoint is an immutable save point of a session's microVM
// (files + live process memory) plus an event-log cursor.
type HostedAgentCheckpoint struct {
	CheckpointID string                      `json:"checkpoint_id"`
	SessionID    string                      `json:"session_id"`
	Status       HostedAgentCheckpointStatus `json:"status"`
	Kind         HostedAgentCheckpointKind   `json:"kind"`
	Label        string                      `json:"label,omitempty"`
	EventID      string                      `json:"event_id,omitempty"`
	SizeBytes    uint64                      `json:"size_bytes,omitempty"`
	CreatedAt    Timestamp                   `json:"created_at"`
	ErrorMessage string                      `json:"error_message,omitempty"`
}

// HostedAgentCheckpointCreateRequest is the optional body for CreateCheckpoint.
type HostedAgentCheckpointCreateRequest struct {
	Label string `json:"label,omitempty"`
}

// HostedAgentCheckpointListOptions paginates ListCheckpoints (newest-first).
type HostedAgentCheckpointListOptions struct {
	PageToken string `url:"page_token,omitempty"`
	PageSize  int    `url:"page_size,omitempty"`
}

// HostedAgentCheckpointsListResponse is returned by ListCheckpoints.
type HostedAgentCheckpointsListResponse struct {
	Checkpoints   []HostedAgentCheckpoint `json:"checkpoints"`
	NextPageToken string                  `json:"next_page_token"`
}

// HostedAgentCheckpointDeleteResponse is returned by DeleteCheckpoint.
type HostedAgentCheckpointDeleteResponse struct {
	CheckpointID string `json:"checkpoint_id"`
	Deleted      bool   `json:"deleted"`
}

// HostedAgentForkSessionRequest is the body for ForkSession.
// Count defaults to 1 when zero; the server rejects values above HostedAgentForkMaxCount.
type HostedAgentForkSessionRequest struct {
	FromCheckpointID string `json:"from_checkpoint_id,omitempty"`
	Count            int    `json:"count,omitempty"`
}

// HostedAgentForkSessionResponse is returned by ForkSession.
type HostedAgentForkSessionResponse struct {
	Sessions []HostedAgentSession `json:"sessions"`
}

type hostedAgentCheckpointRoot struct {
	Checkpoint *HostedAgentCheckpoint `json:"checkpoint"`
}

// CreateCheckpoint captures a save point for the session. The call blocks until
// the checkpoint reaches a terminal READY state (or the server returns an error).
// Checkpoints are only allowed between turns (at run.completed).
func (s *HostedAgentsServiceOp) CreateCheckpoint(ctx context.Context, sessionID string, create *HostedAgentCheckpointCreateRequest) (*HostedAgentCheckpoint, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if create == nil {
		create = &HostedAgentCheckpointCreateRequest{}
	}
	path := fmt.Sprintf(hostedAgentSessionCheckpointsPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, create)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentCheckpointRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Checkpoint == nil {
		return nil, resp, errors.New("hosted agents: create checkpoint returned no checkpoint")
	}
	return root.Checkpoint, resp, nil
}

// ListCheckpoints returns checkpoints for a session, newest first.
func (s *HostedAgentsServiceOp) ListCheckpoints(ctx context.Context, sessionID string, opt *HostedAgentCheckpointListOptions) (*HostedAgentCheckpointsListResponse, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	path := fmt.Sprintf(hostedAgentSessionCheckpointsPath, sessionID)
	path, err := addOptions(path, opt)
	if err != nil {
		return nil, nil, err
	}
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentCheckpointsListResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetCheckpoint returns a single checkpoint by ID.
func (s *HostedAgentsServiceOp) GetCheckpoint(ctx context.Context, sessionID, checkpointID string) (*HostedAgentCheckpoint, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if checkpointID == "" {
		return nil, nil, errors.New("hosted agents: checkpoint id is required")
	}
	path := fmt.Sprintf(hostedAgentSessionCheckpointByIDPath, sessionID, checkpointID)
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentCheckpointRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Checkpoint == nil {
		return nil, resp, errors.New("hosted agents: get checkpoint returned no checkpoint")
	}
	return root.Checkpoint, resp, nil
}

// DeleteCheckpoint removes a checkpoint (DB row + substrate capture). Idempotent:
// deleting an already-gone checkpoint still returns success.
func (s *HostedAgentsServiceOp) DeleteCheckpoint(ctx context.Context, sessionID, checkpointID string) (*HostedAgentCheckpointDeleteResponse, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if checkpointID == "" {
		return nil, nil, errors.New("hosted agents: checkpoint id is required")
	}
	path := fmt.Sprintf(hostedAgentSessionCheckpointByIDPath, sessionID, checkpointID)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentCheckpointDeleteResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ForkSession creates N independent child sessions from a checkpoint (or from
// "now" when FromCheckpointID is empty). Count defaults to 1; max is HostedAgentForkMaxCount.
// The operation is all-or-nothing: on failure nothing is left running.
func (s *HostedAgentsServiceOp) ForkSession(ctx context.Context, sessionID string, fork *HostedAgentForkSessionRequest) (*HostedAgentForkSessionResponse, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if fork == nil {
		fork = &HostedAgentForkSessionRequest{}
	}
	if fork.Count < 0 || fork.Count > HostedAgentForkMaxCount {
		return nil, nil, fmt.Errorf("hosted agents: fork count must be between 1 and %d (got %d)", HostedAgentForkMaxCount, fork.Count)
	}
	path := fmt.Sprintf(hostedAgentSessionForkPath, sessionID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, fork)
	if err != nil {
		return nil, nil, err
	}
	root := new(HostedAgentForkSessionResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// RollbackToCheckpoint rewinds the session in place to the given checkpoint.
// The session_id is unchanged; the underlying sandbox is replaced. Returns the
// updated session.
func (s *HostedAgentsServiceOp) RollbackToCheckpoint(ctx context.Context, sessionID, checkpointID string) (*HostedAgentSession, *Response, error) {
	if sessionID == "" {
		return nil, nil, errors.New("hosted agents: session id is required")
	}
	if checkpointID == "" {
		return nil, nil, errors.New("hosted agents: checkpoint id is required")
	}
	path := fmt.Sprintf(hostedAgentSessionCheckpointRollbackPath, sessionID, checkpointID)
	req, err := s.client.NewRequest(ctx, http.MethodPost, path, struct{}{})
	if err != nil {
		return nil, nil, err
	}
	root := new(hostedAgentSessionRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	if root.Session == nil {
		return nil, resp, errors.New("hosted agents: rollback returned no session")
	}
	return root.Session, resp, nil
}
