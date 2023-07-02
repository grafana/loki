package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

// DevicePostureIntegrationConfig contains authentication information
// for a device posture integration.
type DevicePostureIntegrationConfig struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	AuthUrl      string `json:"auth_url,omitempty"`
	ApiUrl       string `json:"api_url,omitempty"`
}

// DevicePosturIntegration represents a device posture integration.
type DevicePostureIntegration struct {
	IntegrationID string                         `json:"id,omitempty"`
	Name          string                         `json:"name,omitempty"`
	Type          string                         `json:"type,omitempty"`
	Interval      string                         `json:"interval,omitempty"`
	Config        DevicePostureIntegrationConfig `json:"config,omitempty"`
}

// DevicePostureIntegrationResponse represents the response from the get
// device posture integrations endpoint.
type DevicePostureIntegrationResponse struct {
	Result DevicePostureIntegration `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// DevicePostureIntegrationListResponse represents the response from the list
// device posture integrations endpoint.
type DevicePostureIntegrationListResponse struct {
	Result []DevicePostureIntegration `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// CreateDevicePostureIntegration creates a device posture integration within an account.
//
// API reference: https://api.cloudflare.com/#device-posture-integrations-create-device-posture-integration
func (api *API) CreateDevicePostureIntegration(ctx context.Context, accountID string, integration DevicePostureIntegration) (DevicePostureIntegration, error) {
	uri := fmt.Sprintf("/%s/%s/devices/posture/integration", AccountRouteRoot, accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, integration)
	if err != nil {
		fmt.Printf("err:%+v res:%+v\n", err, res)
		return DevicePostureIntegration{}, err
	}

	var devicePostureIntegrationResponse DevicePostureIntegrationResponse
	err = json.Unmarshal(res, &devicePostureIntegrationResponse)
	if err != nil {
		return DevicePostureIntegration{}, errors.Wrap(err, errUnmarshalError)
	}

	return devicePostureIntegrationResponse.Result, nil
}

// UpdateDevicePostureIntegration updates a device posture integration within an account.
//
// API reference: https://api.cloudflare.com/#device-posture-integrations-update-device-posture-integration
func (api *API) UpdateDevicePostureIntegration(ctx context.Context, accountID string, integration DevicePostureIntegration) (DevicePostureIntegration, error) {
	uri := fmt.Sprintf("/%s/%s/devices/posture/integration/%s", AccountRouteRoot, accountID, integration.IntegrationID)

	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, integration)
	if err != nil {
		return DevicePostureIntegration{}, err
	}

	var devicePostureIntegrationResponse DevicePostureIntegrationResponse
	err = json.Unmarshal(res, &devicePostureIntegrationResponse)
	if err != nil {
		return DevicePostureIntegration{}, errors.Wrap(err, errUnmarshalError)
	}

	return devicePostureIntegrationResponse.Result, nil
}

// DevicePostureIntegration returns a specific device posture integrations within an account.
//
// API reference: https://api.cloudflare.com/#device-posture-integrations-device-posture-integration-details
func (api *API) DevicePostureIntegration(ctx context.Context, accountID, integrationID string) (DevicePostureIntegration, error) {
	uri := fmt.Sprintf("/%s/%s/devices/posture/integration/%s", AccountRouteRoot, accountID, integrationID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return DevicePostureIntegration{}, err
	}

	var devicePostureIntegrationResponse DevicePostureIntegrationResponse
	err = json.Unmarshal(res, &devicePostureIntegrationResponse)
	if err != nil {
		return DevicePostureIntegration{}, errors.Wrap(err, errUnmarshalError)
	}

	return devicePostureIntegrationResponse.Result, nil
}

