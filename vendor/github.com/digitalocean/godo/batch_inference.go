package godo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const (
	defaultBatchInferenceBaseURL = "https://inference.do-ai.run/"
	batchInferenceBasePath       = "v1/batches"
	batchInferenceFilePath       = batchInferenceBasePath + "/files"
	batchInferenceByIDPath       = batchInferenceBasePath + "/%s"
	batchInferenceResultsPath    = batchInferenceBasePath + "/%s/results"
	batchInferenceCancelPath     = batchInferenceBasePath + "/%s/cancel"
)

// BatchInferenceService is an interface for managing batch inference jobs
// via the inference proxy at inference.do-ai.run.
type BatchInferenceService interface {
	CreatePresignedUploadURL(context.Context, *CreateBatchFileRequest) (*CreateBatchFileResponse, *Response, error)
	UploadInputFile(ctx context.Context, uploadURL string, content io.Reader) (*Response, error)
	CreateJob(context.Context, *CreateBatchRequest) (*Batch, *Response, error)
	ListJobs(context.Context, *ListBatchesOptions) (*ListBatchesResponse, *Response, error)
	GetJob(context.Context, string) (*Batch, *Response, error)
	CancelJob(context.Context, string) (*Batch, *Response, error)
	GetJobResult(context.Context, string) (*BatchResultsResponse, *Response, error)
}

// BatchInferenceServiceOp handles communication with the batch inference
// endpoints on the inference proxy (inference.do-ai.run).
type BatchInferenceServiceOp struct {
	client  *Client
	baseURL *url.URL
}

var _ BatchInferenceService = &BatchInferenceServiceOp{}

// -- Request types --

// CreateBatchFileRequest represents a request to create a presigned URL for
// uploading a batch JSONL input file.
type CreateBatchFileRequest struct {
	FileName string `json:"file_name"`
}

// CreateBatchRequest represents a request to create a batch inference job.
// Provider is always required. Endpoint is required only when Provider is "openai".
type CreateBatchRequest struct {
	Provider         string `json:"provider"`
	FileID           string `json:"file_id"`
	CompletionWindow string `json:"completion_window"`
	RequestID        string `json:"request_id,omitempty"`
	Endpoint         string `json:"endpoint,omitempty"`
}

// ListBatchesOptions specifies optional query parameters for listing batch jobs.
type ListBatchesOptions struct {
	After  string `url:"after,omitempty"`
	Limit  int    `url:"limit,omitempty"`
	Status string `url:"status,omitempty"`
}

// -- Response types --

// CreateBatchFileResponse is returned when creating a presigned file upload URL.
type CreateBatchFileResponse struct {
	FileID    string `json:"file_id"`
	UploadURL string `json:"upload_url"`
	ExpiresAt string `json:"expires_at"`
}

// BatchRequestCounts holds per-request completion counts for a batch job.
type BatchRequestCounts struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// Batch represents a batch inference job returned by the inference proxy.
// Timestamps are ISO 8601 strings. Nullable fields use *string.
type Batch struct {
	BatchID           string              `json:"batch_id"`
	Provider          string              `json:"provider"`
	FileID            string              `json:"file_id"`
	CompletionWindow  string              `json:"completion_window"`
	Status            string              `json:"status"`
	RequestID         string              `json:"request_id"`
	RequestCounts     *BatchRequestCounts `json:"request_counts,omitempty"`
	ResultAvailable   bool                `json:"result_available"`
	CancelRequestedAt *string             `json:"cancel_requested_at"`
	CreatedAt         string              `json:"created_at"`
	UpdatedAt         string              `json:"updated_at"`
	ExpiresAt         *string             `json:"expires_at"`
}

// BatchEdge is a single edge in the Relay-style batch list response.
type BatchEdge struct {
	Cursor string `json:"cursor"`
	Node   Batch  `json:"node"`
}

// BatchPageInfo holds forward-only pagination info for batch listing.
type BatchPageInfo struct {
	EndCursor   string `json:"endCursor"`
	HasNextPage bool   `json:"hasNextPage"`
}

