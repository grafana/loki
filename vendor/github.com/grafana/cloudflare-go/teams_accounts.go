package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkg/errors"
)

type TeamsAccount struct {
	GatewayTag   string `json:"gateway_tag"`   // Internal teams ID
	ProviderName string `json:"provider_name"` // Auth provider
	ID           string `json:"id"`            // cloudflare account ID
}

// TeamsAccountResponse is the API response, containing information on teams
// account.
type TeamsAccountResponse struct {
	Response
	Result TeamsAccount `json:"result"`
}

// TeamsConfigResponse is the API response, containing information on teams
// account config.
type TeamsConfigResponse struct {
	Response
	Result TeamsConfiguration `json:"result"`
}

// TeamsConfiguration data model.
type TeamsConfiguration struct {
	Settings  TeamsAccountSettings `json:"settings"`
	CreatedAt time.Time            `json:"created_at,omitempty"`
	UpdatedAt time.Time            `json:"updated_at,omitempty"`
}

type TeamsAccountSettings struct {
	Antivirus   *TeamsAntivirus   `json:"antivirus,omitempty"`
	TLSDecrypt  *TeamsTLSDecrypt  `json:"tls_decrypt,omitempty"`
	ActivityLog *TeamsActivityLog `json:"activity_log,omitempty"`
	BlockPage   *TeamsBlockPage   `json:"block_page,omitempty"`
	FIPS        *TeamsFIPS        `json:"fips,omitempty"`
}

type TeamsAntivirus struct {
	EnabledDownloadPhase bool `json:"enabled_download_phase"`
	EnabledUploadPhase   bool `json:"enabled_upload_phase"`
	FailClosed           bool `json:"fail_closed"`
}

type TeamsFIPS struct {
	TLS bool `json:"tls"`
}

type TeamsTLSDecrypt struct {
	Enabled bool `json:"enabled"`
}

type TeamsActivityLog struct {
	Enabled bool `json:"enabled"`
}

type TeamsBlockPage struct {
	Enabled         *bool  `json:"enabled,omitempty"`
	FooterText      string `json:"footer_text,omitempty"`
	HeaderText      string `json:"header_text,omitempty"`
	LogoPath        string `json:"logo_path,omitempty"`
	BackgroundColor string `json:"background_color,omitempty"`
	Name            string `json:"name,omitempty"`
}

// TeamsAccount returns teams account information with internal and external ID.
//
// API reference: TBA
func (api *API) TeamsAccount(ctx context.Context, accountID string) (TeamsAccount, error) {
	uri := fmt.Sprintf("/accounts/%s/gateway", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return TeamsAccount{}, err
	}

	var teamsAccountResponse TeamsAccountResponse
	err = json.Unmarshal(res, &teamsAccountResponse)
	if err != nil {
		return TeamsAccount{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsAccountResponse.Result, nil
}

// TeamsAccountConfiguration returns teams account configuration.
//
// API reference: TBA
func (api *API) TeamsAccountConfiguration(ctx context.Context, accountID string) (TeamsConfiguration, error) {
	uri := fmt.Sprintf("/accounts/%s/gateway/configuration", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return TeamsConfiguration{}, err
	}

	var teamsConfigResponse TeamsConfigResponse
	err = json.Unmarshal(res, &teamsConfigResponse)
	if err != nil {
		return TeamsConfiguration{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsConfigResponse.Result, nil
}

// TeamsAccountUpdateConfiguration updates a teams account configuration.
//
// API reference: TBA
func (api *API) TeamsAccountUpdateConfiguration(ctx context.Context, accountID string, config TeamsConfiguration) (TeamsConfiguration, error) {
	uri := fmt.Sprintf("/accounts/%s/gateway/configuration", accountID)

	res, err := api.makeRequestContext(ctx, http.MethodPut, uri, config)
	if err != nil {
		return TeamsConfiguration{}, err
	}

	var teamsConfigResponse TeamsConfigResponse
	err = json.Unmarshal(res, &teamsConfigResponse)
	if err != nil {
		return TeamsConfiguration{}, errors.Wrap(err, errUnmarshalError)
	}

	return teamsConfigResponse.Result, nil
}
