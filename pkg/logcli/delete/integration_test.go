package delete

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/logcli/client"
	"github.com/grafana/loki/v3/pkg/logcli/volume"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// TestDeleteWorkflowIntegration tests the complete workflow of creating, listing, and canceling delete requests
func TestDeleteWorkflowIntegration(t *testing.T) {
	// Create a mock client that simulates the workflow
	mockClient := &workflowMockClient{
		deleteRequests: make(map[string]client.DeleteRequest),
	}

	// Test 1: Create a delete request
	createQuery := Query{
		QueryString: "{job=\"test\"}",
		Start:       time.Unix(1000, 0),
		End:         time.Unix(2000, 0),
		Quiet:       true,
	}

	err := createQuery.CreateQuery(mockClient)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(mockClient.deleteRequests))

	// Test 2: List delete requests to verify creation
	listQuery := Query{Quiet: true}
	
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = listQuery.ListQuery(mockClient)
	assert.NoError(t, err)

	// Restore stdout and capture output
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "{job=\"test\"}")
	assert.Contains(t, output, "received")

	// Test 3: Cancel the delete request
	cancelQuery := Query{Quiet: true}
	err = cancelQuery.CancelQuery(mockClient, "test-request-1", false)
	assert.NoError(t, err)

	// Test 4: Verify the request was cancelled
	requests, err := mockClient.ListDeleteRequests(true)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(requests))
	assert.Equal(t, "cancelled", requests[0].Status)
}

