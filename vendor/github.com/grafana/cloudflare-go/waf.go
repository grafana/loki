package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pkg/errors"
)

// WAFPackage represents a WAF package configuration.
type WAFPackage struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ZoneID        string `json:"zone_id"`
	DetectionMode string `json:"detection_mode"`
	Sensitivity   string `json:"sensitivity"`
	ActionMode    string `json:"action_mode"`
}

// WAFPackagesResponse represents the response from the WAF packages endpoint.
type WAFPackagesResponse struct {
	Response
	Result     []WAFPackage `json:"result"`
	ResultInfo ResultInfo   `json:"result_info"`
}

// WAFPackageResponse represents the response from the WAF package endpoint.
type WAFPackageResponse struct {
	Response
	Result     WAFPackage `json:"result"`
	ResultInfo ResultInfo `json:"result_info"`
}

// WAFPackageOptions represents options to edit a WAF package.
type WAFPackageOptions struct {
	Sensitivity string `json:"sensitivity,omitempty"`
	ActionMode  string `json:"action_mode,omitempty"`
}

// WAFGroup represents a WAF rule group.
type WAFGroup struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Description        string   `json:"description"`
	RulesCount         int      `json:"rules_count"`
	ModifiedRulesCount int      `json:"modified_rules_count"`
	PackageID          string   `json:"package_id"`
	Mode               string   `json:"mode"`
	AllowedModes       []string `json:"allowed_modes"`
}

// WAFGroupsResponse represents the response from the WAF groups endpoint.
type WAFGroupsResponse struct {
	Response
	Result     []WAFGroup `json:"result"`
	ResultInfo ResultInfo `json:"result_info"`
}

// WAFGroupResponse represents the response from the WAF group endpoint.
type WAFGroupResponse struct {
	Response
	Result     WAFGroup   `json:"result"`
	ResultInfo ResultInfo `json:"result_info"`
}

