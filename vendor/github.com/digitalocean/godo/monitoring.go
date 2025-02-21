package godo

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/digitalocean/godo/metrics"
)

const (
	monitoringBasePath     = "v2/monitoring"
	alertPolicyBasePath    = monitoringBasePath + "/alerts"
	dropletMetricsBasePath = monitoringBasePath + "/metrics/droplet"

	DropletCPUUtilizationPercent        = "v1/insights/droplet/cpu"
	DropletMemoryUtilizationPercent     = "v1/insights/droplet/memory_utilization_percent"
	DropletDiskUtilizationPercent       = "v1/insights/droplet/disk_utilization_percent"
	DropletPublicOutboundBandwidthRate  = "v1/insights/droplet/public_outbound_bandwidth"
	DropletPublicInboundBandwidthRate   = "v1/insights/droplet/public_inbound_bandwidth"
	DropletPrivateOutboundBandwidthRate = "v1/insights/droplet/private_outbound_bandwidth"
	DropletPrivateInboundBandwidthRate  = "v1/insights/droplet/private_inbound_bandwidth"
	DropletDiskReadRate                 = "v1/insights/droplet/disk_read"
	DropletDiskWriteRate                = "v1/insights/droplet/disk_write"
	DropletOneMinuteLoadAverage         = "v1/insights/droplet/load_1"
	DropletFiveMinuteLoadAverage        = "v1/insights/droplet/load_5"
	DropletFifteenMinuteLoadAverage     = "v1/insights/droplet/load_15"

	LoadBalancerCPUUtilizationPercent                = "v1/insights/lbaas/avg_cpu_utilization_percent"
	LoadBalancerConnectionUtilizationPercent         = "v1/insights/lbaas/connection_utilization_percent"
	LoadBalancerDropletHealth                        = "v1/insights/lbaas/droplet_health"
	LoadBalancerTLSUtilizationPercent                = "v1/insights/lbaas/tls_connections_per_second_utilization_percent"
	LoadBalancerIncreaseInHTTPErrorRatePercentage5xx = "v1/insights/lbaas/increase_in_http_error_rate_percentage_5xx"
	LoadBalancerIncreaseInHTTPErrorRatePercentage4xx = "v1/insights/lbaas/increase_in_http_error_rate_percentage_4xx"
	LoadBalancerIncreaseInHTTPErrorRateCount5xx      = "v1/insights/lbaas/increase_in_http_error_rate_count_5xx"
	LoadBalancerIncreaseInHTTPErrorRateCount4xx      = "v1/insights/lbaas/increase_in_http_error_rate_count_4xx"
	LoadBalancerHighHttpResponseTime                 = "v1/insights/lbaas/high_http_request_response_time"
	LoadBalancerHighHttpResponseTime50P              = "v1/insights/lbaas/high_http_request_response_time_50p"
	LoadBalancerHighHttpResponseTime95P              = "v1/insights/lbaas/high_http_request_response_time_95p"
	LoadBalancerHighHttpResponseTime99P              = "v1/insights/lbaas/high_http_request_response_time_99p"

	DbaasFifteenMinuteLoadAverage = "v1/dbaas/alerts/load_15_alerts"
	DbaasMemoryUtilizationPercent = "v1/dbaas/alerts/memory_utilization_alerts"
	DbaasDiskUtilizationPercent   = "v1/dbaas/alerts/disk_utilization_alerts"
	DbaasCPUUtilizationPercent    = "v1/dbaas/alerts/cpu_alerts"
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

	GetDropletBandwidth(context.Context, *DropletBandwidthMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletAvailableMemory(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletCPU(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletFilesystemFree(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletFilesystemSize(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletLoad1(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletLoad5(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletLoad15(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletCachedMemory(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletFreeMemory(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
	GetDropletTotalMemory(context.Context, *DropletMetricsRequest) (*MetricsResponse, *Response, error)
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

// DropletMetricsRequest holds the information needed to retrieve Droplet various metrics.
type DropletMetricsRequest struct {
	HostID string
	Start  time.Time
	End    time.Time
}

// DropletBandwidthMetricsRequest holds the information needed to retrieve Droplet bandwidth metrics.
type DropletBandwidthMetricsRequest struct {
	DropletMetricsRequest
	Interface string
	Direction string
}

// MetricsResponse holds a Metrics query response.
type MetricsResponse struct {
	Status string      `json:"status"`
	Data   MetricsData `json:"data"`
}

// MetricsData holds the data portion of a Metrics response.
type MetricsData struct {
	ResultType string                 `json:"resultType"`
	Result     []metrics.SampleStream `json:"result"`
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

// GetDropletBandwidth retrieves Droplet bandwidth metrics.
func (s *MonitoringServiceOp) GetDropletBandwidth(ctx context.Context, args *DropletBandwidthMetricsRequest) (*MetricsResponse, *Response, error) {
	path := dropletMetricsBasePath + "/bandwidth"
	req, err := s.client.NewRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}

	q := req.URL.Query()
	q.Add("host_id", args.HostID)
	q.Add("interface", args.Interface)
	q.Add("direction", args.Direction)
	q.Add("start", fmt.Sprintf("%d", args.Start.Unix()))
	q.Add("end", fmt.Sprintf("%d", args.End.Unix()))
	req.URL.RawQuery = q.Encode()

	root := new(MetricsResponse)
	resp, err := s.client.Do(ctx, req, root)

	return root, resp, err
}

// GetDropletCPU retrieves Droplet CPU metrics.
func (s *MonitoringServiceOp) GetDropletCPU(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/cpu", args)
}

// GetDropletFilesystemFree retrieves Droplet filesystem free metrics.
func (s *MonitoringServiceOp) GetDropletFilesystemFree(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/filesystem_free", args)
}

// GetDropletFilesystemSize retrieves Droplet filesystem size metrics.
func (s *MonitoringServiceOp) GetDropletFilesystemSize(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/filesystem_size", args)
}

// GetDropletLoad1 retrieves Droplet load 1 metrics.
func (s *MonitoringServiceOp) GetDropletLoad1(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/load_1", args)
}

// GetDropletLoad5 retrieves Droplet load 5 metrics.
func (s *MonitoringServiceOp) GetDropletLoad5(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/load_5", args)
}

// GetDropletLoad15 retrieves Droplet load 15 metrics.
func (s *MonitoringServiceOp) GetDropletLoad15(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/load_15", args)
}

// GetDropletCachedMemory retrieves Droplet cached memory metrics.
func (s *MonitoringServiceOp) GetDropletCachedMemory(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/memory_cached", args)
}

// GetDropletFreeMemory retrieves Droplet free memory metrics.
func (s *MonitoringServiceOp) GetDropletFreeMemory(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/memory_free", args)
}

// GetDropletTotalMemory retrieves Droplet total memory metrics.
func (s *MonitoringServiceOp) GetDropletTotalMemory(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/memory_total", args)
}

// GetDropletAvailableMemory retrieves Droplet available memory metrics.
func (s *MonitoringServiceOp) GetDropletAvailableMemory(ctx context.Context, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	return s.getDropletMetrics(ctx, "/memory_available", args)
}

func (s *MonitoringServiceOp) getDropletMetrics(ctx context.Context, path string, args *DropletMetricsRequest) (*MetricsResponse, *Response, error) {
	fullPath := dropletMetricsBasePath + path
	req, err := s.client.NewRequest(ctx, http.MethodGet, fullPath, nil)
	if err != nil {
		return nil, nil, err
	}

	q := req.URL.Query()
	q.Add("host_id", args.HostID)
	q.Add("start", fmt.Sprintf("%d", args.Start.Unix()))
	q.Add("end", fmt.Sprintf("%d", args.End.Unix()))
	req.URL.RawQuery = q.Encode()

	root := new(MetricsResponse)
	resp, err := s.client.Do(ctx, req, root)

	return root, resp, err
}
