// Copyright 2019 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	raw "google.golang.org/api/storage/v1"
)

// HMACState is the state of the HMAC key.
type HMACState string

const (
	// Active is the status for an active key that can be used to sign
	// requests.
	Active HMACState = "ACTIVE"

	// Inactive is the status for an inactive key thus requests signed by
	// this key will be denied.
	Inactive HMACState = "INACTIVE"

	// Deleted is the status for a key that is deleted.
	// Once in this state the key cannot key cannot be recovered
	// and does not count towards key limits. Deleted keys will be cleaned
	// up later.
	Deleted HMACState = "DELETED"
)

// HMACKey is the representation of a Google Cloud Storage HMAC key.
//
// HMAC keys are used to authenticate signed access to objects. To enable HMAC key
// authentication, please visit https://cloud.google.com/storage/docs/migrating.
//
// This type is experimental and subject to change.
type HMACKey struct {
	// The HMAC's secret key.
	Secret string

	// AccessID is the ID of the HMAC key.
	AccessID string

	// Etag is the HTTP/1.1 Entity tag.
	Etag string

	// ID is the ID of the HMAC key, including the ProjectID and AccessID.
	ID string

	// ProjectID is the ID of the project that owns the
	// service account to which the key authenticates.
	ProjectID string

	// ServiceAccountEmail is the email address
	// of the key's associated service account.
	ServiceAccountEmail string

	// CreatedTime is the creation time of the HMAC key.
	CreatedTime time.Time

	// UpdatedTime is the last modification time of the HMAC key metadata.
	UpdatedTime time.Time

	// State is the state of the HMAC key.
	// It can be one of StateActive, StateInactive or StateDeleted.
	State HMACState
}

// HMACKeyHandle helps provide access and management for HMAC keys.
//
// This type is experimental and subject to change.
type HMACKeyHandle struct {
	projectID string
	accessID  string

	raw *raw.ProjectsHmacKeysService
}

// HMACKeyHandle creates a handle that will be used for HMACKey operations.
func (c *Client) HMACKeyHandle(projectID, accessID string) *HMACKeyHandle {
	return &HMACKeyHandle{
		projectID: projectID,
		accessID:  accessID,
		raw:       raw.NewProjectsHmacKeysService(c.raw),
	}
}

// Get invokes an RPC to retrieve the HMAC key referenced by the
// HMACKeyHandle's accessID.
func (hkh *HMACKeyHandle) Get(ctx context.Context) (*HMACKey, error) {
	call := hkh.raw.Get(hkh.projectID, hkh.accessID)
	setClientHeader(call.Header())

	var metadata *raw.HmacKeyMetadata
	var err error
	err = runWithRetry(ctx, func() error {
		metadata, err = call.Context(ctx).Do()
		return err
	})
	if err != nil {
		return nil, err
	}

	hkPb := &raw.HmacKey{
		Metadata: metadata,
	}
	return pbHmacKeyToHMACKey(hkPb, false)
}

// Delete invokes an RPC to delete the key referenced by accessID, on Google Cloud Storage.
// Only inactive HMAC keys can be deleted.
// After deletion, a key cannot be used to authenticate requests.
func (hkh *HMACKeyHandle) Delete(ctx context.Context) error {
	delCall := hkh.raw.Delete(hkh.projectID, hkh.accessID)
	setClientHeader(delCall.Header())

	return runWithRetry(ctx, func() error {
		return delCall.Context(ctx).Do()
	})
}

func pbHmacKeyToHMACKey(pb *raw.HmacKey, isCreate bool) (*HMACKey, error) {
	pbmd := pb.Metadata
	if pbmd == nil {
		return nil, errors.New("field Metadata cannot be nil")
	}
	createdTime, err := time.Parse(time.RFC3339, pbmd.TimeCreated)
	if err != nil {
		return nil, fmt.Errorf("field CreatedTime: %v", err)
	}
	updatedTime, err := time.Parse(time.RFC3339, pbmd.Updated)
	if err != nil && !isCreate {
		return nil, fmt.Errorf("field UpdatedTime: %v", err)
	}

	hmk := &HMACKey{
		AccessID:    pbmd.AccessId,
		Secret:      pb.Secret,
		Etag:        pbmd.Etag,
		ID:          pbmd.Id,
		State:       HMACState(pbmd.State),
		ProjectID:   pbmd.ProjectId,
		CreatedTime: createdTime,
		UpdatedTime: updatedTime,

		ServiceAccountEmail: pbmd.ServiceAccountEmail,
	}

	return hmk, nil
}

// CreateHMACKey invokes an RPC for Google Cloud Storage to create a new HMACKey.
func (c *Client) CreateHMACKey(ctx context.Context, projectID, serviceAccountEmail string) (*HMACKey, error) {
	if projectID == "" {
		return nil, errors.New("storage: expecting a non-blank projectID")
	}
	if serviceAccountEmail == "" {
		return nil, errors.New("storage: expecting a non-blank service account email")
	}

	svc := raw.NewProjectsHmacKeysService(c.raw)
	call := svc.Create(projectID, serviceAccountEmail)
	setClientHeader(call.Header())

	var hkPb *raw.HmacKey
	var err error
	err = runWithRetry(ctx, func() error {
		hkPb, err = call.Context(ctx).Do()
		return err
	})
	if err != nil {
		return nil, err
	}

	return pbHmacKeyToHMACKey(hkPb, true)
}

// HMACKeyAttrsToUpdate defines the attributes of an HMACKey that will be updated.
type HMACKeyAttrsToUpdate struct {
	// State is required and must be either StateActive or StateInactive.
	State HMACState

	// Etag is an optional field and it is the HTTP/1.1 Entity tag.
	Etag string
}

// Update mutates the HMACKey referred to by accessID.
func (h *HMACKeyHandle) Update(ctx context.Context, au HMACKeyAttrsToUpdate) (*HMACKey, error) {
	if au.State != Active && au.State != Inactive {
		return nil, fmt.Errorf("storage: invalid state %q for update, must be either %q or %q", au.State, Active, Inactive)
	}

	call := h.raw.Update(h.projectID, h.accessID, &raw.HmacKeyMetadata{
		Etag:  au.Etag,
		State: string(au.State),
	})
	setClientHeader(call.Header())

	var metadata *raw.HmacKeyMetadata
	var err error
	err = runWithRetry(ctx, func() error {
		metadata, err = call.Context(ctx).Do()
		return err
	})

	if err != nil {
		return nil, err
	}
	hkPb := &raw.HmacKey{
		Metadata: metadata,
	}
	return pbHmacKeyToHMACKey(hkPb, false)
}