// ListBatchesResponse is returned by GET /v1/batches.
// Uses Relay-style cursor pagination.
type ListBatchesResponse struct {
	Edges    []BatchEdge   `json:"edges"`
	PageInfo BatchPageInfo `json:"page_info"`
}

// BatchResultsDownload holds the presigned download URL and its expiry.
type BatchResultsDownload struct {
	PresignedURL string `json:"presigned_url"`
	ExpiresAt    string `json:"expires_at"`
}

// BatchResultsResponse is returned by GET /v1/batches/{batch_id}/results.
type BatchResultsResponse struct {
	Download     BatchResultsDownload `json:"download"`
	OutputFileID string               `json:"output_file_id"`
}

// -- helpers --

// newRequest builds an HTTP request resolved against the inference proxy base URL
// rather than the default api.digitalocean.com. The resulting absolute URL is
// passed to Client.NewRequest so that standard headers and auth are applied.
func (s *BatchInferenceServiceOp) newRequest(ctx context.Context, method, path string, body interface{}) (*http.Request, error) {
	rel, err := url.Parse(path)
	if err != nil {
		return nil, err
	}
	u := s.baseURL.ResolveReference(rel)
	return s.client.NewRequest(ctx, method, u.String(), body)
}

// -- Service methods --

// CreatePresignedUploadURL creates a presigned URL for uploading a batch JSONL input file.
func (s *BatchInferenceServiceOp) CreatePresignedUploadURL(ctx context.Context, createReq *CreateBatchFileRequest) (*CreateBatchFileResponse, *Response, error) {
	req, err := s.newRequest(ctx, http.MethodPost, batchInferenceFilePath, createReq)
	if err != nil {
		return nil, nil, err
	}

	root := new(CreateBatchFileResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// UploadInputFile uploads batch JSONL content to a presigned S3 URL returned by
// CreatePresignedUploadURL. A plain HTTP client is used (no Authorization header)
// because the presigned URL already embeds authentication in its query parameters.
func (s *BatchInferenceServiceOp) UploadInputFile(ctx context.Context, uploadURL string, content io.Reader) (*Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, content)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	response := newResponse(resp)
	if err := CheckResponse(resp); err != nil {
		return response, err
	}
	return response, nil
}

// CreateJob creates a new batch inference job.
func (s *BatchInferenceServiceOp) CreateJob(ctx context.Context, createReq *CreateBatchRequest) (*Batch, *Response, error) {
	req, err := s.newRequest(ctx, http.MethodPost, batchInferenceBasePath, createReq)
	if err != nil {
		return nil, nil, err
	}

	root := new(Batch)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// ListJobs returns a list of batch inference jobs with optional filtering and pagination.
func (s *BatchInferenceServiceOp) ListJobs(ctx context.Context, opts *ListBatchesOptions) (*ListBatchesResponse, *Response, error) {
	path, err := addOptions(batchInferenceBasePath, opts)
	if err != nil {
		return nil, nil, err
	}

	req, err := s.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(ListBatchesResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetJob retrieves a batch inference job by ID.
func (s *BatchInferenceServiceOp) GetJob(ctx context.Context, batchID string) (*Batch, *Response, error) {
	path := fmt.Sprintf(batchInferenceByIDPath, batchID)

	req, err := s.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(Batch)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// CancelJob requests cancellation of a batch inference job.
func (s *BatchInferenceServiceOp) CancelJob(ctx context.Context, batchID string) (*Batch, *Response, error) {
	path := fmt.Sprintf(batchInferenceCancelPath, batchID)

	req, err := s.newRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(Batch)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}

// GetJobResult retrieves the result download URLs for a completed batch inference job.
func (s *BatchInferenceServiceOp) GetJobResult(ctx context.Context, batchID string) (*BatchResultsResponse, *Response, error) {
	path := fmt.Sprintf(batchInferenceResultsPath, batchID)

	req, err := s.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(BatchResultsResponse)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}
	return root, resp, nil
}
