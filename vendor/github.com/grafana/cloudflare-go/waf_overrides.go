package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

// WAFOverridesResponse represents the response form the WAF overrides endpoint.
type WAFOverridesResponse struct {
	Response
	Result     []WAFOverride `json:"result"`
	ResultInfo ResultInfo    `json:"result_info"`
}

// WAFOverrideResponse represents the response form the WAF override endpoint.
type WAFOverrideResponse struct {
	Response
	Result     WAFOverride `json:"result"`
	ResultInfo ResultInfo  `json:"result_info"`
}

// WAFOverride represents a WAF override.
type WAFOverride struct {
	ID            string            `json:"id,omitempty"`
	Description   string            `json:"description"`
	URLs          []string          `json:"urls"`
	Priority      int               `json:"priority"`
	Groups        map[string]string `json:"groups"`
	RewriteAction map[string]string `json:"rewrite_action"`
	Rules         map[string]string `json:"rules"`
	Paused        bool              `json:"paused"`
}

// ListWAFOverrides returns a slice of the WAF overrides.
//
// API Reference: https://api.cloudflare.com/#waf-overrides-list-uri-controlled-waf-configurations
func (api *API) ListWAFOverrides(ctx context.Context, zoneID string) ([]WAFOverride, error) {
	var overrides []WAFOverride
	var res []byte
	var err error

	uri := fmt.Sprintf("/zones/%s/firewall/waf/overrides", zoneID)
	res, err = api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []WAFOverride{}, err
	}

	var r WAFOverridesResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []WAFOverride{}, errors.Wrap(err, errUnmarshalError)
	}

	if !r.Success {
		// TODO: Provide an actual error message instead of always returning nil
		return []WAFOverride{}, err
	}

	for ri := range r.Result {
		overrides = append(overrides, r.Result[ri])
	}
	return overrides, nil
}

// WAFOverride returns a WAF override from the given override ID.
//
// API Reference: https://api.cloudflare.com/#waf-overrides-uri-controlled-waf-configuration-details
func (api *API) WAFOverride(ctx context.Context, zoneID, overrideID string) (WAFOverride, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/waf/overrides/%s", zoneID, overrideID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return WAFOverride{}, err
	}

	var r WAFOverrideResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WAFOverride{}, errors.Wrap(err, errUnmarshalError)
	}

	return r.Result, nil
}

// CreateWAFOverride creates a new WAF override.
//
// API reference: https://api.cloudflare.com/#waf-overrides-create-a-uri-controlled-waf-configuration
func (api *API) CreateWAFOverride(ctx context.Context, zoneID string, override WAFOverride) (WAFOverride, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/waf/overrides", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, override)
	if err != nil {
		return WAFOverride{}, err
	}
	var r WAFOverrideResponse
	if err := json.Unmarshal(res, &r); err != nil {
		return WAFOverride{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UpdateWAFOverride updates an existing WAF override.
//
// API reference: https://api.cloudflare.com/#waf-overrides-update-uri-controlled-waf-configuration
func (api *API) UpdateWAFOverride(ctx context.Context, zoneID, overrideID string, override WAFOverride) (WAFOverride, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/waf/overrides/%s", zoneID, overrideID)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, override)
	if err != nil {
		return WAFOverride{}, err
	}

	var r WAFOverrideResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return WAFOverride{}, errors.Wrap(err, errUnmarshalError)
	}

	return r.Result, nil
}

// DeleteWAFOverride deletes a WAF override for a zone.
//
// API reference: https://api.cloudflare.com/#waf-overrides-delete-lockdown-rule
func (api *API) DeleteWAFOverride(ctx context.Context, zoneID, overrideID string) error {
	uri := fmt.Sprintf("/zones/%s/firewall/waf/overrides/%s", zoneID, overrideID)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}
	var r WAFOverrideResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}
	return nil
}
