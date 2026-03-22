package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// NotificationMechanismData holds a single public facing mechanism data
// integation.
type NotificationMechanismData struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// NotificationMechanismIntegrations is a list of all the integrations of a
// certain mechanism type e.g. all email integrations
type NotificationMechanismIntegrations []NotificationMechanismData

// NotificationPolicy represents the notification policy created along with
// the destinations.
type NotificationPolicy struct {
	ID          string                                       `json:"id"`
	Name        string                                       `json:"name"`
	Description string                                       `json:"description"`
	Enabled     bool                                         `json:"enabled"`
	AlertType   string                                       `json:"alert_type"`
	Mechanisms  map[string]NotificationMechanismIntegrations `json:"mechanisms"`
	Created     time.Time                                    `json:"created"`
	Modified    time.Time                                    `json:"modified"`
	Conditions  map[string]interface{}                       `json:"conditions"`
	Filters     map[string][]string                          `json:"filters"`
}

// NotificationPoliciesResponse holds the response for listing all
// notification policies for an account.
type NotificationPoliciesResponse struct {
	Response
	ResultInfo
	Result []NotificationPolicy
}

// NotificationPolicyResponse holds the response type when a single policy
// is retrieved.
type NotificationPolicyResponse struct {
	Response
	Result NotificationPolicy
}

// NotificationWebhookIntegration describes the webhook information along
// with its status.
type NotificationWebhookIntegration struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type"`
	URL         string     `json:"url"`
	CreatedAt   time.Time  `json:"created_at"`
	LastSuccess *time.Time `json:"last_success"`
	LastFailure *time.Time `json:"last_failure"`
}

// NotificationWebhookResponse describes a single webhook retrieved.
type NotificationWebhookResponse struct {
	Response
	ResultInfo
	Result NotificationWebhookIntegration
}

// NotificationWebhooksResponse describes a list of webhooks retrieved.
type NotificationWebhooksResponse struct {
	Response
	ResultInfo
	Result []NotificationWebhookIntegration
}

// NotificationUpsertWebhooks describes a valid webhook request.
type NotificationUpsertWebhooks struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

// NotificationPagerDutyResource describes a PagerDuty integration.
type NotificationPagerDutyResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// NotificationPagerDutyResponse describes the PagerDuty integration
// retrieved.
type NotificationPagerDutyResponse struct {
	Response
	ResultInfo
	Result NotificationPagerDutyResource
}

// NotificationResource describes the id of an inserted/updated/deleted
// resource.
type NotificationResource struct {
	ID string
}

// SaveResponse is returned when a resource is inserted/updated/deleted.
type SaveResponse struct {
	Response
	Result NotificationResource
}

// NotificationMechanismMetaData represents the state of the delivery
// mechanism.
type NotificationMechanismMetaData struct {
	Eligible bool   `json:"eligible"`
	Ready    bool   `json:"ready"`
	Type     string `json:"type"`
}

// NotificationMechanisms are the different possible delivery mechanisms.
type NotificationMechanisms struct {
	Email     NotificationMechanismMetaData `json:"email"`
	PagerDuty NotificationMechanismMetaData `json:"pagerduty"`
	Webhooks  NotificationMechanismMetaData `json:"webhooks,omitempty"`
}

// NotificationEligibilityResponse describes the eligible mechanisms that
// can be configured for a notification.
type NotificationEligibilityResponse struct {
	Response
	Result NotificationMechanisms
}

// NotificationsGroupedByProduct are grouped by products.
type NotificationsGroupedByProduct map[string][]NotificationAlertWithDescription

