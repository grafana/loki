package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pkg/errors"
)

// AccessRule represents a firewall access rule.
type AccessRule struct {
	ID            string                  `json:"id,omitempty"`
	Notes         string                  `json:"notes,omitempty"`
	AllowedModes  []string                `json:"allowed_modes,omitempty"`
	Mode          string                  `json:"mode,omitempty"`
	Configuration AccessRuleConfiguration `json:"configuration,omitempty"`
	Scope         AccessRuleScope         `json:"scope,omitempty"`
	CreatedOn     time.Time               `json:"created_on,omitempty"`
	ModifiedOn    time.Time               `json:"modified_on,omitempty"`
}

// AccessRuleConfiguration represents the configuration of a firewall
// access rule.
type AccessRuleConfiguration struct {
	Target string `json:"target,omitempty"`
	Value  string `json:"value,omitempty"`
}

// AccessRuleScope represents the scope of a firewall access rule.
type AccessRuleScope struct {
	ID    string `json:"id,omitempty"`
	Email string `json:"email,omitempty"`
	Name  string `json:"name,omitempty"`
	Type  string `json:"type,omitempty"`
}

// AccessRuleResponse represents the response from the firewall access
// rule endpoint.
type AccessRuleResponse struct {
	Result AccessRule `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// AccessRuleListResponse represents the response from the list access rules
// endpoint.
type AccessRuleListResponse struct {
	Result []AccessRule `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// ListUserAccessRules returns a slice of access rules for the logged-in user.
//
// This takes an AccessRule to allow filtering of the results returned.
//
// API reference: https://api.cloudflare.com/#user-level-firewall-access-rule-list-access-rules
func (api *API) ListUserAccessRules(ctx context.Context, accessRule AccessRule, page int) (*AccessRuleListResponse, error) {
	return api.listAccessRules(ctx, "/user", accessRule, page)
}

// CreateUserAccessRule creates a firewall access rule for the logged-in user.
//
// API reference: https://api.cloudflare.com/#user-level-firewall-access-rule-create-access-rule
func (api *API) CreateUserAccessRule(ctx context.Context, accessRule AccessRule) (*AccessRuleResponse, error) {
	return api.createAccessRule(ctx, "/user", accessRule)
}

// UserAccessRule returns the details of a user's account access rule.
//
// API reference: https://api.cloudflare.com/#user-level-firewall-access-rule-list-access-rules
func (api *API) UserAccessRule(ctx context.Context, accessRuleID string) (*AccessRuleResponse, error) {
	return api.retrieveAccessRule(ctx, "/user", accessRuleID)
}

// UpdateUserAccessRule updates a single access rule for the logged-in user &
// given access rule identifier.
//
// API reference: https://api.cloudflare.com/#user-level-firewall-access-rule-update-access-rule
func (api *API) UpdateUserAccessRule(ctx context.Context, accessRuleID string, accessRule AccessRule) (*AccessRuleResponse, error) {
	return api.updateAccessRule(ctx, "/user", accessRuleID, accessRule)
}

// DeleteUserAccessRule deletes a single access rule for the logged-in user and
// access rule identifiers.
//
// API reference: https://api.cloudflare.com/#user-level-firewall-access-rule-update-access-rule
func (api *API) DeleteUserAccessRule(ctx context.Context, accessRuleID string) (*AccessRuleResponse, error) {
	return api.deleteAccessRule(ctx, "/user", accessRuleID)
}

// ListZoneAccessRules returns a slice of access rules for the given zone
// identifier.
//
// This takes an AccessRule to allow filtering of the results returned.
//
// API reference: https://api.cloudflare.com/#firewall-access-rule-for-a-zone-list-access-rules
func (api *API) ListZoneAccessRules(ctx context.Context, zoneID string, accessRule AccessRule, page int) (*AccessRuleListResponse, error) {
	return api.listAccessRules(ctx, fmt.Sprintf("/zones/%s", zoneID), accessRule, page)
}

// CreateZoneAccessRule creates a firewall access rule for the given zone
// identifier.
//
// API reference: https://api.cloudflare.com/#firewall-access-rule-for-a-zone-create-access-rule
func (api *API) CreateZoneAccessRule(ctx context.Context, zoneID string, accessRule AccessRule) (*AccessRuleResponse, error) {
	return api.createAccessRule(ctx, fmt.Sprintf("/zones/%s", zoneID), accessRule)
}

// ZoneAccessRule returns the details of a zone's access rule.
//
// API reference: https://api.cloudflare.com/#firewall-access-rule-for-a-zone-list-access-rules
func (api *API) ZoneAccessRule(ctx context.Context, zoneID string, accessRuleID string) (*AccessRuleResponse, error) {
	return api.retrieveAccessRule(ctx, fmt.Sprintf("/zones/%s", zoneID), accessRuleID)
}

// UpdateZoneAccessRule updates a single access rule for the given zone &
// access rule identifiers.
//
// API reference: https://api.cloudflare.com/#firewall-access-rule-for-a-zone-update-access-rule
func (api *API) UpdateZoneAccessRule(ctx context.Context, zoneID, accessRuleID string, accessRule AccessRule) (*AccessRuleResponse, error) {
	return api.updateAccessRule(ctx, fmt.Sprintf("/zones/%s", zoneID), accessRuleID, accessRule)
}

// DeleteZoneAccessRule deletes a single access rule for the given zone and
// access rule identifiers.
//
// API reference: https://api.cloudflare.com/#firewall-access-rule-for-a-zone-delete-access-rule
func (api *API) DeleteZoneAccessRule(ctx context.Context, zoneID, accessRuleID string) (*AccessRuleResponse, error) {
	return api.deleteAccessRule(ctx, fmt.Sprintf("/zones/%s", zoneID), accessRuleID)
}

// ListAccountAccessRules returns a slice of access rules for the given
// account identifier.
//
// This takes an AccessRule to allow filtering of the results returned.
//
// API reference: https://api.cloudflare.com/#account-level-firewall-access-rule-list-access-rules
func (api *API) ListAccountAccessRules(ctx context.Context, accountID string, accessRule AccessRule, page int) (*AccessRuleListResponse, error) {
	return api.listAccessRules(ctx, fmt.Sprintf("/accounts/%s", accountID), accessRule, page)
}

// CreateAccountAccessRule creates a firewall access rule for the given
// account identifier.
//
// API reference: https://api.cloudflare.com/#account-level-firewall-access-rule-create-access-rule
func (api *API) CreateAccountAccessRule(ctx context.Context, accountID string, accessRule AccessRule) (*AccessRuleResponse, error) {
	return api.createAccessRule(ctx, fmt.Sprintf("/accounts/%s", accountID), accessRule)
}

// AccountAccessRule returns the details of an account's access rule.
//
// API reference: https://api.cloudflare.com/#account-level-firewall-access-rule-access-rule-details
func (api *API) AccountAccessRule(ctx context.Context, accountID string, accessRuleID string) (*AccessRuleResponse, error) {
	return api.retrieveAccessRule(ctx, fmt.Sprintf("/accounts/%s", accountID), accessRuleID)
}

// UpdateAccountAccessRule updates a single access rule for the given
// account & access rule identifiers.
//
// API reference: https://api.cloudflare.com/#account-level-firewall-access-rule-update-access-rule
func (api *API) UpdateAccountAccessRule(ctx context.Context, accountID, accessRuleID string, accessRule AccessRule) (*AccessRuleResponse, error) {
	return api.updateAccessRule(ctx, fmt.Sprintf("/accounts/%s", accountID), accessRuleID, accessRule)
}

// DeleteAccountAccessRule deletes a single access rule for the given
// account and access rule identifiers.
//
// API reference: https://api.cloudflare.com/#account-level-firewall-access-rule-delete-access-rule
func (api *API) DeleteAccountAccessRule(ctx context.Context, accountID, accessRuleID string) (*AccessRuleResponse, error) {
	return api.deleteAccessRule(ctx, fmt.Sprintf("/accounts/%s", accountID), accessRuleID)
}

func (api *API) listAccessRules(ctx context.Context, prefix string, accessRule AccessRule, page int) (*AccessRuleListResponse, error) {
	// Construct a query string
	v := url.Values{}
	if page <= 0 {
		page = 1
	}
	v.Set("page", strconv.Itoa(page))
	// Request as many rules as possible per page - API max is 100
	v.Set("per_page", "100")
	if accessRule.Notes != "" {
		v.Set("notes", accessRule.Notes)
	}
	if accessRule.Mode != "" {
		v.Set("mode", accessRule.Mode)
	}
	if accessRule.Scope.Type != "" {
		v.Set("scope_type", accessRule.Scope.Type)
	}
	if accessRule.Configuration.Value != "" {
		v.Set("configuration_value", accessRule.Configuration.Value)
	}
	if accessRule.Configuration.Target != "" {
		v.Set("configuration_target", accessRule.Configuration.Target)
	}
	v.Set("page", strconv.Itoa(page))

	uri := fmt.Sprintf("%s/firewall/access_rules/rules?%s", prefix, v.Encode())
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	response := &AccessRuleListResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return response, nil
}

func (api *API) createAccessRule(ctx context.Context, prefix string, accessRule AccessRule) (*AccessRuleResponse, error) {
	uri := fmt.Sprintf("%s/firewall/access_rules/rules", prefix)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, accessRule)
	if err != nil {
		return nil, err
	}

	response := &AccessRuleResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

func (api *API) retrieveAccessRule(ctx context.Context, prefix, accessRuleID string) (*AccessRuleResponse, error) {
	uri := fmt.Sprintf("%s/firewall/access_rules/rules/%s", prefix, accessRuleID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)

	if err != nil {
		return nil, err
	}

	response := &AccessRuleResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

func (api *API) updateAccessRule(ctx context.Context, prefix, accessRuleID string, accessRule AccessRule) (*AccessRuleResponse, error) {
	uri := fmt.Sprintf("%s/firewall/access_rules/rules/%s", prefix, accessRuleID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, accessRule)
	if err != nil {
		return nil, err
	}

	response := &AccessRuleResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}
	return response, nil
}

func (api *API) deleteAccessRule(ctx context.Context, prefix, accessRuleID string) (*AccessRuleResponse, error) {
	uri := fmt.Sprintf("%s/firewall/access_rules/rules/%s", prefix, accessRuleID)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return nil, err
	}

	response := &AccessRuleResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}
