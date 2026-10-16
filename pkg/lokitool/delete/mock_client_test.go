package delete

import (
	"github.com/grafana/loki/v3/pkg/lokitool/client"
)

// mockDeleteClient implements the client.DeleteClient interface for testing delete functionality
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
	lastCancelRequestID string
	lastCancelForce     bool
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
func (m *mockDeleteClient) CreateDeleteRequest(params client.DeleteRequestParams) error {
	m.createDeleteRequestCalls++
	m.lastCreateParams = params
	return m.createError
}

func (m *mockDeleteClient) ListDeleteRequests() ([]client.DeleteRequest, error) {
	m.listDeleteRequestsCalls++
	if m.listError != nil {
		return nil, m.listError
	}
	return m.deleteRequests, nil
}

func (m *mockDeleteClient) CancelDeleteRequest(requestID string, force bool) error {
	m.cancelDeleteRequestCalls++
	m.lastCancelRequestID = requestID
	m.lastCancelForce = force
	return m.cancelError
}
