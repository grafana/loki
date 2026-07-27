package godo

import (
	"context"
	"fmt"
	"net/http"
)

const microDropletImageBasePath = "v2/microdroplets/images"

// MicroDropletImageStatus represents the lifecycle status of a MicroDroplet image.
type MicroDropletImageStatus string

// Possible states for a MicroDroplet image.
const (
	MicroDropletImageStatusUnknown   = MicroDropletImageStatus("IMAGE_UNKNOWN")
	MicroDropletImageStatusImporting = MicroDropletImageStatus("IMAGE_IMPORTING")
	MicroDropletImageStatusAvailable = MicroDropletImageStatus("IMAGE_AVAILABLE")
	MicroDropletImageStatusFailed    = MicroDropletImageStatus("IMAGE_FAILED")
	MicroDropletImageStatusDeleted   = MicroDropletImageStatus("IMAGE_DELETED")
)

// MicroDropletImagesService is an interface for interfacing with the
// MicroDroplet image endpoints of the DigitalOcean API.
// See: https://docs.digitalocean.com/reference/api/api-reference/#tag/MicroDroplets
type MicroDropletImagesService interface {
	List(ctx context.Context, opt *ListOptions) ([]MicroDropletImage, *Response, error)
	Get(ctx context.Context, id string) (*MicroDropletImage, *Response, error)
	Create(ctx context.Context, createRequest *MicroDropletImageCreateRequest) (*MicroDropletImage, *Response, error)
	Delete(ctx context.Context, id string) (*Response, error)
}

// MicroDropletImagesServiceOp handles communication with the MicroDroplet
// image related methods of the DigitalOcean API.
type MicroDropletImagesServiceOp struct {
	client *Client
}

var _ MicroDropletImagesService = &MicroDropletImagesServiceOp{}

// MicroDropletImage represents an OCI image imported for use with MicroDroplets.
type MicroDropletImage struct {
	ID      string                  `json:"id,omitempty"`
	Name    string                  `json:"name,omitempty"`
	Source  string                  `json:"source,omitempty"`
	Status  MicroDropletImageStatus `json:"status,omitempty"`
	Created string                  `json:"created_at,omitempty"`
}

// MicroDropletImageCreateRequest represents a request to import a new
// MicroDroplet image from a public OCI ref or a DOCR ref.
type MicroDropletImageCreateRequest struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// String returns a human-readable description of a MicroDropletImage.
func (i MicroDropletImage) String() string {
	return Stringify(i)
}

// URN returns the MicroDropletImage ID in a valid DO API URN form.
func (i MicroDropletImage) URN() string {
	return ToURN("MicroDropletImage", i.ID)
}

// String returns a human-readable description of a MicroDropletImageCreateRequest.
func (r MicroDropletImageCreateRequest) String() string {
	return Stringify(r)
}

type microDropletImageRoot struct {
	Image *MicroDropletImage `json:"image"`
}

type microDropletImagesRoot struct {
	Images []MicroDropletImage `json:"images"`
	Links  *Links              `json:"links"`
	Meta   *Meta               `json:"meta"`
}

// List lists all MicroDroplet images, with optional pagination.
func (s *MicroDropletImagesServiceOp) List(ctx context.Context, opt *ListOptions) ([]MicroDropletImage, *Response, error) {
	path, err := addOptions(microDropletImageBasePath, opt)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microDropletImagesRoot)
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

	return root.Images, resp, nil
}

// Get retrieves a MicroDroplet image by its ID.
func (s *MicroDropletImagesServiceOp) Get(ctx context.Context, id string) (*MicroDropletImage, *Response, error) {
	if id == "" {
		return nil, nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", microDropletImageBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(microDropletImageRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Image, resp, nil
}

// Create imports a new MicroDroplet image from an OCI ref. Image import is
// asynchronous; callers should poll Get and inspect Status until it reports
// MicroDropletImageStatusAvailable or MicroDropletImageStatusFailed.
func (s *MicroDropletImagesServiceOp) Create(ctx context.Context, createRequest *MicroDropletImageCreateRequest) (*MicroDropletImage, *Response, error) {
	if createRequest == nil {
		return nil, nil, NewArgError("createRequest", "cannot be nil")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, microDropletImageBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(microDropletImageRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.Image, resp, nil
}

// Delete removes a MicroDroplet image by its ID. The DigitalOcean API returns
// a 204 on success and does not include a response body.
func (s *MicroDropletImagesServiceOp) Delete(ctx context.Context, id string) (*Response, error) {
	if id == "" {
		return nil, NewArgError("id", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", microDropletImageBasePath, id)

	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	return s.client.Do(ctx, req, nil)
}
