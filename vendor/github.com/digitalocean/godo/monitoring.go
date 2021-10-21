package godo

import (
	"context"
	"fmt"
	"net/http"
)

const (
	monitoringBasePath  = "v2/monitoring"
	alertPolicyBasePath = monitoringBasePath + "/alerts"

	DropletCPUUtilizationPercent       = "v1/insights/droplet/cpu"
	DropletMemoryUtilizationPercent    = "v1/insights/droplet/memory_utilization_percent"
	DropletDiskUtilizationPercent      = "v1/insights/droplet/disk_utilization_percent"
	DropletPublicOutboundBandwidthRate = "v1/insights/droplet/public_outbound_bandwidth"
	DropletDiskReadRate                = "v1/insights/droplet/disk_read"
	DropletDiskWriteRate               = "v1/insights/droplet/disk_write"
	DropletOneMinuteLoadAverage        = "v1/insights/droplet/load_1"
	DropletFiveMinuteLoadAverage       = "v1/insights/droplet/load_5"
	DropletFifteenMinuteLoadAverage    = "v1/insights/droplet/load_15"
)

// MonitoringService is an interface for interfacing with the
// monitoring endpoints of the DigitalOcean API
// See: https://docs.digitalocean.com/reference/api/api-reference/#tag/Monitoring
type MonitoringService interface {
	ListAlertPolicies(context.Context, *ListOptions) ([]AlertPolicy, *Response, error)
	GetAlertPolicy(context.Context, string) (*AlertPolicy, *Response, error)
	CreateAlertPolicy(context.Context, *AlertPolicyCreateRequest) (*AlertPolicy, *Response, error)
	UpdateAlertPolicy(context.Context, string, *AlertPolicyUpdateRequest) (*AlertPolicy, *Response, error)
	DeleteAlertPolicy(context.Context, string) (*Response, error)
}

// MonitoringServiceOp handles communication with monitoring related methods of the
// DigitalOcean API.
type MonitoringServiceOp struct {
	client *Client
}

var _ MonitoringService = &MonitoringServiceOp{}

// AlertPolicy represents a DigitalOcean alert policy
type AlertPolicy struct {
	UUID        string          `json:"uuid"`
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Compare     AlertPolicyComp `json:"compare"`
	Value       float32         `json:"value"`
	Window      string          `json:"window"`
	Entities    []string        `json:"entities"`
	Tags        []string        `json:"tags"`
	Alerts      Alerts          `json:"alerts"`
	Enabled     bool            `json:"enabled"`
}

// Alerts represents the alerts section of an alert policy
type Alerts struct {
	Slack []SlackDetails `json:"slack"`
	Email []string       `json:"email"`
}

// SlackDetails represents the details required to send a slack alert
type SlackDetails struct {
	URL     string `json:"url"`
	Channel string `json:"channel"`
}

// AlertPolicyComp represents an alert policy comparison operation
type AlertPolicyComp string

const (
	// GreaterThan is the comparison >
	GreaterThan AlertPolicyComp = "GreaterThan"
	// LessThan is the comparison <
	LessThan AlertPolicyComp = "LessThan"
)

// AlertPolicyCreateRequest holds the info for creating a new alert policy
type AlertPolicyCreateRequest struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Compare     AlertPolicyComp `json:"compare"`
	Value       float32         `json:"value"`
	Window      string          `json:"window"`
	Entities    []string        `json:"entities"`
	Tags        []string        `json:"tags"`
	Alerts      Alerts          `json:"alerts"`
	Enabled     *bool           `json:"enabled"`
}

// AlertPolicyUpdateRequest holds the info for updating an existing alert policy
type AlertPolicyUpdateRequest struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Compare     AlertPolicyComp `json:"compare"`
	Value       float32         `json:"value"`
	Window      string          `json:"window"`
	Entities    []string        `json:"entities"`
	Tags        []string        `json:"tags"`
	Alerts      Alerts          `json:"alerts"`
	Enabled     *bool           `json:"enabled"`
}

type alertPoliciesRoot struct {
	AlertPolicies []AlertPolicy `json:"policies"`
	Links         *Links        `json:"links"`
	Meta          *Meta         `json:"meta"`
}

type alertPolicyRoot struct {
	AlertPolicy *AlertPolicy `json:"policy,omitempty"`
}

// ListAlertPolicies all alert policies
func (s *MonitoringServiceOp) ListAlertPolicies(ctx context.Context, opt *ListOptions) ([]AlertPolicy, *Response, error) {
	path := alertPolicyBasePath
	path, err := addOptions(path, opt)

	if err != nil {
		return nil, nil, err
	}

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertPoliciesRoot)
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
	return root.AlertPolicies, resp, err
}

// GetAlertPolicy gets a single alert policy
func (s *MonitoringServiceOp) GetAlertPolicy(ctx context.Context, uuid string) (*AlertPolicy, *Response, error) {
	path := fmt.Sprintf("%s/%s", alertPolicyBasePath, uuid)

	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertPolicyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AlertPolicy, resp, err
}

// CreateAlertPolicy creates a new alert policy
func (s *MonitoringServiceOp) CreateAlertPolicy(ctx context.Context, createRequest *AlertPolicyCreateRequest) (*AlertPolicy, *Response, error) {
	if createRequest == nil {
		return nil, nil, NewArgError("createRequest", "cannot be nil")
	}

	req, err := s.client.NewRequest(ctx, http.MethodPost, alertPolicyBasePath, createRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertPolicyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AlertPolicy, resp, err
}

// UpdateAlertPolicy updates an existing alert policy
func (s *MonitoringServiceOp) UpdateAlertPolicy(ctx context.Context, uuid string, updateRequest *AlertPolicyUpdateRequest) (*AlertPolicy, *Response, error) {
	if uuid == "" {
		return nil, nil, NewArgError("uuid", "cannot be empty")
	}
	if updateRequest == nil {
		return nil, nil, NewArgError("updateRequest", "cannot be nil")
	}

	path := fmt.Sprintf("%s/%s", alertPolicyBasePath, uuid)
	req, err := s.client.NewRequest(ctx, http.MethodPut, path, updateRequest)
	if err != nil {
		return nil, nil, err
	}

	root := new(alertPolicyRoot)
	resp, err := s.client.Do(ctx, req, root)
	if err != nil {
		return nil, resp, err
	}

	return root.AlertPolicy, resp, err
}

// DeleteAlertPolicy deletes an existing alert policy
func (s *MonitoringServiceOp) DeleteAlertPolicy(ctx context.Context, uuid string) (*Response, error) {
	if uuid == "" {
		return nil, NewArgError("uuid", "cannot be empty")
	}

	path := fmt.Sprintf("%s/%s", alertPolicyBasePath, uuid)
	req, err := s.client.NewRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(ctx, req, nil)

	return resp, err
}
