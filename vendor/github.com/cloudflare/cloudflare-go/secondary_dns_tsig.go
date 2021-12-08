package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

const (
	errSecondaryDNSTSIGMissingID = "secondary DNS TSIG ID is required"
)

// SecondaryDNSTSIG contains the structure for a secondary DNS TSIG.
type SecondaryDNSTSIG struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name"`
	Secret string `json:"secret"`
	Algo   string `json:"algo"`
}

// SecondaryDNSTSIGDetailResponse is the API response for a single secondary
// DNS TSIG.
type SecondaryDNSTSIGDetailResponse struct {
	Response
	Result SecondaryDNSTSIG `json:"result"`
}

// SecondaryDNSTSIGListResponse is the API response for all secondary DNS TSIGs.
type SecondaryDNSTSIGListResponse struct {
	Response
	Result []SecondaryDNSTSIG `json:"result"`
}

// GetSecondaryDNSTSIG gets a single account level TSIG for a secondary DNS
// configuration.
//
// API reference: https://api.cloudflare.com/#secondary-dns-tsig--tsig-details
func (api *API) GetSecondaryDNSTSIG(ctx context.Context, accountID, tsigID string) (SecondaryDNSTSIG, error) {
	uri := fmt.Sprintf("/accounts/%s/secondary_dns/tsigs/%s", accountID, tsigID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return SecondaryDNSTSIG{}, err
	}

	var r SecondaryDNSTSIGDetailResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return SecondaryDNSTSIG{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// ListSecondaryDNSTSIGs gets all account level TSIG for a secondary DNS
// configuration.
//
// API reference: https://api.cloudflare.com/#secondary-dns-tsig--list-tsigs
func (api *API) ListSecondaryDNSTSIGs(ctx context.Context, accountID string) ([]SecondaryDNSTSIG, error) {
	uri := fmt.Sprintf("/accounts/%s/secondary_dns/tsigs", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []SecondaryDNSTSIG{}, err
	}

	var r SecondaryDNSTSIGListResponse
	err = json.Unmarshal(res, &r)
	if err != nil {
		return []SecondaryDNSTSIG{}, errors.Wrap(err, errUnmarshalError)
	}
	return r.Result, nil
}

// CreateSecondaryDNSTSIG creates a secondary DNS TSIG at the account level.
//
// API reference: https://api.cloudflare.com/#secondary-dns-tsig--create-tsig
func (api *API) CreateSecondaryDNSTSIG(ctx context.Context, accountID string, tsig SecondaryDNSTSIG) (SecondaryDNSTSIG, error) {
	uri := fmt.Sprintf("/accounts/%s/secondary_dns/tsigs", accountID)
	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, tsig)

	if err != nil {
		return SecondaryDNSTSIG{}, err
	}

	result := SecondaryDNSTSIGDetailResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return SecondaryDNSTSIG{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// UpdateSecondaryDNSTSIG updates an existing secondary DNS TSIG at
// the account level.
//
// API reference: https://api.cloudflare.com/#secondary-dns-tsig--update-tsig
func (api *API) UpdateSecondaryDNSTSIG(ctx context.Context, accountID string, tsig SecondaryDNSTSIG) (SecondaryDNSTSIG, error) {
	if tsig.ID == "" {
		return SecondaryDNSTSIG{}, errors.New(errSecondaryDNSTSIGMissingID)
	}

	uri := fmt.Sprintf("/accounts/%s/secondary_dns/tsigs/%s", accountID, tsig.ID)
	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, tsig)

	if err != nil {
		return SecondaryDNSTSIG{}, err
	}

	result := SecondaryDNSTSIGDetailResponse{}
	if err := json.Unmarshal(res, &result); err != nil {
		return SecondaryDNSTSIG{}, errors.Wrap(err, errUnmarshalError)
	}

	return result.Result, nil
}

// DeleteSecondaryDNSTSIG deletes a secondary DNS TSIG.
//
// API reference: https://api.cloudflare.com/#secondary-dns-tsig--delete-tsig
func (api *API) DeleteSecondaryDNSTSIG(ctx context.Context, accountID, tsigID string) error {
	uri := fmt.Sprintf("/accounts/%s/secondary_dns/tsigs/%s", accountID, tsigID)
	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)

	if err != nil {
		return err
	}

	return nil
}