// NotificationAlertWithDescription represents the alert/notification
// available.
type NotificationAlertWithDescription struct {
	DisplayName string `json:"display_name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

// NotificationAvailableAlertsResponse describes the available
// alerts/notifications grouped by products.
type NotificationAvailableAlertsResponse struct {
	Response
	Result NotificationsGroupedByProduct
}

// NotificationHistory describes the history
// of notifications sent for an account.
type NotificationHistory struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	AlertBody     string    `json:"alert_body"`
	AlertType     string    `json:"alert_type"`
	Mechanism     string    `json:"mechanism"`
	MechanismType string    `json:"mechanism_type"`
	Sent          time.Time `json:"sent"`
}

// NotificationHistoryResponse describes the notification history
// response for an account for a specific time period.
type NotificationHistoryResponse struct {
	Response
	ResultInfo `json:"result_info"`
	Result     []NotificationHistory
}

// ListNotificationPolicies will return the notification policies
// created by a user for a specific account.
//
// API Reference: https://api.cloudflare.com/#notification-policies-properties
func (api *API) ListNotificationPolicies(ctx context.Context, accountID string) (NotificationPoliciesResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/policies", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return NotificationPoliciesResponse{}, err
	}
	var r NotificationPoliciesResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}
	return r, nil
}

// GetNotificationPolicy returns a specific created by a user, given the account
// id and the policy id.
//
// API Reference: https://api.cloudflare.com/#notification-policies-properties
func (api *API) GetNotificationPolicy(ctx context.Context, accountID, policyID string) (NotificationPolicyResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/policies/%s", accountID, policyID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return NotificationPolicyResponse{}, err
	}
	var r NotificationPolicyResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}
	return r, nil
}

// CreateNotificationPolicy creates a notification policy for an account.
//
// API Reference: https://api.cloudflare.com/#notification-policies-create-notification-policy
func (api *API) CreateNotificationPolicy(ctx context.Context, accountID string, policy NotificationPolicy) (SaveResponse, error) {

	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/policies", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, baseURL, policy)
	if err != nil {
		return SaveResponse{}, err
	}
	return unmarshalNotificationSaveResponse(res)
}

// UpdateNotificationPolicy updates a notification policy, given the
// account id and the policy id and returns the policy id.
//
// API Reference: https://api.cloudflare.com/#notification-policies-update-notification-policy
func (api *API) UpdateNotificationPolicy(ctx context.Context, accountID string, policy *NotificationPolicy) (SaveResponse, error) {
	if policy == nil {
		return SaveResponse{}, fmt.Errorf("policy cannot be nil")
	}
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/policies/%s", accountID, policy.ID)

	res, err := api.makeRequestContext(ctx, http.MethodPut, baseURL, policy)
	if err != nil {
		return SaveResponse{}, err
	}
	return unmarshalNotificationSaveResponse(res)
}

// DeleteNotificationPolicy deletes a notification policy for an account.
//
// API Reference: https://api.cloudflare.com/#notification-policies-delete-notification-policy
func (api *API) DeleteNotificationPolicy(ctx context.Context, accountID, policyID string) (SaveResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/policies/%s", accountID, policyID)

	res, err := api.makeRequestContext(ctx, http.MethodDelete, baseURL, nil)
	if err != nil {
		return SaveResponse{}, err
	}
	return unmarshalNotificationSaveResponse(res)
}

// ListNotificationWebhooks will return the webhook destinations configured
// for an account.
//
// API Reference: https://api.cloudflare.com/#notification-webhooks-list-webhooks
func (api *API) ListNotificationWebhooks(ctx context.Context, accountID string) (NotificationWebhooksResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/destinations/webhooks", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return NotificationWebhooksResponse{}, err
	}
	var r NotificationWebhooksResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}
	return r, nil

}

// CreateNotificationWebhooks will help connect a webhooks destination.
// A test message will be sent to the webhooks endpoint during creation.
// If added successfully, the webhooks can be setup as a destination mechanism
// while creating policies.
//
// Notifications will be posted to this URL.
//
// API Reference: https://api.cloudflare.com/#notification-webhooks-create-webhook
func (api *API) CreateNotificationWebhooks(ctx context.Context, accountID string, webhooks *NotificationUpsertWebhooks) (SaveResponse, error) {
	if webhooks == nil {
		return SaveResponse{}, fmt.Errorf("webhooks cannot be nil")
	}
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/destinations/webhooks", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, baseURL, webhooks)
	if err != nil {
		return SaveResponse{}, err
	}

	return unmarshalNotificationSaveResponse(res)
}

// GetNotificationWebhooks will return a specific webhook destination,
// given the account and webhooks ids.
//
// API Reference: https://api.cloudflare.com/#notification-webhooks-get-webhook
func (api *API) GetNotificationWebhooks(ctx context.Context, accountID, webhookID string) (NotificationWebhookResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/destinations/webhooks/%s", accountID, webhookID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return NotificationWebhookResponse{}, err
	}
	var r NotificationWebhookResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}
	return r, nil
}

// UpdateNotificationWebhooks will update a particular webhook's name,
// given the account and webhooks ids.
//
// The webhook url and secret cannot be updated.
//
// API Reference: https://api.cloudflare.com/#notification-webhooks-update-webhook
func (api *API) UpdateNotificationWebhooks(ctx context.Context, accountID, webhookID string, webhooks *NotificationUpsertWebhooks) (SaveResponse, error) {
	if webhooks == nil {
		return SaveResponse{}, fmt.Errorf("webhooks cannot be nil")
	}
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/destinations/webhooks/%s", accountID, webhookID)

	res, err := api.makeRequestContext(ctx, http.MethodPut, baseURL, webhooks)
	if err != nil {
		return SaveResponse{}, err
	}

	return unmarshalNotificationSaveResponse(res)
}

// DeleteNotificationWebhooks will delete a webhook, given the account and
// webhooks ids. Deleting the webhooks will remove it from any connected
// notification policies.
//
// API Reference: https://api.cloudflare.com/#notification-webhooks-delete-webhook
func (api *API) DeleteNotificationWebhooks(ctx context.Context, accountID, webhookID string) (SaveResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/destinations/webhooks/%s", accountID, webhookID)

	res, err := api.makeRequestContext(ctx, http.MethodDelete, baseURL, nil)
	if err != nil {
		return SaveResponse{}, err
	}

	return unmarshalNotificationSaveResponse(res)
}

// ListPagerDutyNotificationDestinations will return the pagerduty
// destinations configured for an account.
//
// API Reference: https://api.cloudflare.com/#notification-destinations-with-pagerduty-list-pagerduty-services
func (api *API) ListPagerDutyNotificationDestinations(ctx context.Context, accountID string) (NotificationPagerDutyResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/destinations/pagerduty", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return NotificationPagerDutyResponse{}, err
	}
	var r NotificationPagerDutyResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}
	return r, nil
}

// GetEligibleNotificationDestinations will return the types of
// destinations an account is eligible to configure.
//
// API Reference: https://api.cloudflare.com/#notification-mechanism-eligibility-properties
func (api *API) GetEligibleNotificationDestinations(ctx context.Context, accountID string) (NotificationEligibilityResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/destinations/eligible", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return NotificationEligibilityResponse{}, err
	}
	var r NotificationEligibilityResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}
	return r, nil
}

// GetAvailableNotificationTypes will return the alert types available for
// a given account.
//
// API Reference: https://api.cloudflare.com/#notification-mechanism-eligibility-properties
func (api *API) GetAvailableNotificationTypes(ctx context.Context, accountID string) (NotificationAvailableAlertsResponse, error) {
	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/available_alerts", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return NotificationAvailableAlertsResponse{}, err
	}
	var r NotificationAvailableAlertsResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}
	return r, nil
}

// ListNotificationHistory will return the history of alerts sent for
// a given account. The time period varies based on zone plan.
// Free, Biz, Pro = 30 days
// Ent = 90 days
//
// API Reference: https://api.cloudflare.com/#notification-history-list-history
func (api *API) ListNotificationHistory(ctx context.Context, accountID string, pageOpts PaginationOptions) ([]NotificationHistory, ResultInfo, error) {
	v := url.Values{}
	if pageOpts.PerPage > 0 {
		v.Set("per_page", strconv.Itoa(pageOpts.PerPage))
	}
	if pageOpts.Page > 0 {
		v.Set("page", strconv.Itoa(pageOpts.Page))
	}

	baseURL := fmt.Sprintf("/accounts/%s/alerting/v3/history", accountID)
	if len(v) > 0 {
		baseURL = fmt.Sprintf("%s?%s", baseURL, v.Encode())
	}

	res, err := api.makeRequestContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return []NotificationHistory{}, ResultInfo{}, err
	}
	var r NotificationHistoryResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []NotificationHistory{}, ResultInfo{}, err
	}
	return r.Result, r.ResultInfo, nil
}

// unmarshal will unmarshal bytes and return a SaveResponse
func unmarshalNotificationSaveResponse(res []byte) (SaveResponse, error) {
	var r SaveResponse
	err := json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}
	return r, nil
}
