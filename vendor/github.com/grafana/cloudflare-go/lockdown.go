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

// ZoneLockdown represents a Zone Lockdown rule. A rule only permits access to
// the provided URL pattern(s) from the given IP address(es) or subnet(s).
type ZoneLockdown struct {
	ID             string               `json:"id"`
	Description    string               `json:"description"`
	URLs           []string             `json:"urls"`
	Configurations []ZoneLockdownConfig `json:"configurations"`
	Paused         bool                 `json:"paused"`
	Priority       int                  `json:"priority,omitempty"`
	CreatedOn      *time.Time           `json:"created_on,omitempty"`
	ModifiedOn     *time.Time           `json:"modified_on,omitempty"`
}

// ZoneLockdownConfig represents a Zone Lockdown config, which comprises
// a Target ("ip" or "ip_range") and a Value (an IP address or IP+mask,
// respectively.)
type ZoneLockdownConfig struct {
	Target string `json:"target"`
	Value  string `json:"value"`
}

// ZoneLockdownResponse represents a response from the Zone Lockdown endpoint.
type ZoneLockdownResponse struct {
	Result ZoneLockdown `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// ZoneLockdownListResponse represents a response from the List Zone Lockdown
// endpoint.
type ZoneLockdownListResponse struct {
	Result []ZoneLockdown `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// CreateZoneLockdown creates a Zone ZoneLockdown rule for the given zone ID.
//
// API reference: https://api.cloudflare.com/#zone-ZoneLockdown-create-a-ZoneLockdown-rule
func (api *API) CreateZoneLockdown(ctx context.Context, zoneID string, ld ZoneLockdown) (*ZoneLockdownResponse, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/lockdowns", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, ld)
	if err != nil {
		return nil, err
	}

	response := &ZoneLockdownResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

// UpdateZoneLockdown updates a Zone ZoneLockdown rule (based on the ID) for the
// given zone ID.
//
// API reference: https://api.cloudflare.com/#zone-ZoneLockdown-update-ZoneLockdown-rule
func (api *API) UpdateZoneLockdown(ctx context.Context, zoneID string, id string, ld ZoneLockdown) (*ZoneLockdownResponse, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/lockdowns/%s", zoneID, id)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, ld)
	if err != nil {
		return nil, err
	}

	response := &ZoneLockdownResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

// DeleteZoneLockdown deletes a Zone ZoneLockdown rule (based on the ID) for the
// given zone ID.
//
// API reference: https://api.cloudflare.com/#zone-ZoneLockdown-delete-ZoneLockdown-rule
func (api *API) DeleteZoneLockdown(ctx context.Context, zoneID string, id string) (*ZoneLockdownResponse, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/lockdowns/%s", zoneID, id)
	res, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return nil, err
	}

	response := &ZoneLockdownResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

// ZoneLockdown retrieves a Zone ZoneLockdown rule (based on the ID) for the
// given zone ID.
//
// API reference: https://api.cloudflare.com/#zone-ZoneLockdown-ZoneLockdown-rule-details
func (api *API) ZoneLockdown(ctx context.Context, zoneID string, id string) (*ZoneLockdownResponse, error) {
	uri := fmt.Sprintf("/zones/%s/firewall/lockdowns/%s", zoneID, id)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	response := &ZoneLockdownResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}

// ListZoneLockdowns retrieves a list of Zone ZoneLockdown rules for a given
// zone ID by page number.
//
// API reference: https://api.cloudflare.com/#zone-ZoneLockdown-list-ZoneLockdown-rules
func (api *API) ListZoneLockdowns(ctx context.Context, zoneID string, page int) (*ZoneLockdownListResponse, error) {
	v := url.Values{}
	if page <= 0 {
		page = 1
	}

	v.Set("page", strconv.Itoa(page))
	v.Set("per_page", strconv.Itoa(100))

	uri := fmt.Sprintf("/zones/%s/firewall/lockdowns?%s", zoneID, v.Encode())
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	response := &ZoneLockdownListResponse{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return nil, errors.Wrap(err, errUnmarshalError)
	}

	return response, nil
}
