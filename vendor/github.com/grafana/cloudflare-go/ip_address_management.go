package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

// IPPrefix contains information about an IP prefix
type IPPrefix struct {
	ID                   string     `json:"id"`
	CreatedAt            *time.Time `json:"created_at"`
	ModifiedAt           *time.Time `json:"modified_at"`
	CIDR                 string     `json:"cidr"`
	AccountID            string     `json:"account_id"`
	Description          string     `json:"description"`
	Approved             string     `json:"approved"`
	OnDemandEnabled      bool       `json:"on_demand_enabled"`
	OnDemandLocked       bool       `json:"on_demand_locked"`
	Advertised           bool       `json:"advertised"`
	AdvertisedModifiedAt *time.Time `json:"advertised_modified_at"`
}

// AdvertisementStatus contains information about the BGP status of an IP prefix
type AdvertisementStatus struct {
	Advertised           bool       `json:"advertised"`
	AdvertisedModifiedAt *time.Time `json:"advertised_modified_at"`
}

// ListIPPrefixResponse contains a slice of IP prefixes
type ListIPPrefixResponse struct {
	Response
	Result []IPPrefix `json:"result"`
}

// GetIPPrefixResponse contains a specific IP prefix's API Response
type GetIPPrefixResponse struct {
	Response
	Result IPPrefix `json:"result"`
}

// GetAdvertisementStatusResponse contains an API Response for the BGP status of the IP Prefix
type GetAdvertisementStatusResponse struct {
	Response
	Result AdvertisementStatus `json:"result"`
}

// IPPrefixUpdateRequest contains information about prefix updates
type IPPrefixUpdateRequest struct {
	Description string `json:"description"`
}

// AdvertisementStatusUpdateRequest contains information about bgp status updates
type AdvertisementStatusUpdateRequest struct {
	Advertised bool `json:"advertised"`
}

// ListPrefixes lists all IP prefixes for a given account
//
// API reference: https://api.cloudflare.com/#ip-address-management-prefixes-list-prefixes
func (api *API) ListPrefixes(ctx context.Context) ([]IPPrefix, error) {
	uri := fmt.Sprintf("/accounts/%s/addressing/prefixes", api.AccountID)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []IPPrefix{}, err
	}

	result := ListIPPrefixResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return []IPPrefix{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// GetPrefix returns a specific IP prefix
//
// API reference: https://api.cloudflare.com/#ip-address-management-prefixes-prefix-details
func (api *API) GetPrefix(ctx context.Context, id string) (IPPrefix, error) {
	uri := fmt.Sprintf("/accounts/%s/addressing/prefixes/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return IPPrefix{}, err
	}

	result := GetIPPrefixResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPPrefix{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// UpdatePrefixDescription edits the description of the IP prefix
//
// API reference: https://api.cloudflare.com/#ip-address-management-prefixes-update-prefix-description
func (api *API) UpdatePrefixDescription(ctx context.Context, id string, description string) (IPPrefix, error) {
	uri := fmt.Sprintf("/accounts/%s/addressing/prefixes/%s", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, IPPrefixUpdateRequest{Description: description})
	if err != nil {
		return IPPrefix{}, err
	}

	result := GetIPPrefixResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return IPPrefix{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// GetAdvertisementStatus returns the BGP status of the IP prefix
//
// API reference: https://api.cloudflare.com/#ip-address-management-prefixes-update-prefix-description
func (api *API) GetAdvertisementStatus(ctx context.Context, id string) (AdvertisementStatus, error) {
	uri := fmt.Sprintf("/accounts/%s/addressing/prefixes/%s/bgp/status", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return AdvertisementStatus{}, err
	}

	result := GetAdvertisementStatusResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return AdvertisementStatus{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// UpdateAdvertisementStatus changes the BGP status of an IP prefix
//
// API reference: https://api.cloudflare.com/#ip-address-management-prefixes-update-prefix-description
func (api *API) UpdateAdvertisementStatus(ctx context.Context, id string, advertised bool) (AdvertisementStatus, error) {
	uri := fmt.Sprintf("/accounts/%s/addressing/prefixes/%s/bgp/status", api.AccountID, id)
	res, err := api.makeRequestContext(ctx, http.MethodPatch, uri, AdvertisementStatusUpdateRequest{Advertised: advertised})
	if err != nil {
		return AdvertisementStatus{}, err
	}

	result := GetAdvertisementStatusResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return AdvertisementStatus{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}