// TestDeleteWorkflowWithErrors tests the workflow with various error conditions
func TestDeleteWorkflowWithErrors(t *testing.T) {
	tests := []struct {
		name         string
		createError  error
		listError    error
		cancelError  error
		expectFails  []string // which operations should fail
	}{
		{
			name:        "create fails",
			createError: errors.New("create failed"),
			expectFails: []string{"create"},
		},
		{
			name:        "list fails",
			listError:   errors.New("list failed"),
			expectFails: []string{"list"},
		},
		{
			name:        "cancel fails",
			cancelError: errors.New("cancel failed"),
			expectFails: []string{"cancel"},
		},
		{
			name:        "all operations fail",
			createError: errors.New("create failed"),
			listError:   errors.New("list failed"),
			cancelError: errors.New("cancel failed"),
			expectFails: []string{"create", "list", "cancel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &workflowMockClient{
				deleteRequests: make(map[string]client.DeleteRequest),
				createError:    tt.createError,
				listError:      tt.listError,
				cancelError:    tt.cancelError,
			}

			// Test create
			createQuery := Query{
				QueryString: "{job=\"test\"}",
				Start:       time.Unix(1000, 0),
				End:         time.Unix(2000, 0),
				Quiet:       true,
			}

			err := createQuery.CreateQuery(mockClient)
			if contains(tt.expectFails, "create") {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Test list
			_, err = mockClient.ListDeleteRequests(true)
			if contains(tt.expectFails, "list") {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// Test cancel (only if create succeeded)
			if !contains(tt.expectFails, "create") {
				cancelQuery := Query{Quiet: true}
				err = cancelQuery.CancelQuery(mockClient, "test-request-1", false)
				if contains(tt.expectFails, "cancel") {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			}
		})
	}
}

// TestDeleteWorkflowMultipleRequests tests workflow with multiple delete requests
func TestDeleteWorkflowMultipleRequests(t *testing.T) {
	mockClient := &workflowMockClient{
		deleteRequests: make(map[string]client.DeleteRequest),
	}

	// Create multiple delete requests
	queries := []Query{
		{
			QueryString: "{job=\"app1\"}",
			Start:       time.Unix(1000, 0),
			End:         time.Unix(2000, 0),
			Quiet:       true,
		},
		{
			QueryString: "{job=\"app2\"}",
			Start:       time.Unix(3000, 0),
			End:         time.Unix(4000, 0),
			Quiet:       true,
		},
		{
			QueryString: "{job=\"app3\"}",
			Start:       time.Unix(5000, 0),
			End:         time.Unix(6000, 0),
			Quiet:       true,
		},
	}

	// Create all requests
	for _, query := range queries {
		err := query.CreateQuery(mockClient)
		assert.NoError(t, err)
	}

	// Verify all requests were created
	assert.Equal(t, 3, len(mockClient.deleteRequests))

	// List all requests
	listQuery := Query{Quiet: true}
	
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := listQuery.ListQuery(mockClient)
	assert.NoError(t, err)

	// Restore stdout and capture output
	w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Verify all queries appear in output
	assert.Contains(t, output, "{job=\"app1\"}")
	assert.Contains(t, output, "{job=\"app2\"}")
	assert.Contains(t, output, "{job=\"app3\"}")

	// Cancel one request
	cancelQuery := Query{Quiet: true}
	err = cancelQuery.CancelQuery(mockClient, "test-request-1", false)
	assert.NoError(t, err)

	// Verify the status changed
	requests, err := mockClient.ListDeleteRequests(true)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(requests))
	
	// Find the cancelled request
	found := false
	for _, req := range requests {
		if req.Query == "{job=\"app1\"}" {
			assert.Equal(t, "cancelled", req.Status)
			found = true
			break
		}
	}
	assert.True(t, found, "Cancelled request not found")
}

// workflowMockClient implements client.Client for integration testing
type workflowMockClient struct {
	deleteRequests map[string]client.DeleteRequest
	requestCounter int
	createError    error
	listError      error
	cancelError    error
}

func (m *workflowMockClient) CreateDeleteRequest(params client.DeleteRequestParams, quiet bool) error {
	if m.createError != nil {
		return m.createError
	}

	m.requestCounter++
	requestID := fmt.Sprintf("test-request-%d", m.requestCounter)
	
	startTime := int64(0)
	if params.Start != "" {
		startTime = parseInt64(params.Start)
	}
	
	endTime := int64(0)
	if params.End != "" {
		endTime = parseInt64(params.End)
	}

	m.deleteRequests[requestID] = client.DeleteRequest{
		StartTime: startTime,
		EndTime:   endTime,
		Query:     params.Query,
		Status:    "received",
	}

	return nil
}

func (m *workflowMockClient) ListDeleteRequests(quiet bool) ([]client.DeleteRequest, error) {
	if m.listError != nil {
		return nil, m.listError
	}

	var requests []client.DeleteRequest
	for _, req := range m.deleteRequests {
		requests = append(requests, req)
	}

	return requests, nil
}

func (m *workflowMockClient) CancelDeleteRequest(requestID string, force bool, quiet bool) error {
	if m.cancelError != nil {
		return m.cancelError
	}

	if req, exists := m.deleteRequests[requestID]; exists {
		req.Status = "cancelled"
		m.deleteRequests[requestID] = req
		return nil
	}

	return errors.New("request not found")
}

// Stub implementations for other client methods
func (m *workflowMockClient) Query(string, int, time.Time, logproto.Direction, bool) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (m *workflowMockClient) QueryRange(string, int, time.Time, time.Time, logproto.Direction, time.Duration, time.Duration, bool) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (m *workflowMockClient) ListLabelNames(bool, time.Time, time.Time) (*loghttp.LabelResponse, error) {
	panic("not implemented")
}

func (m *workflowMockClient) ListLabelValues(string, bool, time.Time, time.Time) (*loghttp.LabelResponse, error) {
	panic("not implemented")
}

func (m *workflowMockClient) Series([]string, time.Time, time.Time, bool) (*loghttp.SeriesResponse, error) {
	panic("not implemented")
}

func (m *workflowMockClient) LiveTailQueryConn(string, time.Duration, int, time.Time, bool) (*websocket.Conn, error) {
	panic("not implemented")
}

func (m *workflowMockClient) GetOrgID() string {
	return "test-org"
}

func (m *workflowMockClient) GetStats(string, time.Time, time.Time, bool) (*logproto.IndexStatsResponse, error) {
	panic("not implemented")
}

func (m *workflowMockClient) GetVolume(*volume.Query) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (m *workflowMockClient) GetVolumeRange(*volume.Query) (*loghttp.QueryResponse, error) {
	panic("not implemented")
}

func (m *workflowMockClient) GetDetectedFields(string, string, int, int, time.Time, time.Time, time.Duration, bool) (*loghttp.DetectedFieldsResponse, error) {
	panic("not implemented")
}

// Helper functions
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	// Simple implementation for test
	switch s {
	case "1000":
		return 1000
	case "2000":
		return 2000
	case "3000":
		return 3000
	case "4000":
		return 4000
	case "5000":
		return 5000
	case "6000":
		return 6000
	default:
		return 0
	}
}