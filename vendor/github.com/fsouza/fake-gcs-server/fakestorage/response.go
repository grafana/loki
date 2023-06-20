// Copyright 2017 Francisco Souza. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fakestorage

import (
	"time"

	"github.com/fsouza/fake-gcs-server/internal/backend"
)

const timestampFormat = "2006-01-02T15:04:05.999999Z07:00"

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(timestampFormat)
}

type listResponse struct {
	Kind     string   `json:"kind"`
	Items    []any    `json:"items"`
	Prefixes []string `json:"prefixes,omitempty"`
}

func newListBucketsResponse(buckets []backend.Bucket, location string) listResponse {
	resp := listResponse{
		Kind:  "storage#buckets",
		Items: make([]any, len(buckets)),
	}
	for i, bucket := range buckets {
		resp.Items[i] = newBucketResponse(bucket, location)
	}
	return resp
}

type bucketResponse struct {
	Kind        string            `json:"kind"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Versioning  *bucketVersioning `json:"versioning,omitempty"`
	TimeCreated string            `json:"timeCreated,omitempty"`
	Location    string            `json:"location,omitempty"`
}

type bucketVersioning struct {
	Enabled bool `json:"enabled,omitempty"`
}

func newBucketResponse(bucket backend.Bucket, location string) bucketResponse {
	return bucketResponse{
		Kind:        "storage#bucket",
		ID:          bucket.Name,
		Name:        bucket.Name,
		Versioning:  &bucketVersioning{bucket.VersioningEnabled},
		TimeCreated: formatTime(bucket.TimeCreated),
		Location:    location,
	}
}

func newListObjectsResponse(objs []ObjectAttrs, prefixes []string) listResponse {
	resp := listResponse{
		Kind:     "storage#objects",
		Items:    make([]any, len(objs)),
		Prefixes: prefixes,
	}
	for i, obj := range objs {
		resp.Items[i] = newObjectResponse(obj)
	}
	return resp
}

// objectAccessControl is copied from the Google SDK to avoid direct
// dependency.
type objectAccessControl struct {
	Bucket      string `json:"bucket,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Email       string `json:"email,omitempty"`
	Entity      string `json:"entity,omitempty"`
	EntityID    string `json:"entityId,omitempty"`
	Etag        string `json:"etag,omitempty"`
	Generation  int64  `json:"generation,omitempty,string"`
	ID          string `json:"id,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Object      string `json:"object,omitempty"`
	ProjectTeam struct {
		ProjectNumber string `json:"projectNumber,omitempty"`
		Team          string `json:"team,omitempty"`
	} `json:"projectTeam,omitempty"`
	Role     string `json:"role,omitempty"`
	SelfLink string `json:"selfLink,omitempty"`
}

type objectResponse struct {
	Kind            string                 `json:"kind"`
	Name            string                 `json:"name"`
	ID              string                 `json:"id"`
	Bucket          string                 `json:"bucket"`
	Size            int64                  `json:"size,string"`
	ContentType     string                 `json:"contentType,omitempty"`
	ContentEncoding string                 `json:"contentEncoding,omitempty"`
	Crc32c          string                 `json:"crc32c,omitempty"`
	ACL             []*objectAccessControl `json:"acl,omitempty"`
	Md5Hash         string                 `json:"md5Hash,omitempty"`
	Etag            string                 `json:"etag,omitempty"`
	TimeCreated     string                 `json:"timeCreated,omitempty"`
	TimeDeleted     string                 `json:"timeDeleted,omitempty"`
	Updated         string                 `json:"updated,omitempty"`
	Generation      int64                  `json:"generation,string"`
	CustomTime      string                 `json:"customTime,omitempty"`
	Metadata        map[string]string      `json:"metadata,omitempty"`
}

func newObjectResponse(obj ObjectAttrs) objectResponse {
	acl := getAccessControlsListFromObject(obj)

	return objectResponse{
		Kind:            "storage#object",
		ID:              obj.id(),
		Bucket:          obj.BucketName,
		Name:            obj.Name,
		Size:            obj.Size,
		ContentType:     obj.ContentType,
		ContentEncoding: obj.ContentEncoding,
		Crc32c:          obj.Crc32c,
		Md5Hash:         obj.Md5Hash,
		Etag:            obj.Etag,
		ACL:             acl,
		Metadata:        obj.Metadata,
		TimeCreated:     formatTime(obj.Created),
		TimeDeleted:     formatTime(obj.Deleted),
		Updated:         formatTime(obj.Updated),
		CustomTime:      formatTime(obj.CustomTime),
		Generation:      obj.Generation,
	}
}

type aclListResponse struct {
	Items []*objectAccessControl `json:"items"`
}

func newACLListResponse(obj ObjectAttrs) aclListResponse {
	if len(obj.ACL) == 0 {
		return aclListResponse{}
	}
	return aclListResponse{Items: getAccessControlsListFromObject(obj)}
}

func getAccessControlsListFromObject(obj ObjectAttrs) []*objectAccessControl {
	aclItems := make([]*objectAccessControl, len(obj.ACL))
	for idx, aclRule := range obj.ACL {
		aclItems[idx] = &objectAccessControl{
			Bucket: obj.BucketName,
			Entity: string(aclRule.Entity),
			Object: obj.Name,
			Role:   string(aclRule.Role),
		}
	}
	return aclItems
}

type rewriteResponse struct {
	Kind                string         `json:"kind"`
	TotalBytesRewritten int64          `json:"totalBytesRewritten,string"`
	ObjectSize          int64          `json:"objectSize,string"`
	Done                bool           `json:"done"`
	RewriteToken        string         `json:"rewriteToken"`
	Resource            objectResponse `json:"resource"`
}

func newObjectRewriteResponse(obj ObjectAttrs) rewriteResponse {
	return rewriteResponse{
		Kind:                "storage#rewriteResponse",
		TotalBytesRewritten: obj.Size,
		ObjectSize:          obj.Size,
		Done:                true,
		RewriteToken:        "",
		Resource:            newObjectResponse(obj),
	}
}

type errorResponse struct {
	Error httpError `json:"error"`
}

type httpError struct {
	Code    int        `json:"code"`
	Message string     `json:"message"`
	Errors  []apiError `json:"errors"`
}

type apiError struct {
	Domain  string `json:"domain"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func newErrorResponse(code int, message string, errs []apiError) errorResponse {
	return errorResponse{
		Error: httpError{
			Code:    code,
			Message: message,
			Errors:  errs,
		},
	}
}