// WAFRule represents a WAF rule.
type WAFRule struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
	PackageID   string `json:"package_id"`
	Group       struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"group"`
	Mode         string   `json:"mode"`
	DefaultMode  string   `json:"default_mode"`
	AllowedModes []string `json:"allowed_modes"`
}

// WAFRulesResponse represents the response from the WAF rules endpoint.
type WAFRulesResponse struct {
	Response
	Result     []WAFRule  `json:"result"`
	ResultInfo ResultInfo `json:"result_info"`
}

// WAFRuleResponse represents the response from the WAF rule endpoint.
type WAFRuleResponse struct {
	Response
	Result     WAFRule    `json:"result"`
	ResultInfo ResultInfo `json:"result_info"`
}

// WAFRuleOptions is a subset of WAFRule, for editable options.
type WAFRuleOptions struct {
	Mode string `json:"mode"`
}

// ListWAFPackages returns a slice of the WAF packages for the given zone.
//
// API Reference: https://api.cloudflare.com/#waf-rule-packages-list-firewall-packages
func (api *API) ListWAFPackages(ctx context.Context, zoneID string) ([]WAFPackage, error) {
	// Construct a query string
	v := url.Values{}
	// Request as many WAF packages as possible per page - API max is 100
	v.Set("per_page", "100")

	var packages []WAFPackage
	var res []byte
	var err error
	page := 1

	// Loop over makeRequest until what we've fetched all records
	for {
		v.Set("page", strconv.Itoa(page))
		uri := fmt.Sprintf("/zones/%s/firewall/waf/packages?%s", zoneID, v.Encode())
		res, err = api.makeRequestContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return []WAFPackage{}, err
		}

		var p WAFPackagesResponse
		err = json.Unmarshal(res, &p)
		if err != nil {
			return []WAFPackage{}, errors.Wrap(err, errUnmarshalError)
		}

		if !p.Success {
			// TODO: Provide an actual error message instead of always returning nil
			return []WAFPackage{}, err
		}

		packages = append(packages, p.Result...)
		if p.ResultInfo.Page >= p.ResultInfo.TotalPages {
			break
		}

		// Loop around and fetch the next page
		page++
	}

	return packages, nil
}

// WAFPackage returns a WAF package for the given zone.
//
// API Reference: https://api.cloudflare.com/#waf-rule-packages-firewall-package-details
func (api *API) WAFPackage(ctx context.Context, zoneID, packageID string) (WAFPackage, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/waf/packages/%s", zoneID, packageID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return WAFPackage{}, err
	}

	var r WAFPackageResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WAFPackage{}, errors.Wrap(err, errUnmarshalError)
	}

	return r.Result, nil
}

// UpdateWAFPackage lets you update the a WAF Package.
//
// API Reference: https://api.cloudflare.com/#waf-rule-packages-edit-firewall-package
func (api *API) UpdateWAFPackage(ctx context.Context, zoneID, packageID string, opts WAFPackageOptions) (WAFPackage, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/waf/packages/%s", zoneID, packageID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, opts)
	if err != nil {
		return WAFPackage{}, err
	}

	var r WAFPackageResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WAFPackage{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListWAFGroups returns a slice of the WAF groups for the given WAF package.
//
// API Reference: https://api.cloudflare.com/#waf-rule-groups-list-rule-groups
func (api *API) ListWAFGroups(ctx context.Context, zoneID, packageID string) ([]WAFGroup, error) {
	// Construct a query string
	v := url.Values{}
	// Request as many WAF groups as possible per page - API max is 100
	v.Set("per_page", "100")

	var groups []WAFGroup
	var res []byte
	var err error
	page := 1

	// Loop over makeRequest until what we've fetched all records
	for {
		v.Set("page", strconv.Itoa(page))
		uri := fmt.Sprintf("/zones/%s/firewall/waf/packages/%s/groups?%s", zoneID, packageID, v.Encode())
		res, err = api.makeRequestContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return []WAFGroup{}, err
		}

		var r WAFGroupsResponse
		err = json.Unmarshal(res, &r)
		if err != nil {
			return []WAFGroup{}, errors.Wrap(err, errUnmarshalError)
		}

		if !r.Success {
			// TODO: Provide an actual error message instead of always returning nil
			return []WAFGroup{}, err
		}

		groups = append(groups, r.Result...)
		if r.ResultInfo.Page >= r.ResultInfo.TotalPages {
			break
		}

		// Loop around and fetch the next page
		page++
	}
	return groups, nil
}

// WAFGroup returns a WAF rule group from the given WAF package.
//
// API Reference: https://api.cloudflare.com/#waf-rule-groups-rule-group-details
func (api *API) WAFGroup(ctx context.Context, zoneID, packageID, groupID string) (WAFGroup, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/waf/packages/%s/groups/%s", zoneID, packageID, groupID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return WAFGroup{}, err
	}

	var r WAFGroupResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WAFGroup{}, errors.Wrap(err, errUnmarshalError)
	}

	return r.Result, nil
}

// UpdateWAFGroup lets you update the mode of a WAF Group.
//
// API Reference: https://api.cloudflare.com/#waf-rule-groups-edit-rule-group
func (api *API) UpdateWAFGroup(ctx context.Context, zoneID, packageID, groupID, mode string) (WAFGroup, error) {
	opts := WAFRuleOptions{Mode: mode}
	uri := fmt.Sprintf("/zones/%s/firewall/waf/packages/%s/groups/%s", zoneID, packageID, groupID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, opts)
	if err != nil {
		return WAFGroup{}, err
	}

	var r WAFGroupResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WAFGroup{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListWAFRules returns a slice of the WAF rules for the given WAF package.
//
// API Reference: https://api.cloudflare.com/#waf-rules-list-rules
func (api *API) ListWAFRules(ctx context.Context, zoneID, packageID string) ([]WAFRule, error) {
	// Construct a query string
	v := url.Values{}
	// Request as many WAF rules as possible per page - API max is 100
	v.Set("per_page", "100")

	var rules []WAFRule
	var res []byte
	var err error
	page := 1

	// Loop over makeRequest until what we've fetched all records
	for {
		v.Set("page", strconv.Itoa(page))
		uri := fmt.Sprintf("/zones/%s/firewall/waf/packages/%s/rules?%s", zoneID, packageID, v.Encode())
		res, err = api.makeRequestContext(ctx, http.MethodGet, uri, nil)
		if err != nil {
			return []WAFRule{}, err
		}

		var r WAFRulesResponse
		err = json.Unmarshal(res, &r)
		if err != nil {
			return []WAFRule{}, errors.Wrap(err, errUnmarshalError)
		}

		if !r.Success {
			// TODO: Provide an actual error message instead of always returning nil
			return []WAFRule{}, err
		}

		rules = append(rules, r.Result...)
		if r.ResultInfo.Page >= r.ResultInfo.TotalPages {
			break
		}

		// Loop around and fetch the next page
		page++
	}

	return rules, nil
}

// WAFRule returns a WAF rule from the given WAF package.
//
// API Reference: https://api.cloudflare.com/#waf-rules-rule-details
func (api *API) WAFRule(ctx context.Context, zoneID, packageID, ruleID string) (WAFRule, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/waf/packages/%s/rules/%s", zoneID, packageID, ruleID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return WAFRule{}, err
	}

	var r WAFRuleResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WAFRule{}, errors.Wrap(err, errUnmarshalError)
	}

	return r.Result, nil
}

// UpdateWAFRule lets you update the mode of a WAF Rule.
//
// API Reference: https://api.cloudflare.com/#waf-rules-edit-rule
func (api *API) UpdateWAFRule(ctx context.Context, zoneID, packageID, ruleID, mode string) (WAFRule, error) {
	opts := WAFRuleOptions{Mode: mode}
	uri := fmt.Sprintf("/zones/%s/firewall/waf/packages/%s/rules/%s", zoneID, packageID, ruleID)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, opts)
	if err != nil {
		return WAFRule{}, err
	}

	var r WAFRuleResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WAFRule{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}
