package delete

import (
	"time"

	"github.com/gorilla/websocket"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/volume"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// mockDeleteClient implements the client.Client interface for testing delete functionality
type mockDeleteClient struct {
	// Call tracking
	createDeleteRequestCalls int
	listDeleteRequestsCalls  int
	cancelDeleteRequestCalls int

	// Response control
	deleteRequests []client.DeleteRequest
	createError    error
	listError      error
	cancelError    error

	// Parameter tracking
	lastCreateParams    client.DeleteRequestParams
	lastCreateQuiet     bool
	lastListQuiet       bool
	lastCancelRequestID string
	lastCancelForce     bool
	lastCancelQuiet     bool
}

// newMockDeleteClient creates a new mock client with default behavior
func newMockDeleteClient() *mockDeleteClient {
	return &mockDeleteClient{
		deleteRequests: []client.DeleteRequest{},
	}
}

// withDeleteRequests sets the delete requests to return from ListDeleteRequests
func (m *mockDeleteClient) withDeleteRequests(requests []client.DeleteRequest) *mockDeleteClient {
	m.deleteRequests = requests
	return m
}

// withCreateError sets an error to return from CreateDeleteRequest
func (m *mockDeleteClient) withCreateError(err error) *mockDeleteClient {
	m.createError = err
	return m
}

// withListError sets an error to return from ListDeleteRequests
func (m *mockDeleteClient) withListError(err error) *mockDeleteClient {
	m.listError = err
	return m
}

// withCancelError sets an error to return from CancelDeleteRequest
func (m *mockDeleteClient) withCancelError(err error) *mockDeleteClient {
	m.cancelError = err
	return m
}

// Delete-specific method implementations
func (m *mockDeleteClient) CreateDeleteRequest(params client.DeleteRequestParams, quiet bool) error {
	m.createDeleteRequestCalls++
	m.lastCreateParams = params
	m.lastCreateQuiet = quiet
	return m.createError
}

func (m *mockDeleteClient) ListDeleteRequests(quiet bool) ([]client.DeleteRequest, error) {
	m.listDeleteRequestsCalls++
	m.lastListQuiet = quiet
	if m.listError != nil {
		return nil, m.listError
	}
	return m.deleteRequests, nil
}

func (m *mockDeleteClient) CancelDeleteRequest(requestID string, force bool, quiet bool) error {
	m.cancelDeleteRequestCalls++
	m.lastCancelRequestID = requestID
	m.lastCancelForce = force
	m.lastCancelQuiet = quiet
	return m.cancelError
}

// Stub implementations for other Client interface methods
func (m *mockDeleteClient) Query(_ string, _ int, _ time.Time, _ logproto.Direction, _ bool) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) QueryRange(_ string, _ int, _, _ time.Time, _ logproto.Direction, _, _ time.Duration, _ bool) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) ListLabelNames(_ bool, _, _ time.Time) (*loghttp.LabelResponse, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) ListLabelValues(_ string, _ bool, _, _ time.Time) (*loghttp.LabelResponse, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) Series(_ []string, _, _ time.Time, _ bool) (*loghttp.SeriesResponse, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) LiveTailQueryConn(_ string, _ time.Duration, _ int, _ time.Time, _ bool) (*websocket.Conn, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) GetOrgID() string {
	return "test-org"
}

func (m *mockDeleteClient) GetStats(_ string, _, _ time.Time, _ bool) (*logproto.IndexStatsResponse, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) GetVolume(_ *volume.Query) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) GetVolumeRange(_ *volume.Query) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (m *mockDeleteClient) GetDetectedFields(_, _ string, _, _ int, _, _ time.Time, _ time.Duration, _ bool) (*loghttp.DetectedFieldsResponse, error) {
	panic("not implemented")
}
