package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

const (
	errSecondaryDNSInvalidAutoRefreshValue = "secondary DNS auto refresh value is invalid"
	errSecondaryDNSInvalidZoneName         = "secondary DNS zone name is invalid"
	errSecondaryDNSInvalidPrimaries        = "secondary DNS primaries value is invalid"
)

// SecondaryDNSZone contains the high level structure of a secondary DNS zone.
type SecondaryDNSZone struct {
	ID                 string    `json:"id,omitempty"`
	Name               string    `json:"name,omitempty"`
	Primaries          []string  `json:"primaries,omitempty"`
	AutoRefreshSeconds int       `json:"auto_refresh_seconds,omitempty"`
	SoaSerial          int       `json:"soa_serial,omitempty"`
	CreatedTime        time.Time `json:"created_time,omitempty"`
	CheckedTime        time.Time `json:"checked_time,omitempty"`
	ModifiedTime       time.Time `json:"modified_time,omitempty"`
}

// SecondaryDNSZoneDetailResponse is the API response for a single secondary
// DNS zone.
type SecondaryDNSZoneDetailResponse struct {
	Response
	Result SecondaryDNSZone `json:"result"`
}

// SecondaryDNSZoneAXFRResponse is the API response for a single secondary
// DNS AXFR response.
type SecondaryDNSZoneAXFRResponse struct {
	Response
	Result string `json:"result"`
}

// GetSecondaryDNSZone returns the secondary DNS zone configuration for a
// single zone.
//
// API reference: https://api.cloudflare.com/#secondary-dns-secondary-zone-configuration-details
func (api *API) GetSecondaryDNSZone(ctx context.Context, zoneID string) (SecondaryDNSZone, error) {
	uri := fmt.Sprintf("/zones/%s/secondary_dns", zoneID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return SecondaryDNSZone{}, err
	}

	var r SecondaryDNSZoneDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return SecondaryDNSZone{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// CreateSecondaryDNSZone creates a secondary DNS zone.
//
// API reference: https://api.cloudflare.com/#secondary-dns-create-secondary-zone-configuration
func (api *API) CreateSecondaryDNSZone(ctx context.Context, zoneID string, zone SecondaryDNSZone) (SecondaryDNSZone, error) {
	if err := validateRequiredSecondaryDNSZoneValues(zone); err != nil {
		return SecondaryDNSZone{}, err
	}

	uri := fmt.Sprintf("/zones/%s/secondary_dns", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri,
		SecondaryDNSZone{
			Name:               zone.Name,
			AutoRefreshSeconds: zone.AutoRefreshSeconds,
			Primaries:          zone.Primaries,
		},
	)

	if err != nil {
		return SecondaryDNSZone{}, err
	}

	result := SecondaryDNSZoneDetailResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return SecondaryDNSZone{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// UpdateSecondaryDNSZone updates an existing secondary DNS zone.
//
// API reference: https://api.cloudflare.com/#secondary-dns-update-secondary-zone-configuration
func (api *API) UpdateSecondaryDNSZone(ctx context.Context, zoneID string, zone SecondaryDNSZone) (SecondaryDNSZone, error) {
	if err := validateRequiredSecondaryDNSZoneValues(zone); err != nil {
		return SecondaryDNSZone{}, err
	}

	uri := fmt.Sprintf("/zones/%s/secondary_dns", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri,
		SecondaryDNSZone{
			Name:               zone.Name,
			AutoRefreshSeconds: zone.AutoRefreshSeconds,
			Primaries:          zone.Primaries,
		},
	)

	if err != nil {
		return SecondaryDNSZone{}, err
	}

	result := SecondaryDNSZoneDetailResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return SecondaryDNSZone{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// DeleteSecondaryDNSZone deletes a secondary DNS zone.
//
// API reference: https://api.cloudflare.com/#secondary-dns-delete-secondary-zone-configuration
func (api *API) DeleteSecondaryDNSZone(ctx context.Context, zoneID string) error {
	uri := fmt.Sprintf("/zones/%s/secondary_dns", zoneID)
	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)

	if err != nil {
		return err
	}

	return nil
}

// ForceSecondaryDNSZoneAXFR requests an immediate AXFR request.
//
// API reference: https://api.cloudflare.com/#secondary-dns-update-secondary-zone-configuration
func (api *API) ForceSecondaryDNSZoneAXFR(ctx context.Context, zoneID string) error {
	uri := fmt.Sprintf("/zones/%s/secondary_dns/force_axfr", zoneID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, nil)

	if err != nil {
		return err
	}

	result := SecondaryDNSZoneAXFRResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return errors.Wrap(err, errUnmarshalError)
	}

	return nil
}

// validateRequiredSecondaryDNSZoneValues ensures that the payload matches the
// API requirements for required fields.
func validateRequiredSecondaryDNSZoneValues(zone SecondaryDNSZone) error {
	if zone.Name == "" {
		return errors.New(errSecondaryDNSInvalidZoneName)
	}

	if zone.AutoRefreshSeconds == 0 || zone.AutoRefreshSeconds < 0 {
		return errors.New(errSecondaryDNSInvalidAutoRefreshValue)
	}

	if len(zone.Primaries) == 0 {
		return errors.New(errSecondaryDNSInvalidPrimaries)
	}

	return nil
}
