package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

const (
	errSecondaryDNSInvalidPrimaryID   = "secondary DNS primary ID is required"
	errSecondaryDNSInvalidPrimaryIP   = "secondary DNS primary IP invalid"
	errSecondaryDNSInvalidPrimaryPort = "secondary DNS primary port invalid"
)

// SecondaryDNSPrimary is the representation of the DNS Primary.
type SecondaryDNSPrimary struct {
	ID         string `json:"id,omitempty"`
	IP         string `json:"ip"`
	Port       int    `json:"port"`
	IxfrEnable bool   `json:"ixfr_enable"`
	TsigID     string `json:"tsig_id"`
	Name       string `json:"name"`
}

// SecondaryDNSPrimaryDetailResponse is the API representation of a single
// secondary DNS primary response.
type SecondaryDNSPrimaryDetailResponse struct {
	Response
	Result SecondaryDNSPrimary `json:"result"`
}

// SecondaryDNSPrimaryListResponse is the API representation of all secondary DNS
// primaries.
type SecondaryDNSPrimaryListResponse struct {
	Response
	Result []SecondaryDNSPrimary `json:"result"`
}

// GetSecondaryDNSPrimary returns a single secondary DNS primary.
//
// API reference: https://api.cloudflare.com/#secondary-dns-primary--primary-details
func (api *API) GetSecondaryDNSPrimary(ctx context.Context, accountID, primaryID string) (SecondaryDNSPrimary, error) {
	uri := fmt.Sprintf("/accounts/%s/secondary_dns/primaries/%s", accountID, primaryID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return SecondaryDNSPrimary{}, err
	}

	var r SecondaryDNSPrimaryDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return SecondaryDNSPrimary{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListSecondaryDNSPrimaries returns all secondary DNS primaries for an account.
//
// API reference: https://api.cloudflare.com/#secondary-dns-primary--list-primaries
func (api *API) ListSecondaryDNSPrimaries(ctx context.Context, accountID string) ([]SecondaryDNSPrimary, error) {
	uri := fmt.Sprintf("/accounts/%s/secondary_dns/primaries", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []SecondaryDNSPrimary{}, err
	}

	var r SecondaryDNSPrimaryListResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []SecondaryDNSPrimary{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// CreateSecondaryDNSPrimary creates a secondary DNS primary.
//
// API reference: https://api.cloudflare.com/#secondary-dns-primary--create-primary
func (api *API) CreateSecondaryDNSPrimary(ctx context.Context, accountID string, primary SecondaryDNSPrimary) (SecondaryDNSPrimary, error) {
	if err := validateRequiredSecondaryDNSPrimaries(primary); err != nil {
		return SecondaryDNSPrimary{}, err
	}

	uri := fmt.Sprintf("/accounts/%s/secondary_dns/primaries", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, SecondaryDNSPrimary{
		IP:         primary.IP,
		Port:       primary.Port,
		IxfrEnable: primary.IxfrEnable,
		TsigID:     primary.TsigID,
		Name:       primary.Name,
	})
	if err != nil {
		return SecondaryDNSPrimary{}, err
	}

	var r SecondaryDNSPrimaryDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return SecondaryDNSPrimary{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// UpdateSecondaryDNSPrimary creates a secondary DNS primary.
//
// API reference: https://api.cloudflare.com/#secondary-dns-primary--update-primary
func (api *API) UpdateSecondaryDNSPrimary(ctx context.Context, accountID string, primary SecondaryDNSPrimary) (SecondaryDNSPrimary, error) {
	if primary.ID == "" {
		return SecondaryDNSPrimary{}, errors.New(errSecondaryDNSInvalidPrimaryID)
	}

	if err := validateRequiredSecondaryDNSPrimaries(primary); err != nil {
		return SecondaryDNSPrimary{}, err
	}

	uri := fmt.Sprintf("/accounts/%s/secondary_dns/primaries/%s", accountID, primary.ID)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, SecondaryDNSPrimary{
		IP:         primary.IP,
		Port:       primary.Port,
		IxfrEnable: primary.IxfrEnable,
		TsigID:     primary.TsigID,
		Name:       primary.Name,
	})
	if err != nil {
		return SecondaryDNSPrimary{}, err
	}

	var r SecondaryDNSPrimaryDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return SecondaryDNSPrimary{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// DeleteSecondaryDNSPrimary deletes a secondary DNS primary.
//
// API reference: https://api.cloudflare.com/#secondary-dns-primary--delete-primary
func (api *API) DeleteSecondaryDNSPrimary(ctx context.Context, accountID, primaryID string) error {
	uri := fmt.Sprintf("/zones/%s/secondary_dns/primaries/%s", accountID, primaryID)
	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)

	if err != nil {
		return err
	}

	return nil
}

func validateRequiredSecondaryDNSPrimaries(p SecondaryDNSPrimary) error {
	if p.IP == "" {
		return errors.New(errSecondaryDNSInvalidPrimaryIP)
	}

	if p.Port == 0 {
		return errors.New(errSecondaryDNSInvalidPrimaryPort)
	}

	return nil
}
