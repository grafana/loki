package client

import (
	"context"
	"net/http"

	"github.com/grafana/loki/v3/pkg/util"
)

const deletePath = "/loki/api/v1/delete"

// DeleteRequest represents a log deletion request.
type DeleteRequest struct {
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
	Query     string `json:"query"`
	Status    string `json:"status"`
}

// DeleteRequestParams represents the parameters for creating a delete request.
type DeleteRequestParams struct {
	Query       string `json:"query"`
	Start       string `json:"start,omitempty"`
	End         string `json:"end,omitempty"`
	MaxInterval string `json:"max_interval,omitempty"`
}

// DeleteClient contains the methods of the delete API.
type DeleteClient interface {
	CreateDeleteRequest(params DeleteRequestParams) error
	ListDeleteRequests() ([]DeleteRequest, error)
	CancelDeleteRequest(requestID string, force bool) error
}

var _ DeleteClient = (*Client)(nil)

// CreateDeleteRequest creates a log deletion request.
func (c *Client) CreateDeleteRequest(params DeleteRequestParams) error {
	qsb := util.NewQueryStringBuilder()
	qsb.SetString("query", params.Query)
	if params.Start != "" {
		qsb.SetString("start", params.Start)
	}
	if params.End != "" {
		qsb.SetString("end", params.End)
	}
	if params.MaxInterval != "" {
		qsb.SetString("max_interval", params.MaxInterval)
	}

	return c.send(http.MethodPost, deletePath, qsb.Encode())
}

// ListDeleteRequests lists the log deletion requests.
func (c *Client) ListDeleteRequests() ([]DeleteRequest, error) {
	var deleteRequests []DeleteRequest
	if err := c.getJSON(deletePath, "", &deleteRequests); err != nil {
		return nil, err
	}
	return deleteRequests, nil
}

// CancelDeleteRequest cancels a log deletion request. Force cancels a request that is partially completed.
func (c *Client) CancelDeleteRequest(requestID string, force bool) error {
	qsb := util.NewQueryStringBuilder()
	qsb.SetString("request_id", requestID)
	if force {
		qsb.SetString("force", "true")
	}

	return c.send(http.MethodDelete, deletePath, qsb.Encode())
}

// send sends a request without a body and ignores the body of the response.
func (c *Client) send(method, path, query string) error {
	res, err := c.Do(context.Background(), method, path, query, nil)
	if err != nil {
		return err
	}
	return res.Body.Close()
}
