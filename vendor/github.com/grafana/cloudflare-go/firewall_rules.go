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

// FirewallRule is the struct of the firewall rule.
type FirewallRule struct {
	ID          string      `json:"id,omitempty"`
	Paused      bool        `json:"paused"`
	Description string      `json:"description"`
	Action      string      `json:"action"`
	Priority    interface{} `json:"priority"`
	Filter      Filter      `json:"filter"`
	Products    []string    `json:"products,omitempty"`
	CreatedOn   time.Time   `json:"created_on,omitempty"`
	ModifiedOn  time.Time   `json:"modified_on,omitempty"`
}

// FirewallRulesDetailResponse is the API response for the firewall
// rules.
type FirewallRulesDetailResponse struct {
	Result     []FirewallRule `json:"result"`
	ResultInfo `json:"result_info"`
	Response
}

// FirewallRuleResponse is the API response that is returned
// for requesting a single firewall rule on a zone.
type FirewallRuleResponse struct {
	Result     FirewallRule `json:"result"`
	ResultInfo `json:"result_info"`
	Response
}

// FirewallRules returns all firewall rules.
//
// API reference: https://developers.cloudflare.com/firewall/api/cf-firewall-rules/get/#get-all-rules
func (api *API) FirewallRules(ctx context.Context, zoneID string, pageOpts PaginationOptions) ([]FirewallRule, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/rules", zoneID)
	v := url.Values{}

	if pageOpts.PerPage > 0 {
		v.Set("per_page", strconv.Itoa(pageOpts.PerPage))
	}

	if pageOpts.Page > 0 {
		v.Set("page", strconv.Itoa(pageOpts.Page))
	}

	if len(v) > 0 {
		uri = fmt.Sprintf("%s?%s", uri, v.Encode())
	}

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []FirewallRule{}, err
	}

	var firewallDetailResponse FirewallRulesDetailResponse
	err = json.Unmarshal(res, &firewallDetailResponse)
	if err != nil {
		return []FirewallRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return firewallDetailResponse.Result, nil
}

// FirewallRule returns a single firewall rule based on the ID.
//
// API reference: https://developers.cloudflare.com/firewall/api/cf-firewall-rules/get/#get-by-rule-id
func (api *API) FirewallRule(ctx context.Context, zoneID, firewallRuleID string) (FirewallRule, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/rules/%s", zoneID, firewallRuleID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return FirewallRule{}, err
	}

	var firewallRuleResponse FirewallRuleResponse
	err = json.Unmarshal(res, &firewallRuleResponse)
	if err != nil {
		return FirewallRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return firewallRuleResponse.Result, nil
}

// CreateFirewallRules creates new firewall rules.
//
// API reference: https://developers.cloudflare.com/firewall/api/cf-firewall-rules/post/
func (api *API) CreateFirewallRules(ctx context.Context, zoneID string, firewallRules []FirewallRule) ([]FirewallRule, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/rules", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, firewallRules)
	if err != nil {
		return []FirewallRule{}, err
	}

	var firewallRulesDetailResponse FirewallRulesDetailResponse
	err = json.Unmarshal(res, &firewallRulesDetailResponse)
	if err != nil {
		return []FirewallRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return firewallRulesDetailResponse.Result, nil
}

// UpdateFirewallRule updates a single firewall rule.
//
// API reference: https://developers.cloudflare.com/firewall/api/cf-firewall-rules/put/#update-a-single-rule
func (api *API) UpdateFirewallRule(ctx context.Context, zoneID string, firewallRule FirewallRule) (FirewallRule, error) {
	if firewallRule.ID == "" {
		return FirewallRule{}, errors.Errorf("firewall rule ID cannot be empty")
	}

	uri := fmt.Sprintf("/zones/%s/firewall/rules/%s", zoneID, firewallRule.ID)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, firewallRule)
	if err != nil {
		return FirewallRule{}, err
	}

	var firewallRuleResponse FirewallRuleResponse
	err = json.Unmarshal(res, &firewallRuleResponse)
	if err != nil {
		return FirewallRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return firewallRuleResponse.Result, nil
}

// UpdateFirewallRules updates a single firewall rule.
//
// API reference: https://developers.cloudflare.com/firewall/api/cf-firewall-rules/put/#update-multiple-rules
func (api *API) UpdateFirewallRules(ctx context.Context, zoneID string, firewallRules []FirewallRule) ([]FirewallRule, error) {
	for _, firewallRule := range firewallRules {
		if firewallRule.ID == "" {
			return []FirewallRule{}, errors.Errorf("firewall ID cannot be empty")
		}
	}

	uri := fmt.Sprintf("/zones/%s/firewall/rules", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, firewallRules)
	if err != nil {
		return []FirewallRule{}, err
	}

	var firewallRulesDetailResponse FirewallRulesDetailResponse
	err = json.Unmarshal(res, &firewallRulesDetailResponse)
	if err != nil {
		return []FirewallRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return firewallRulesDetailResponse.Result, nil
}

// DeleteFirewallRule deletes a single firewall rule.
//
// API reference: https://developers.cloudflare.com/firewall/api/cf-firewall-rules/delete/#delete-a-single-rule
func (api *API) DeleteFirewallRule(ctx context.Context, zoneID, firewallRuleID string) error {
	if firewallRuleID == "" {
		return errors.Errorf("firewall rule ID cannot be empty")
	}

	uri := fmt.Sprintf("/zones/%s/firewall/rules/%s", zoneID, firewallRuleID)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}

// DeleteFirewallRules deletes multiple firewall rules at once.
//
// API reference: https://developers.cloudflare.com/firewall/api/cf-firewall-rules/delete/#delete-multiple-rules
func (api *API) DeleteFirewallRules(ctx context.Context, zoneID string, firewallRuleIDs []string) error {
	v := url.Values{}

	for _, ruleID := range firewallRuleIDs {
		v.Add("id", ruleID)
	}

	uri := fmt.Sprintf("/zones/%s/firewall/rules?%s", zoneID, v.Encode())

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
