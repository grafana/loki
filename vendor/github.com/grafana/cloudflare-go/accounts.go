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

// AccountSettings outlines the available options for an account.
type AccountSettings struct {
	EnforceTwoFactor bool `json:"enforce_twofactor"`
}

// Account represents the root object that owns resources.
type Account struct {
	ID       string           `json:"id,omitempty"`
	Name     string           `json:"name,omitempty"`
	Type     string           `json:"type,omitempty"`
	Settings *AccountSettings `json:"settings,omitempty"`
}

// AccountResponse represents the response from the accounts endpoint for a
// single account ID.
type AccountResponse struct {
	Result Account `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// AccountListResponse represents the response from the list accounts endpoint.
type AccountListResponse struct {
	Result []Account `json:"result"`
	Response
	ResultInfo `json:"result_info"`
}

// AccountDetailResponse is the API response, containing a single Account.
type AccountDetailResponse struct {
	Success  bool     `json:"success"`
	Errors   []string `json:"errors"`
	Messages []string `json:"messages"`
	Result   Account  `json:"result"`
}

// Accounts returns all accounts the logged in user has access to.
//
// API reference: https://api.cloudflare.com/#accounts-list-accounts
func (api *API) Accounts(ctx context.Context, pageOpts PaginationOptions) ([]Account, ResultInfo, error) {
	v := url.Values{}
	if pageOpts.PerPage > 0 {
		v.Set("per_page", strconv.Itoa(pageOpts.PerPage))
	}
	if pageOpts.Page > 0 {
		v.Set("page", strconv.Itoa(pageOpts.Page))
	}

	uri := "/accounts"
	if len(v) > 0 {
		uri = fmt.Sprintf("%s?%s", uri, v.Encode())
	}

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return []Account{}, ResultInfo{}, err
	}

	var accListResponse AccountListResponse
	err = json.Unmarshal(res, &accListResponse)
	if err != nil {
		return []Account{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}
	return accListResponse.Result, accListResponse.ResultInfo, nil
}

// Account returns a single account based on the ID.
//
// API reference: https://api.cloudflare.com/#accounts-account-details
func (api *API) Account(ctx context.Context, accountID string) (Account, ResultInfo, error) {
	uri := fmt.Sprintf("/accounts/%s", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return Account{}, ResultInfo{}, err
	}

	var accResponse AccountResponse
	err = json.Unmarshal(res, &accResponse)
	if err != nil {
		return Account{}, ResultInfo{}, errors.Wrap(err, errUnmarshalError)
	}

	return accResponse.Result, accResponse.ResultInfo, nil
}

// UpdateAccount allows management of an account using the account ID.
//
// API reference: https://api.cloudflare.com/#accounts-update-account
func (api *API) UpdateAccount(ctx context.Context, accountID string, account Account) (Account, error) {
	uri := fmt.Sprintf("/accounts/%s", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, account)
	if err != nil {
		return Account{}, err
	}

	var a AccountDetailResponse
	err = json.Unmarshal(res, &a)
	if err != nil {
		return Account{}, errors.Wrap(err, errUnmarshalError)
	}

	return a.Result, nil
}

// CreateAccount creates a new account. Note: This requires the Tenant
// entitlement.
//
// API reference: https://developers.cloudflare.com/tenant/tutorial/provisioning-resources#creating-an-account
func (api *API) CreateAccount(ctx context.Context, account Account) (Account, error) {
	uri := "/accounts"

	res, err := api.makeRequestContext(ctx, http.MethodPost, uri, account)
	if err != nil {
		return Account{}, err
	}

	var a AccountDetailResponse
	err = json.Unmarshal(res, &a)
	if err != nil {
		return Account{}, errors.Wrap(err, errUnmarshalError)
	}

	return a.Result, nil
}

// DeleteAccount removes an account. Note: This requires the Tenant
// entitlement.
//
// API reference: https://developers.cloudflare.com/tenant/tutorial/provisioning-resources#optional-deleting-accounts
func (api *API) DeleteAccount(ctx context.Context, accountID string) error {
	if accountID == "" {
		return errors.New(errMissingAccountID)
	}

	uri := fmt.Sprintf("/accounts/%s", accountID)

	_, err := api.makeRequestContext(ctx, http.MethodDelete, uri, nil)
	if err != nil {
		return err
	}

	return nil
}