// DevicePostureIntegrations returns all device posture integrations within an account.
//
// API reference: https://api.cloudflare.com/#device-posture-integrations-list-device-posture-integrations
func (api *API) DevicePostureIntegrations(ctx context.Context, accountID string) ([]DevicePostureIntegration, ResultInfo, error) {
	uri := fmt.Sprintf("/%s/%s/devices/posture/integration", AccountRouteRoot, accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []DevicePostureIntegration{}, ResultInfo{}, err
	}

	var devicePostureIntegrationListResponse DevicePostureIntegrationListResponse
	err = json.Unmarshal(res, &devicePostureIntegrationListResponse)
	if err != nil {
		return []DevicePostureIntegration{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}

	return devicePostureIntegrationListResponse.Result, devicePostureIntegrationListResponse.ResultInfo, nil
}

// DeleteDevicePostureIntegration deletes a device posture integration.
//
// API reference: https://api.cloudflare.com/#device-posture-integrations-delete-device-posture-integration
func (api *API) DeleteDevicePostureIntegration(ctx context.Context, accountID, ruleID string) error {
	uri := fmt.Sprintf(
		"/%s/%s/devices/posture/integration/%s",
		AccountRouteRoot,
		accountID,
		ruleID,
	)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}

// DevicePostureRule represents a device posture rule.
type DevicePostureRule struct {
	ID          string                   `json:"id,omitempty"`
	Type        string                   `json:"type"`
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	Schedule    string                   `json:"schedule,omitempty"`
	Match       []DevicePostureRuleMatch `json:"match,omitempty"`
	Input       DevicePostureRuleInput   `json:"input,omitempty"`
}

// DevicePostureRuleMatch represents the conditions that the client must match to run the rule.
type DevicePostureRuleMatch struct {
	Platform string `json:"platform,omitempty"`
}

// DevicePostureRuleInput represents the value to be checked against.
type DevicePostureRuleInput struct {
	ID               string `json:"id,omitempty"`
	Path             string `json:"path,omitempty"`
	Exists           bool   `json:"exists,omitempty"`
	Thumbprint       string `json:"thumbprint,omitempty"`
	Sha256           string `json:"sha256,omitempty"`
	Running          bool   `json:"running,omitempty"`
	RequireAll       bool   `json:"requireAll,omitempty"`
	Enabled          bool   `json:"enabled,omitempty"`
	Version          string `json:"version,omitempty"`
	Operator         string `json:"operator,omitempty"`
	Domain           string `json:"domain,omitempty"`
	ComplianceStatus string `json:"compliance_status,omitempty"`
	ConnectionID     string `json:"connection_id,omitempty"`
}

// DevicePostureRuleListResponse represents the response from the list
// device posture rules endpoint.
type DevicePostureRuleListResponse struct {
	Result []DevicePostureRule `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// DevicePostureRuleDetailResponse is the API response, containing a single
// device posture rule.
type DevicePostureRuleDetailResponse struct {
	Response
	Result DevicePostureRule `json:"result"`
}

// DevicePostureRules returns all device posture rules within an account.
//
// API reference: https://api.cloudflare.com/#device-posture-rules-list-device-posture-rules
func (api *API) DevicePostureRules(ctx context.Context, accountID string) ([]DevicePostureRule, ResultInfo, error) {
	uri := fmt.Sprintf("/%s/%s/devices/posture", AccountRouteRoot, accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []DevicePostureRule{}, ResultInfo{}, err
	}

	var devicePostureRuleListResponse DevicePostureRuleListResponse
	err = json.Unmarshal(res, &devicePostureRuleListResponse)
	if err != nil {
		return []DevicePostureRule{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}

	return devicePostureRuleListResponse.Result, devicePostureRuleListResponse.ResultInfo, nil
}

// DevicePostureRule returns a single device posture rule based on the rule ID.
//
// API reference: https://api.cloudflare.com/#device-posture-rules-device-posture-rules-details
func (api *API) DevicePostureRule(ctx context.Context, accountID, ruleID string) (DevicePostureRule, error) {
	uri := fmt.Sprintf(
		"/%s/%s/devices/posture/%s",
		AccountRouteRoot,
		accountID,
		ruleID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return DevicePostureRule{}, err
	}

	var devicePostureRuleDetailResponse DevicePostureRuleDetailResponse
	err = json.Unmarshal(res, &devicePostureRuleDetailResponse)
	if err != nil {
		return DevicePostureRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return devicePostureRuleDetailResponse.Result, nil
}

// CreateDevicePostureRule creates a new device posture rule.
//
// API reference: https://api.cloudflare.com/#device-posture-rules-create-device-posture-rule
func (api *API) CreateDevicePostureRule(ctx context.Context, accountID string, rule DevicePostureRule) (DevicePostureRule, error) {
	uri := fmt.Sprintf("/%s/%s/devices/posture", AccountRouteRoot, accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, rule)
	if err != nil {
		return DevicePostureRule{}, err
	}

	var devicePostureRuleDetailResponse DevicePostureRuleDetailResponse
	err = json.Unmarshal(res, &devicePostureRuleDetailResponse)
	if err != nil {
		return DevicePostureRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return devicePostureRuleDetailResponse.Result, nil
}

// UpdateDevicePostureRule updates an existing device posture rule.
//
// API reference: https://api.cloudflare.com/#device-posture-rules-update-device-posture-rule
func (api *API) UpdateDevicePostureRule(ctx context.Context, accountID string, rule DevicePostureRule) (DevicePostureRule, error) {
	if rule.ID == "" {
		return DevicePostureRule{}, errors.Errorf("device posture rule ID cannot be empty")
	}

	uri := fmt.Sprintf(
		"/%s/%s/devices/posture/%s",
		AccountRouteRoot,
		accountID,
		rule.ID,
	)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, rule)
	if err != nil {
		return DevicePostureRule{}, err
	}

	var devicePostureRuleDetailResponse DevicePostureRuleDetailResponse
	err = json.Unmarshal(res, &devicePostureRuleDetailResponse)
	if err != nil {
		return DevicePostureRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return devicePostureRuleDetailResponse.Result, nil
}

// DeleteDevicePostureRule deletes a device posture rule.
//
// API reference: https://api.cloudflare.com/#device-posture-rules-delete-device-posture-rule
func (api *API) DeleteDevicePostureRule(ctx context.Context, accountID, ruleID string) error {
	uri := fmt.Sprintf(
		"/%s/%s/devices/posture/%s",
		AccountRouteRoot,
		accountID,
		ruleID,
	)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
